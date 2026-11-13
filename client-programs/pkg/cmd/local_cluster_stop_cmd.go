package cmd

import (
	"github.com/spf13/cobra"

	"github.com/educates/educates-training-platform/client-programs/pkg/cluster"
)

var localClusterStopExample = `
  # Stop the local cluster without deleting it:
  educates local cluster stop

  # Stop the named local cluster test2:
  educates local cluster stop --local-cluster test2
`

func (p *ProjectInfo) NewLocalClusterStopCmd() *cobra.Command {
	var c = &cobra.Command{
		Args:    cobra.NoArgs,
		Use:     "stop",
		Short:   "Stops the local Kubernetes cluster",
		Example: localClusterStopExample,
		RunE: func(cmd *cobra.Command, _ []string) error {
			id, err := selectedLocalCluster(cmd)
			if err != nil {
				return err
			}
			return cluster.NewKindClusterConfig("", id).StopCluster()
		},
	}

	addLocalClusterFlag(c)

	return c
}
