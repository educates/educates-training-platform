package cmd

import (
	"context"
	"fmt"
	"io"
	"net/netip"

	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/network"
	"github.com/moby/moby/client"

	"github.com/educates/educates-training-platform/client-programs/pkg/docker"
)

// checkHostPortsAvailable confirms host 80 and 443 are free on the given
// listen address by attempting to start a busybox container with those
// ports bound. Mirrors the v3 'local cluster create' preflight — kind
// otherwise comes up successfully but Envoy can't publish 80/443 and
// the install hangs at IngressReady, leaving users to debug "cluster
// created but nothing reachable".
//
// The probe container is named probeContainer, the local cluster's own
// probe name, so that creates of different local clusters running at the
// same time never remove each other's probe.
func checkHostPortsAvailable(ctx context.Context, probeContainer, listenAddress string, verbose bool, w io.Writer) error {

	const busyboxImage = "docker.io/library/busybox:latest"

	cli, err := docker.NewDockerClient()
	if err != nil {
		return fmt.Errorf("port-availability check: docker client: %w", err)
	}

	// Remove any leftover probe container from a previous interrupted run.
	_, _ = cli.ContainerRemove(ctx, probeContainer, client.ContainerRemoveOptions{Force: true})

	// Pull busybox only when it isn't already present locally, so repeat
	// runs — and air-gapped hosts where it's already cached — don't need
	// registry access just to probe the ports.
	if _, err := cli.ImageInspect(ctx, busyboxImage); err != nil {
		reader, err := cli.ImagePull(ctx, busyboxImage, client.ImagePullOptions{})
		if err != nil {
			return fmt.Errorf("port-availability check: pull busybox: %w", err)
		}
		defer reader.Close()
		if verbose {
			_, _ = io.Copy(w, reader)
		} else {
			_, _ = io.Copy(io.Discard, reader)
		}
	}

	listenAddr, err := netip.ParseAddr(listenAddress)
	if err != nil {
		return fmt.Errorf("port-availability check: invalid listen address %q: %w", listenAddress, err)
	}

	exposed := network.PortSet{}
	bindings := network.PortMap{}
	for _, port := range []uint{80, 443} {
		key := network.MustParsePort(fmt.Sprintf("%d/tcp", port))
		exposed[key] = struct{}{}
		bindings[key] = []network.PortBinding{{HostIP: listenAddr, HostPort: fmt.Sprintf("%d", port)}}
	}

	resp, err := cli.ContainerCreate(ctx, client.ContainerCreateOptions{
		Config: &container.Config{
			Image:        busyboxImage,
			Cmd:          []string{"/bin/true"},
			Tty:          false,
			ExposedPorts: exposed,
		},
		HostConfig: &container.HostConfig{PortBindings: bindings},
		Name:       probeContainer,
	})
	if err != nil {
		return fmt.Errorf("port-availability check: create probe: %w", err)
	}
	defer cli.ContainerRemove(ctx, probeContainer, client.ContainerRemoveOptions{Force: true})

	if _, err := cli.ContainerStart(ctx, resp.ID, client.ContainerStartOptions{}); err != nil {
		return fmt.Errorf("ports 80/443 not available on %s — another process (an ingress controller, a dev server, Docker Desktop port forwards) is holding them: %w", listenAddress, err)
	}

	waitResult := cli.ContainerWait(ctx, probeContainer, client.ContainerWaitOptions{Condition: container.WaitConditionNotRunning})
	select {
	case err := <-waitResult.Error:
		if err != nil {
			return fmt.Errorf("port-availability check: wait for probe: %w", err)
		}
	case <-waitResult.Result:
	case <-ctx.Done():
		return ctx.Err()
	}
	return nil
}
