package cmd

import (
	"github.com/spf13/cobra"

	"github.com/educates/educates-training-platform/client-programs/pkg/cluster"
	"github.com/educates/educates-training-platform/client-programs/pkg/registry"
	"github.com/educates/educates-training-platform/client-programs/pkg/resolver"
)

const localClusterDeleteExample = `
  # Delete the local Kubernetes cluster
  educates local cluster delete

  # Delete the local Kubernetes cluster and all components (registry, mirrors and resolver)
  educates local cluster delete --all
`

type LocalClusterDeleteOptions struct {
	Kubeconfig    string
	AllComponents bool
}

func (o *LocalClusterDeleteOptions) Run() error {
	c := cluster.NewKindClusterConfig("")

	if o.AllComponents {
		registry.DeleteRegistry()
		resolver.DeleteResolver()
		// Delete all mirrors
		registry.DeleteRegistryMirrors()
	}

	return c.DeleteCluster()
}

func (p *ProjectInfo) NewLocalClusterDeleteCmd() *cobra.Command {
	var o LocalClusterDeleteOptions

	var c = &cobra.Command{
		Args:  cobra.NoArgs,
		Use:   "delete",
		Short: "Deletes the local Kubernetes cluster",
		RunE:  func(_ *cobra.Command, _ []string) error { return o.Run() },
		Example: localClusterDeleteExample,
	}

	c.Flags().BoolVar(
		&o.AllComponents,
		"all",
		false,
		"delete everything, including image registry and resolver",
	)

	return c
}
