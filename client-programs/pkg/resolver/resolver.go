// Package resolver runs the local DNS resolver, a dnsmasq container that
// serves the configuration it is given from the data home.
package resolver

import (
	"context"
	"fmt"
	"io"
	"net/netip"
	"os"
	"path"
	"slices"

	cerrdefs "github.com/containerd/errdefs"
	"github.com/educates/educates-training-platform/client-programs/pkg/config"
	"github.com/educates/educates-training-platform/client-programs/pkg/constants"
	"github.com/educates/educates-training-platform/client-programs/pkg/docker"
	"github.com/educates/educates-training-platform/client-programs/pkg/localclusters"
	"github.com/educates/educates-training-platform/client-programs/pkg/utils"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/mount"
	"github.com/moby/moby/api/types/network"
	"github.com/moby/moby/client"
	"github.com/pkg/errors"
)

const (
	dnsmasqImage          = "ghcr.io/dockur/dnsmasq:2.93"
	resolverContainerName = "educates-resolver"
)

// DeployResolver starts the local DNS resolver container serving the
// dnsmasq configuration content. When the container already exists it is
// left as it is, its configuration included.
func DeployResolver(content []byte) error {
	ctx := context.Background()

	fmt.Println("Deploying local DNS resolver")

	cli, err := docker.NewDockerClient()

	if err != nil {
		return errors.Wrap(err, "unable to create docker client")
	}

	_, err = cli.ContainerInspect(ctx, resolverContainerName, client.ContainerInspectOptions{})

	if err == nil {
		// If we can retrieve a container of required name we assume it is
		// running okay. Technically it could be restarting, stopping or
		// have exited and container was not removed, but if that is the case
		// then leave it up to the user to sort out.

		return nil
	}

	reader, err := cli.ImagePull(ctx, dnsmasqImage, client.ImagePullOptions{})
	if err != nil {
		return errors.Wrap(err, "cannot pull dnsmasq image")
	}

	defer reader.Close()
	io.Copy(os.Stdout, reader)

	configFileName, err := writeConfig(content)
	if err != nil {
		return err
	}

	// The rendered configuration reads the user's own entries from the
	// include folder, which writeConfig creates when it is missing.
	hostConfig := &container.HostConfig{
		PortBindings: network.PortMap{
			network.MustParsePort("53/udp"): []network.PortBinding{
				{
					HostIP:   netip.MustParseAddr("127.0.0.1"),
					HostPort: "53",
				},
			},
		},
		Mounts: []mount.Mount{
			{
				Type:     "bind",
				Source:   configFileName,
				Target:   "/etc/dnsmasq.conf",
				ReadOnly: true,
			},
			{
				Type:     "bind",
				Source:   config.DnsmasqIncludeDir(utils.GetEducatesHomeDir()),
				Target:   localclusters.ResolverIncludeDir,
				ReadOnly: true,
			},
		},
		RestartPolicy: container.RestartPolicy{
			Name: "always",
		},
		LogConfig: container.LogConfig{
			Config: map[string]string{
				"max-size": "100m",
			},
		},
	}

	resp, err := cli.ContainerCreate(ctx, client.ContainerCreateOptions{
		Config: &container.Config{
			Image: dnsmasqImage,
			Tty:   false,
			ExposedPorts: network.PortSet{
				network.MustParsePort("53/udp"): struct{}{},
			},
			Labels: map[string]string{
				constants.EducatesContainersAppLabelKey:  constants.EducatesContainersAppLabel,
				constants.EducatesContainersRoleLabelKey: constants.EducatesContainersResolverRoleLabel,
			},
		},
		HostConfig: hostConfig,
		Name:       resolverContainerName,
	})

	if err != nil {
		return errors.Wrap(err, "cannot create resolver container")
	}

	if _, err := cli.ContainerStart(ctx, resp.ID, client.ContainerStartOptions{}); err != nil {
		return errors.Wrap(err, "unable to start resolver")
	}

	fmt.Println("Local DNS resolver running as a Docker container", resolverContainerName)
	fmt.Println("Local DNS resolver configuration in", configFileName)

	return nil
}

