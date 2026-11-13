package cmd

import (
	"github.com/spf13/cobra"

	"github.com/educates/educates-training-platform/client-programs/pkg/cluster"
	"github.com/educates/educates-training-platform/client-programs/pkg/deployer/progress"
	"github.com/educates/educates-training-platform/client-programs/pkg/registry"
	"github.com/educates/educates-training-platform/client-programs/pkg/resolver"
)

var localClusterDeleteExample = `
  # Delete the local cluster:
  educates local cluster delete

  # Delete the local cluster along with the registry, resolver, and mirrors:
  educates local cluster delete --all
`

type LocalClusterDeleteOptions struct {
	Kubeconfig    string
	AllComponents bool
}

func (o *LocalClusterDeleteOptions) Run() error {
	c := cluster.NewKindClusterConfig("", defaultLocalCluster())

	if o.AllComponents {
		// Best-effort cleanup: surface each as a step but don't abort the
		// cluster delete if a component is already gone.
		_ = stepOnStdout(false, "delete local registry", "deleted", func(s progress.Step) error {
			return registry.DeleteRegistry(s)
		})
		resolver.DeleteResolver()
		_ = stepOnStdout(false, "delete registry mirrors", "deleted", func(s progress.Step) error {
			return registry.DeleteRegistryMirrors(s)
		})
	}

	return c.DeleteCluster()
}

func (p *ProjectInfo) NewLocalClusterDeleteCmd() *cobra.Command {
	var o LocalClusterDeleteOptions

	var c = &cobra.Command{
		Args:    cobra.NoArgs,
		Use:     "delete",
		Short:   "Deletes the local Kubernetes cluster",
		Example: localClusterDeleteExample,
		RunE:    func(_ *cobra.Command, _ []string) error { return o.Run() },
	}

	c.Flags().BoolVar(
		&o.AllComponents,
		"all",
		false,
		"delete everything, including image registry and resolver",
	)

	return c
}
