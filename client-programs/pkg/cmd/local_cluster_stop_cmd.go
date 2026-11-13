package cmd

import (
	"github.com/spf13/cobra"

	"github.com/educates/educates-training-platform/client-programs/pkg/cluster"
)

var localClusterStopExample = `
  # Stop the local cluster without deleting it:
  educates local cluster stop
`

func (p *ProjectInfo) NewLocalClusterStopCmd() *cobra.Command {
	var c = &cobra.Command{
		Args:    cobra.NoArgs,
		Use:     "stop",
		Short:   "Stops the local Kubernetes cluster",
		Example: localClusterStopExample,
		RunE: func(_ *cobra.Command, _ []string) error {
			c := cluster.NewKindClusterConfig("", defaultLocalCluster())

			return c.StopCluster()
		},
	}

	return c
}
