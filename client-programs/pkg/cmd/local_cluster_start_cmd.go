package cmd

import (
	"github.com/spf13/cobra"

	"github.com/educates/educates-training-platform/client-programs/pkg/cluster"
)

var localClusterStartExample = `
  # Start a stopped local cluster:
  educates local cluster start
`

func (p *ProjectInfo) NewLocalClusterStartCmd() *cobra.Command {
	var c = &cobra.Command{
		Args:    cobra.NoArgs,
		Use:     "start",
		Short:   "Start the local Kubernetes cluster",
		Example: localClusterStartExample,
		RunE: func(_ *cobra.Command, _ []string) error {
			c := cluster.NewKindClusterConfig("", defaultLocalCluster())

			return c.StartCluster()
		},
	}

	return c
}
