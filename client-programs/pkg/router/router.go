// Package router runs the router, the HAProxy container that publishes
// ports 80 and 443 on the router address for every local cluster's
// ingress, and port 6443 for their API servers, and sends each request to
// the cluster that owns its name. It serves the
// configuration localclusters renders into the router folder of the data
// home, and reports the container's state for the inventory.
package router

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/netip"
	"os"
	"strconv"
	"time"

	cerrdefs "github.com/containerd/errdefs"
	"github.com/moby/moby/api/pkg/stdcopy"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/mount"
	"github.com/moby/moby/api/types/network"
	"github.com/moby/moby/client"
	"github.com/pkg/errors"

	"github.com/educates/educates-training-platform/client-programs/pkg/constants"
	"github.com/educates/educates-training-platform/client-programs/pkg/docker"
	"github.com/educates/educates-training-platform/client-programs/pkg/localclusters"
	"github.com/educates/educates-training-platform/client-programs/pkg/registry"
)

const (
	// Image is the router image the CLI pins: HAProxy, the alpine image of
	// the 3.4 long-term support branch, by its tag and the digest of its
	// image index. A router created from any other image is re-created.
	Image = "haproxy:3.4.8-alpine@sha256:8aa6458a9d22a8364a2d2ee9ce93de0cc4a2789e3cf63f0b44adc972863724d7"

	// ContainerName is the name of the router container.
	ContainerName = "educates-router"

	// setUpTimeout is how long a command waits for another command that
	// creates the router at the same time to start it.
	setUpTimeout = 60 * time.Second

	// setUpPollInterval is how often it looks again.
	setUpPollInterval = 250 * time.Millisecond
)

// StartError is the error of a router container that was created but did
// not start, which is how the router finds out that something else holds
// one of its ports, 80, 443 or 6443, on the router address. The container
// is removed.
type StartError struct {
	Err error
}

func (e *StartError) Error() string {
	return e.Err.Error()
}

func (e *StartError) Unwrap() error {
	return e.Err
}

// Observe returns the router container, running or stopped, as the
// inventory of local clusters takes it, or nil when there is none.
func Observe(ctx context.Context) (*localclusters.RouterContainer, error) {
	cli, err := docker.NewDockerClient()
	if err != nil {
		return nil, errors.Wrap(err, "unable to create docker client")
	}

	info, err := cli.ContainerInspect(ctx, ContainerName, client.ContainerInspectOptions{})
	if cerrdefs.IsNotFound(err) {
		return nil, nil
	}
	if err != nil {
		return nil, errors.Wrapf(err, "unable to inspect router container %s", ContainerName)
	}

	router := &localclusters.RouterContainer{ImageID: info.Container.Image}
	if info.Container.State != nil {
		router.Running = info.Container.State.Running
	}
	if info.Container.Config != nil {
		router.Image = info.Container.Config.Image
	}
	// The host may not have the pinned image, as when the CLI pins a newer
	// one than the router runs: the inventory then compares digests.
	if pinned, err := cli.ImageInspect(ctx, Image); err == nil {
		router.PinnedImageID = pinned.ID
	}
	if info.Container.HostConfig != nil {
		for _, bindings := range info.Container.HostConfig.PortBindings {
			for _, b := range bindings {
				port, err := strconv.ParseUint(b.HostPort, 10, 16)
				if err != nil {
					continue
				}
				router.PortBindings = append(router.PortBindings, localclusters.PortBinding{HostIP: b.HostIP, HostPort: uint16(port)})
			}
		}
	}
	return router, nil
}

