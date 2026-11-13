package cmd

import (
	"github.com/spf13/cobra"

	"github.com/educates/educates-training-platform/client-programs/pkg/cluster"
)

var localClusterStartExample = `
  # Start a stopped local cluster:
  educates local cluster start

  # Start the stopped named local cluster test2:
  educates local cluster start --local-cluster test2
`

func (p *ProjectInfo) NewLocalClusterStartCmd() *cobra.Command {
	var c = &cobra.Command{
		Args:    cobra.NoArgs,
		Use:     "start",
		Short:   "Start the local Kubernetes cluster",
		Example: localClusterStartExample,
		RunE: func(cmd *cobra.Command, _ []string) error {
			id, err := selectedEducatesCluster(cmd)
			if err != nil {
				return err
			}
			// Refuse to start a cluster whose listen address another
			// Educates cluster, running or stopped, also binds, or that
			// is on no host interface.
			inventory, err := localClusterInventory(cmd.Context())
			if err != nil {
				return err
			}
			if err := inventory.CheckStart(id); err != nil {
				return err
			}
			return cluster.NewKindClusterConfig("", id).StartCluster()
		},
	}

	addLocalClusterFlag(c)

	return c
}
