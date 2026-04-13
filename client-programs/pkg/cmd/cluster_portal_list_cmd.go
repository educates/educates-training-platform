package cmd

import (
	"fmt"

	"github.com/educates/educates-training-platform/client-programs/pkg/cluster"
	educatesResources "github.com/educates/educates-training-platform/client-programs/pkg/educates/resources"
	"github.com/pkg/errors"
	"github.com/spf13/cobra"
)

type ClusterPortalListOptions struct {
	KubeconfigOptions
}

const clusterPortalListExample = `
# List TrainingPortals deployed to Educates cluster
educates cluster portal list

# List TrainingPortals deployed to Educaets cluster and save to file
educates cluster portal list --kubeconfig ~/.kube/config --context=my-context
`

func (o *ClusterPortalListOptions) Run() error {
	var err error

	clusterConfig, err := cluster.NewClusterConfigIfAvailable(o.Kubeconfig, o.Context)

	if err != nil {
		return err
	}

	dynamicClient, err := clusterConfig.GetDynamicClient()

	if err != nil {
		return errors.Wrapf(err, "unable to create Kubernetes client")
	}

	manager := educatesResources.NewPortalManager(dynamicClient)

	list, err := manager.ListTrainingPortals(nil)

	if err != nil {
		return err
	}

	fmt.Println(list)

	return nil
}

func (p *ProjectInfo) NewClusterPortalListCmd() *cobra.Command {
	var o ClusterPortalListOptions

	var c = &cobra.Command{
		Args:  cobra.NoArgs,
		Use:   "list",
		Short: "List portals deployed to Kubernetes",
		RunE:  func(_ *cobra.Command, _ []string) error { return o.Run() },
		Example: clusterPortalListExample,
	}

	c.Flags().StringVar(
		&o.Kubeconfig,
		"kubeconfig",
		"",
		"kubeconfig file to use instead of $KUBECONFIG or $HOME/.kube/config",
	)

	c.Flags().StringVar(
		&o.Context,
		"context",
		"",
		"Context to use from Kubeconfig",
	)

	return c
}
