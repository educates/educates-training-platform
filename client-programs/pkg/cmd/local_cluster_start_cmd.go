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
			id, err := selectedLocalCluster(cmd)
			if err != nil {
				return err
			}
			return cluster.NewKindClusterConfig("", id).StartCluster()
		},
	}

	addLocalClusterFlag(c)

	return c
}