// Create creates the router container and starts it: from Image, pulled
// when missing, publishing ports 80, 443 and 6443 on the router address
// address only, and serving the configuration of the router folder dir,
// which it mounts read-only. It runs with restart policy always, so it
// comes back with Docker.
//
// The router starts on the educates network, so that Docker's embedded
// DNS finds the nodes for it by their container names, and joins the kind
// network, where the nodes are, when it exists: JoinClusterNetwork joins
// it once kind has made it.
//
// A container that fails to start is removed, and the error is a
// *StartError carrying Docker's. When another command creates the router
// at the same time, Create waits for that command to start it, and
// reports that it adopted it, which leaves its configuration to a
// reload.
func Create(ctx context.Context, address, dir string) (adopted bool, err error) {
	cli, err := docker.NewDockerClient()
	if err != nil {
		return false, errors.Wrap(err, "unable to create docker client")
	}

	hostIP, err := netip.ParseAddr(address)
	if err != nil {
		return false, errors.Wrapf(err, "invalid router address %q", address)
	}

	if err := pullImage(ctx, cli); err != nil {
		return false, err
	}
	if err := registry.EnsureEducatesNetwork(ctx, cli); err != nil {
		return false, err
	}
	engine, err := cli.Info(ctx, client.InfoOptions{})
	if err != nil {
		return false, errors.Wrap(err, "unable to read the Docker engine's information")
	}
	user := localclusters.RouterUser(engine.Info.SecurityOptions, os.Getuid(), os.Getgid())

	for attempt := 0; ; attempt++ {
		err := createContainer(ctx, cli, hostIP, dir, user)
		if err == nil {
			break
		}
		if !cerrdefs.IsConflict(err) || attempt > 0 {
			return false, errors.Wrap(err, "cannot create router container")
		}
		// Another command holds the name: wait for it to start the router.
		running, found, err := waitForStart(ctx, cli)
		if err != nil {
			return false, err
		}
		if running {
			return true, nil
		}
		if found {
			return false, errors.Errorf("router container %s, which another command created, has not started: "+
				"if it does not, delete it with 'docker rm -f %s' and run the command again", ContainerName, ContainerName)
		}
	}

	if err := JoinClusterNetwork(ctx); err != nil {
		cli.ContainerRemove(ctx, ContainerName, client.ContainerRemoveOptions{Force: true})
		return false, err
	}
	if _, err := cli.ContainerStart(ctx, ContainerName, client.ContainerStartOptions{}); err != nil {
		cli.ContainerRemove(ctx, ContainerName, client.ContainerRemoveOptions{Force: true})
		return false, &StartError{Err: err}
	}
	return false, nil
}

// pullImage pulls Image unless the Docker host has it.
func pullImage(ctx context.Context, cli *client.Client) error {
	if _, err := cli.ImageInspect(ctx, Image); err == nil {
		return nil
	}
	reader, err := cli.ImagePull(ctx, Image, client.ImagePullOptions{})
	if err != nil {
		return errors.Wrap(err, "cannot pull router image")
	}
	defer reader.Close()
	// Drain the pull stream so the image fully downloads.
	if _, err := io.Copy(io.Discard, reader); err != nil {
		return errors.Wrap(err, "cannot pull router image")
	}
	return nil
}

// createContainer creates the router container, without starting it,
// publishing the router ports, localclusters.RouterPorts, on hostIP: 80
// and 443 for the local clusters' ingress and 6443 for their API servers.
// It mounts the router folder dir,
// and running HAProxy as user, as localclusters.RouterUser chooses it. A
// router container that exists already fails it with Docker's conflict
// error.
func createContainer(ctx context.Context, cli *client.Client, hostIP netip.Addr, dir, user string) error {
	exposed := network.PortSet{}
	bindings := network.PortMap{}
	for _, port := range localclusters.RouterPorts() {
		key := network.MustParsePort(fmt.Sprintf("%d/tcp", port))
		exposed[key] = struct{}{}
		bindings[key] = []network.PortBinding{{HostIP: hostIP, HostPort: strconv.Itoa(int(port))}}
	}

	_, err := cli.ContainerCreate(ctx, client.ContainerCreateOptions{
		Config: &container.Config{
			Image:        Image,
			ExposedPorts: exposed,
			Labels: map[string]string{
				constants.EducatesContainersAppLabelKey:  constants.EducatesContainersAppLabel,
				constants.EducatesContainersRoleLabelKey: constants.EducatesContainersRouterRoleLabel,
			},
			User: user,
		},
		HostConfig: &container.HostConfig{
			NetworkMode:  container.NetworkMode(registry.EducatesNetworkName),
			PortBindings: bindings,
			// HAProxy runs as a user that is not root, except on a
			// rootless engine, and such a user binds ports below 1024 only
			// with this setting.
			Sysctls: map[string]string{"net.ipv4.ip_unprivileged_port_start": "0"},
			Mounts: []mount.Mount{{
				Type:     mount.TypeBind,
				Source:   dir,
				Target:   localclusters.RouterConfigDir,
				ReadOnly: true,
			}},
			RestartPolicy: container.RestartPolicy{Name: container.RestartPolicyAlways},
			LogConfig: container.LogConfig{
				Config: map[string]string{"max-size": "100m"},
			},
		},
		Name: ContainerName,
	})
	return err
}

// waitForStart waits for the router container another command creates to
// start, and reports whether it runs, or whether it still exists but has
// not started once setUpTimeout passes. A container that goes away, as
// one whose start failed does, is neither.
func waitForStart(ctx context.Context, cli *client.Client) (running, found bool, err error) {
	deadline := time.Now().Add(setUpTimeout)
	for {
		info, err := cli.ContainerInspect(ctx, ContainerName, client.ContainerInspectOptions{})
		switch {
		case cerrdefs.IsNotFound(err):
			// The other command has not finished creating it, or removed
			// it after its start failed.
		case err != nil:
			return false, false, errors.Wrapf(err, "unable to inspect router container %s", ContainerName)
		case info.Container.State != nil && info.Container.State.Running:
			return true, true, nil
		case info.Container.State != nil && info.Container.State.Status != container.StateCreated:
			return false, true, nil
		}
		if !time.Now().Before(deadline) {
			return false, !cerrdefs.IsNotFound(err), nil
		}
		select {
		case <-ctx.Done():
			return false, false, ctx.Err()
		case <-time.After(setUpPollInterval):
		}
	}
}

