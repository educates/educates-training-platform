package cmd

import (
	"github.com/spf13/cobra"

	"github.com/educates/educates-training-platform/client-programs/pkg/cluster"
)

const localClusterStopExample = `
  # Stop the local Kubernetes cluster
  educates local cluster stop
`

func (p *ProjectInfo) NewLocalClusterStopCmd() *cobra.Command {
	var c = &cobra.Command{
		Args:  cobra.NoArgs,
		Use:   "stop",
		Short: "Stops the local Kubernetes cluster",
		RunE: func(_ *cobra.Command, _ []string) error {
			c := cluster.NewKindClusterConfig("")

			return c.StopCluster()
		},
		Example: localClusterStopExample,
	}

	return c
}
