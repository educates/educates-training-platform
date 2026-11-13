package cmd

import (
	"context"

	"github.com/educates/educates-training-platform/client-programs/pkg/cluster"
	"github.com/educates/educates-training-platform/client-programs/pkg/localclusters"
	"github.com/educates/educates-training-platform/client-programs/pkg/utils"
)

// localClusterInventory observes the kind node containers on the Docker
// host and the host's network interfaces, and returns the inventory of
// the Educates clusters of the data home in that state.
func localClusterInventory(ctx context.Context) (localclusters.Inventory, error) {
	observed, err := cluster.ObserveLocalClusters(ctx)
	if err != nil {
		return localclusters.Inventory{}, err
	}
	return localclusters.NewInventory(observed, utils.GetEducatesHomeDir()), nil
}
