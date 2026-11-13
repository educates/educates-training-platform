package cluster

import (
	"context"

	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/client"
	"github.com/pkg/errors"

	"github.com/educates/educates-training-platform/client-programs/pkg/docker"
	"github.com/educates/educates-training-platform/client-programs/pkg/localclusters"
	"github.com/educates/educates-training-platform/client-programs/pkg/utils"
)

// The labels kind puts on every node container it creates, naming the
// kind cluster the container belongs to and the node's role.
const (
	kindClusterLabelKey = "io.x-k8s.kind.cluster"
	kindRoleLabelKey    = "io.x-k8s.kind.role"
)

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
