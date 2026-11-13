package cmd

import (
	"context"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/educates/educates-training-platform/client-programs/pkg/cluster"
	"github.com/educates/educates-training-platform/client-programs/pkg/deployer/progress"
	"github.com/educates/educates-training-platform/client-programs/pkg/localclusters"
	"github.com/educates/educates-training-platform/client-programs/pkg/registry"
	"github.com/educates/educates-training-platform/client-programs/pkg/resolver"
)

var localClusterDeleteExample = `
  # Delete the local cluster:
  educates local cluster delete

  # Delete the named local cluster test2, keeping its config:
  educates local cluster delete --local-cluster test2

  # Delete the local cluster along with the registry, resolver, and mirrors:
  educates local cluster delete --all
`

type LocalClusterDeleteOptions struct {
	Kubeconfig    string
	AllComponents bool
}

// Run deletes the kind cluster of the local cluster id. The cluster's
// config stays in the data home.
func (o *LocalClusterDeleteOptions) Run(id localclusters.Identity) error {
	c := cluster.NewKindClusterConfig("", id)

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

	if err := c.DeleteCluster(); err != nil {
		return err
	}

	// A running local DNS resolver stops serving the domains of a named
	// cluster once its kind cluster is gone.
	rep := progress.New(os.Stdout, 0, isStdoutTTY(os.Stdout))
	if err := refreshLocalResolver(rep, func() ([]byte, error) {
		return localResolverConfig(context.Background())
	}); err != nil {
		return fmt.Errorf("local DNS resolver: %w", err)
	}
	return nil
}

func (p *ProjectInfo) NewLocalClusterDeleteCmd() *cobra.Command {
	var o LocalClusterDeleteOptions

	var c = &cobra.Command{
		Args:    cobra.NoArgs,
		Use:     "delete",
		Short:   "Deletes the local Kubernetes cluster",
		Example: localClusterDeleteExample,
		RunE: func(cmd *cobra.Command, _ []string) error {
			id, err := selectedLocalCluster(cmd)
			if err != nil {
				return err
			}
			return o.Run(id)
		},
	}

	c.Flags().BoolVar(
		&o.AllComponents,
		"all",
		false,
		"delete everything, including image registry and resolver",
	)
	addLocalClusterFlag(c)

	return c
}
