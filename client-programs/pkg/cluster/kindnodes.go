package cluster

import (
	"context"
	"net"
	"net/netip"
	"strconv"

	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/client"
	"github.com/pkg/errors"

	"github.com/educates/educates-training-platform/client-programs/pkg/docker"
	"github.com/educates/educates-training-platform/client-programs/pkg/localclusters"
	"github.com/educates/educates-training-platform/client-programs/pkg/registry"
	"github.com/educates/educates-training-platform/client-programs/pkg/utils"
)

// The labels kind puts on every node container it creates, naming the
// kind cluster the container belongs to and the node's role.
const (
	kindClusterLabelKey = "io.x-k8s.kind.cluster"
	kindRoleLabelKey    = "io.x-k8s.kind.role"
)

// kindControlPlaneRole is the role kind names a control-plane node by.
const kindControlPlaneRole = "control-plane"

// ObserveLocalClusters returns the state of the host the inventory of
// local clusters answers from: every kind node container on the Docker
// host, as ObserveNodeContainers returns them, with the host ports each
// control-plane node is configured to publish, the addresses of the
// host's network interfaces, and the registry mirror containers, as
// registry.ObserveMirrorContainers returns them.
//
// The port bindings come from each container's configured host config,
// not from the ports it publishes now, so a stopped cluster's count too.
func ObserveLocalClusters(ctx context.Context) (localclusters.Observed, error) {
	nodes, err := ObserveNodeContainers(ctx)
	if err != nil {
		return localclusters.Observed{}, err
	}

	cli, err := docker.NewDockerClient()
	if err != nil {
		return localclusters.Observed{}, errors.Wrap(err, "unable to create docker client")
	}

	for i, n := range nodes {
		if n.Role != kindControlPlaneRole {
			continue
		}
		info, err := cli.ContainerInspect(ctx, n.Name, client.ContainerInspectOptions{})
		if err != nil {
			return localclusters.Observed{}, errors.Wrapf(err, "unable to inspect kind node container %s", n.Name)
		}
		if info.Container.HostConfig == nil {
			continue
		}
		for _, bindings := range info.Container.HostConfig.PortBindings {
			for _, b := range bindings {
				port, err := strconv.ParseUint(b.HostPort, 10, 16)
				if err != nil {
					// No host port, or a range: Docker picks the port.
					continue
				}
				nodes[i].PortBindings = append(nodes[i].PortBindings, localclusters.PortBinding{
					HostIP:   b.HostIP,
					HostPort: uint16(port),
				})
			}
		}
	}

	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return localclusters.Observed{}, errors.Wrap(err, "unable to list the addresses of the host's network interfaces")
	}
	interfaces := make([]netip.Addr, 0, len(addrs))
	for _, a := range addrs {
		ipNet, ok := a.(*net.IPNet)
		if !ok {
			continue
		}
		if addr, ok := netip.AddrFromSlice(ipNet.IP); ok {
			interfaces = append(interfaces, addr.Unmap())
		}
	}

	mirrors, err := registry.ObserveMirrorContainers(ctx)
	if err != nil {
		return localclusters.Observed{}, err
	}

	return localclusters.Observed{Nodes: nodes, InterfaceAddresses: interfaces, Mirrors: mirrors}, nil
}

// ObserveNodeContainers returns every kind node container on the Docker
// host, running or stopped, with the kind cluster and role its labels
// name.
func ObserveNodeContainers(ctx context.Context) ([]localclusters.NodeContainer, error) {
	cli, err := docker.NewDockerClient()
	if err != nil {
		return nil, errors.Wrap(err, "unable to create docker client")
	}

	list, err := cli.ContainerList(ctx, client.ContainerListOptions{
		All:     true,
		Filters: client.Filters{}.Add("label", kindClusterLabelKey),
	})
	if err != nil {
		return nil, errors.Wrap(err, "unable to list kind node containers")
	}

	nodes := make([]localclusters.NodeContainer, 0, len(list.Items))
	for _, item := range list.Items {
		nodes = append(nodes, localclusters.NodeContainer{
			Name:        utils.GetContainerName(item),
			KindCluster: item.Labels[kindClusterLabelKey],
			Role:        item.Labels[kindRoleLabelKey],
			Running:     item.State == container.StateRunning,
		})
	}
	return nodes, nil
}