// JoinClusterNetwork connects the router container to the kind network,
// where the local clusters' nodes are, when that network exists and the
// container is not on it yet. The kind network exists once kind has
// created a cluster.
func JoinClusterNetwork(ctx context.Context) error {
	cli, err := docker.NewDockerClient()
	if err != nil {
		return errors.Wrap(err, "unable to create docker client")
	}

	if _, err := cli.NetworkInspect(ctx, registry.ClusterNetworkName, client.NetworkInspectOptions{}); cerrdefs.IsNotFound(err) {
		return nil
	} else if err != nil {
		return errors.Wrapf(err, "unable to inspect %s network", registry.ClusterNetworkName)
	}

	info, err := cli.ContainerInspect(ctx, ContainerName, client.ContainerInspectOptions{})
	if err != nil {
		return errors.Wrapf(err, "unable to inspect router container %s", ContainerName)
	}
	if settings := info.Container.NetworkSettings; settings != nil && settings.Networks[registry.ClusterNetworkName] != nil {
		return nil
	}

	_, err = cli.NetworkConnect(ctx, registry.ClusterNetworkName, client.NetworkConnectOptions{
		Container:      ContainerName,
		EndpointConfig: &network.EndpointSettings{},
	})
	if err != nil && !cerrdefs.IsConflict(err) {
		return errors.Wrapf(err, "unable to connect router container %s to the %s network", ContainerName, registry.ClusterNetworkName)
	}
	return nil
}

// Reload makes the running router container serve the configuration of
// its folder: it checks the configuration with HAProxy first, and then
// signals HAProxy's master process to reload. The master starts new
// workers with the new configuration, and the previous workers keep their
// open connections, such as terminals, until those close. A configuration
// HAProxy refuses is an error carrying HAProxy's output, and the router
// keeps serving the previous one.
func Reload(ctx context.Context) error {
	cli, err := docker.NewDockerClient()
	if err != nil {
		return errors.Wrap(err, "unable to create docker client")
	}

	if err := checkConfig(ctx, cli); err != nil {
		return err
	}
	if _, err := cli.ContainerKill(ctx, ContainerName, client.ContainerKillOptions{Signal: "USR2"}); err != nil {
		return errors.Wrapf(err, "unable to signal router container %s to reload", ContainerName)
	}
	return nil
}

// checkConfig runs HAProxy's configuration check in the router container
// against the configuration of its folder.
func checkConfig(ctx context.Context, cli *client.Client) error {
	created, err := cli.ExecCreate(ctx, ContainerName, client.ExecCreateOptions{
		AttachStdout: true,
		AttachStderr: true,
		Cmd:          []string{"haproxy", "-c", "-q", "-f", localclusters.RouterConfigDir + "/haproxy.cfg"},
	})
	if err != nil {
		return errors.Wrapf(err, "unable to check the configuration of router container %s", ContainerName)
	}
	attached, err := cli.ExecAttach(ctx, created.ID, client.ExecAttachOptions{})
	if err != nil {
		return errors.Wrapf(err, "unable to check the configuration of router container %s", ContainerName)
	}
	var output bytes.Buffer
	_, err = stdcopy.StdCopy(&output, &output, attached.Reader)
	attached.Close()
	if err != nil {
		return errors.Wrapf(err, "unable to check the configuration of router container %s", ContainerName)
	}
	inspected, err := cli.ExecInspect(ctx, created.ID, client.ExecInspectOptions{})
	if err != nil {
		return errors.Wrapf(err, "unable to check the configuration of router container %s", ContainerName)
	}
	if inspected.ExitCode != 0 {
		return fmt.Errorf("the router refuses its configuration, so it keeps serving the previous one: %s",
			bytes.TrimSpace(output.Bytes()))
	}
	return nil
}

// Remove removes the router container, killing it first when it runs. A
// missing container is no error.
//
// The router is removed only where its open connections are dropped
// anyway, when it is re-created or deleted with the last local cluster,
// so it is not stopped gracefully: the image stops HAProxy with SIGUSR1,
// a soft stop that waits for every open connection, such as a terminal's,
// to close.
func Remove(ctx context.Context) error {
	cli, err := docker.NewDockerClient()
	if err != nil {
		return errors.Wrap(err, "unable to create docker client")
	}

	if _, err := cli.ContainerRemove(ctx, ContainerName, client.ContainerRemoveOptions{Force: true}); err != nil && !cerrdefs.IsNotFound(err) {
		return errors.Wrapf(err, "unable to remove router container %s", ContainerName)
	}
	return nil
}
