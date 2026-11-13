package cmd

import (
	"github.com/educates/educates-training-platform/client-programs/pkg/localclusters"
	"github.com/educates/educates-training-platform/client-programs/pkg/utils"
)

// defaultLocalCluster returns the identity of the default local cluster of
// the data home: the cluster the local commands act on.
func defaultLocalCluster() localclusters.Identity {
	return localclusters.Default(utils.GetEducatesHomeDir())
}
