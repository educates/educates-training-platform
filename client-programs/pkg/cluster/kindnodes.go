package cluster

import (
	"archive/tar"
	"context"
	"io"
	"strconv"
	"strings"

	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/client"
	"github.com/pkg/errors"

	"github.com/educates/educates-training-platform/client-programs/pkg/config/hostinfo"
	"github.com/educates/educates-training-platform/client-programs/pkg/docker"
	"github.com/educates/educates-training-platform/client-programs/pkg/localclusters"
	"github.com/educates/educates-training-platform/client-programs/pkg/registry"
	"github.com/educates/educates-training-platform/client-programs/pkg/router"
	"github.com/educates/educates-training-platform/client-programs/pkg/utils"
)

// The labels kind puts on every node container it creates, naming the
// kind cluster the container belongs to and the node's role.
const (
	kindClusterLabelKey = "io.x-k8s.kind.cluster"
	kindRoleLabelKey    = "io.x-k8s.kind.role"
)

// kindVersionPath is the file in which a kind node image records the
// Kubernetes version it holds, such as v1.36.4. kind reads it to configure
// the cluster.
const kindVersionPath = "/kind/version"

// maxKindVersionSize bounds what is read of a node container's
// kindVersionPath, which holds a version string alone.
const maxKindVersionSize = 256

// ObserveLocalClusters returns the state of the host the inventory of
// local clusters answers from: every kind node container on the Docker
// host, as ObserveNodeContainers returns them, with the host ports each
// control-plane node is configured to publish and the Kubernetes version
// of its node image, the addresses of the host's network interfaces, the
// registry mirror containers, as registry.ObserveMirrorContainers returns
// them, and the router container, as router.Observe returns it.
//
// The port bindings come from each container's configured host config,
// not from the ports it publishes now, and the Kubernetes version from
// the container's file system, so a stopped cluster's count too. A
// version that can't be read is left empty and fails no observation.
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
		if n.Role != localclusters.ControlPlaneRole {
			continue
		}
		info, err := cli.ContainerInspect(ctx, n.Name, client.ContainerInspectOptions{})
		if err != nil {
			return localclusters.Observed{}, errors.Wrapf(err, "unable to inspect kind node container %s", n.Name)
		}
		nodes[i].KubernetesVersion = nodeKubernetesVersion(ctx, cli, n.Name)
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

	interfaces, err := hostinfo.InterfaceAddresses()
	if err != nil {
		return localclusters.Observed{}, err
	}

	mirrors, err := registry.ObserveMirrorContainers(ctx)
	if err != nil {
		return localclusters.Observed{}, err
	}

	routerContainer, err := router.Observe(ctx)
	if err != nil {
		return localclusters.Observed{}, err
	}

	return localclusters.Observed{
		Nodes:              nodes,
		InterfaceAddresses: interfaces,
		Mirrors:            mirrors,
		Router:             routerContainer,
	}, nil
}

// nodeKubernetesVersion returns the Kubernetes version of the node image
// of the kind node container name, from its kindVersionPath, or an empty
// string when it can't be read. Docker copies a file out of a container
// whether it runs or not.
func nodeKubernetesVersion(ctx context.Context, cli *client.Client, name string) string {
	res, err := cli.CopyFromContainer(ctx, name, client.CopyFromContainerOptions{SourcePath: kindVersionPath})
	if err != nil {
		return ""
	}
	defer res.Content.Close()
	version, err := kindVersionFromArchive(res.Content)
	if err != nil {
		return ""
	}
	return version
}

// kindVersionFromArchive returns the Kubernetes version a kind node
// image's kindVersionPath holds, from the tar archive of that one file
// that Docker copies out of a node container.
func kindVersionFromArchive(archive io.Reader) (string, error) {
	tr := tar.NewReader(archive)
	header, err := tr.Next()
	if err != nil {
		return "", errors.Wrapf(err, "unable to read the archive of %s", kindVersionPath)
	}
	if header.Typeflag != tar.TypeReg {
		return "", errors.Errorf("%s is not a regular file", kindVersionPath)
	}
	data, err := io.ReadAll(io.LimitReader(tr, maxKindVersionSize))
	if err != nil {
		return "", errors.Wrapf(err, "unable to read %s", kindVersionPath)
	}
	version := strings.TrimSpace(string(data))
	if version == "" {
		return "", errors.Errorf("%s is empty", kindVersionPath)
	}
	return version, nil
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
