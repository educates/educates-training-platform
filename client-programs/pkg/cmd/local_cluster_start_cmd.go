package cmd

import (
	"github.com/spf13/cobra"

	"github.com/educates/educates-training-platform/client-programs/pkg/cluster"
)

const localClusterStartExample = `
  # Start the local Kubernetes cluster
  educates local cluster start
`

func (p *ProjectInfo) NewLocalClusterStartCmd() *cobra.Command {
	var c = &cobra.Command{
		Args:  cobra.NoArgs,
		Use:   "start",
		Short: "Start the local Kubernetes cluster",
		RunE: func(_ *cobra.Command, _ []string) error {
			c := cluster.NewKindClusterConfig("")

			return c.StartCluster()
		},
		Example: localClusterStartExample,
	}

	return c
}