func DeleteResolver() error {
	ctx := context.Background()

	fmt.Println("Deleting local DNS resolver")

	cli, err := docker.NewDockerClient()

	if err != nil {
		return errors.Wrap(err, "unable to create docker client")
	}

	_, err = cli.ContainerInspect(ctx, resolverContainerName, client.ContainerInspectOptions{})

	if err != nil {
		// If we can't retrieve a container of required name we assume it does
		// not actually exist.

		return nil
	}

	timeout := 30

	_, err = cli.ContainerStop(ctx, resolverContainerName, client.ContainerStopOptions{Timeout: &timeout})

	if err != nil {
		return errors.Wrap(err, "unable to stop DNS resolver container")
	}

	_, err = cli.ContainerRemove(ctx, resolverContainerName, client.ContainerRemoveOptions{})

	if err != nil {
		return errors.Wrap(err, "unable to delete DNS resolver container")
	}

	return nil
}

// UpdateResolver replaces the configuration of the local DNS resolver
// with the dnsmasq configuration content and restarts its container, so
// it serves it, and reports what it does to w: io.Discard for a command
// that reports its own progress. It fails when the container does not
// exist.
//
// It also reports whether the container mounts the include folder of the
// data home, which a container created by an earlier release does not:
// such a container serves the configuration, but none of the user's own
// entries, until it is deleted and deployed again.
func UpdateResolver(w io.Writer, content []byte) (includeFolderMounted bool, err error) {
	fmt.Fprintln(w, "Updating local DNS resolver configuration")

	ctx := context.Background()

	cli, err := docker.NewDockerClient()
	if err != nil {
		return false, errors.Wrap(err, "unable to create docker client")
	}

	inspected, err := cli.ContainerInspect(ctx, resolverContainerName, client.ContainerInspectOptions{})
	if err != nil {
		return false, errors.Wrap(err, "resolver container not found")
	}

	configFileName, err := writeConfig(content)
	if err != nil {
		return false, err
	}

	_, err = cli.ContainerRestart(ctx, resolverContainerName, client.ContainerRestartOptions{})
	if err != nil {
		return false, errors.Wrap(err, "failed to restart resolver")
	}

	fmt.Fprintln(w, "Local DNS resolver configuration updated and reloaded")
	fmt.Fprintln(w, "Local DNS resolver configuration in", configFileName)

	return mountsIncludeFolder(inspected.Container.Mounts), nil
}

// mountsIncludeFolder reports whether a resolver container with the
// mounts given mounts the include folder at localclusters.ResolverIncludeDir.
func mountsIncludeFolder(mounts []container.MountPoint) bool {
	return slices.ContainsFunc(mounts, func(m container.MountPoint) bool {
		return m.Destination == localclusters.ResolverIncludeDir
	})
}

// ResolverExists reports whether the local DNS resolver container exists,
// running or not.
func ResolverExists() (bool, error) {
	cli, err := docker.NewDockerClient()
	if err != nil {
		return false, errors.Wrap(err, "unable to create docker client")
	}

	_, err = cli.ContainerInspect(context.Background(), resolverContainerName, client.ContainerInspectOptions{})
	switch {
	case err == nil:
		return true, nil
	case cerrdefs.IsNotFound(err):
		return false, nil
	default:
		return false, errors.Wrap(err, "unable to inspect DNS resolver container")
	}
}

// writeConfig writes the dnsmasq configuration content to dnsmasq.conf in
// the data home, the file the resolver container mounts, rewriting it in
// place, and returns its path. It also creates the include folder the
// container mounts, empty, when it is missing, and never writes into it.
func writeConfig(content []byte) (string, error) {
	configFileDir := utils.GetEducatesHomeDir()
	configFileName := path.Join(configFileDir, "dnsmasq.conf")

	err := os.MkdirAll(configFileDir, os.ModePerm)

	if err != nil {
		return "", errors.Wrapf(err, "unable to create config directory")
	}

	includeDir := config.DnsmasqIncludeDir(configFileDir)
	if err := os.MkdirAll(includeDir, os.ModePerm); err != nil {
		return "", errors.Wrapf(err, "unable to create the local DNS resolver include folder %s", includeDir)
	}

	err = os.WriteFile(configFileName, content, 0644)

	if err != nil {
		return "", errors.Wrap(err, "failed to write dnsmasq config")
	}

	return configFileName, nil
}
