package cmd

import (
	"context"

	"github.com/educates/educates-training-platform/client-programs/pkg/cluster"
	"github.com/educates/educates-training-platform/client-programs/pkg/localclusters"
	"github.com/educates/educates-training-platform/client-programs/pkg/utils"
)

// defaultLocalCluster returns the identity of the default local cluster of
// the data home: the cluster the local commands act on.
func defaultLocalCluster() localclusters.Identity {
	return localclusters.Default(utils.GetEducatesHomeDir())
}

// registryNodes returns the names of the node containers of the local
// cluster id that get the containerd host config of the local registry and
// the registry mirrors: every node of the cluster that runs a kubelet. It is
// empty when the cluster has no node containers.
func registryNodes(ctx context.Context, id localclusters.Identity) ([]string, error) {
	observed, err := cluster.ObserveNodeContainers(ctx)
	if err != nil {
		return nil, err
	}
	return id.RegistryNodes(observed), nil
}
