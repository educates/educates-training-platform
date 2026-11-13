package cmd

import (
	"context"
	"fmt"

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

  # Delete the local cluster along with the registry, resolver, and mirrors,
  # which are kept while another local cluster still uses them:
  educates local cluster delete --all
`

type LocalClusterDeleteOptions struct {
	Kubeconfig    string
	AllComponents bool
}

// Run deletes the kind cluster of the local cluster id. The cluster's
// config stays in the data home.
//
// With AllComponents it then deletes the host infrastructure every local
// cluster shares, the local registry, the DNS resolver and the registry
// mirrors, but only when no other Educates cluster remains to use them;
// otherwise it keeps them and names the clusters still using them. It
// does so even when the kind cluster was already gone, and still reports
// that.
func (o *LocalClusterDeleteOptions) Run(ctx context.Context, id localclusters.Identity) error {
	c := cluster.NewKindClusterConfig("", id)

	err := c.DeleteCluster()
	if !o.AllComponents || (err != nil && !cluster.IsNotFound(err)) {
		return err
	}

	inventory, inventoryErr := localClusterInventory(ctx)
	if inventoryErr != nil {
		return inventoryErr
	}
	if users := inventory.SharedInfrastructureUsers(id); len(users) > 0 {
		fmt.Printf("Skipped deleting the local registry, DNS resolver and registry mirrors: still in use by %s.\n",
			localclusters.DescribeClusters(users))
		return err
	}

	// Best-effort cleanup: surface each as a step but don't fail the
	// delete if a component is already gone.
	_ = stepOnStdout(false, "delete local registry", "deleted", func(s progress.Step) error {
		return registry.DeleteRegistry(s)
	})
	resolver.DeleteResolver()
	_ = stepOnStdout(false, "delete registry mirrors", "deleted", func(s progress.Step) error {
		return registry.DeleteRegistryMirrors(s)
	})

	return err
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
			return o.Run(cmd.Context(), id)
		},
	}

	c.Flags().BoolVar(
		&o.AllComponents,
		"all",
		false,
		"also delete the image registry, resolver and registry mirrors, unless another local cluster still uses them",
	)
	addLocalClusterFlag(c)

	return c
}
