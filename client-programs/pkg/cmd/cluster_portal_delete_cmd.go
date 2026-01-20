package cmd

import (
	"github.com/educates/educates-training-platform/client-programs/pkg/cluster"
	"github.com/educates/educates-training-platform/client-programs/pkg/constants"
	"github.com/educates/educates-training-platform/client-programs/pkg/educates/resources/portal"
	"github.com/pkg/errors"
	"github.com/spf13/cobra"
)

type ClusterPortalDeleteOptions struct {
	KubeconfigOptions
	Portal string
}

const clusterPortalDeleteExample = `
# Delete TrainingPortal from Educates cluster with default name
educates cluster portal delete

# Delete TrainingPortal from Educates cluster with specific name
educates cluster portal delete --portal=my-portal

# Delete given TrainingPortal from given Educates cluster
educates cluster portal delete --portal=my-portal --kubeconfig ~/.kube/config --context=my-context
`

func (o *ClusterPortalDeleteOptions) Run() error {
	var err error

	// Ensure have portal name.

	if o.Portal == "" {
		o.Portal = constants.DefaultPortalName
	}

	clusterConfig, err := cluster.NewClusterConfigIfAvailable(o.Kubeconfig, o.Context)

	if err != nil {
		return err
	}

	dynamicClient, err := clusterConfig.GetDynamicClient()

	if err != nil {
		return errors.Wrapf(err, "unable to create Kubernetes client")
	}

	config := portal.TrainingPortalDeleteConfig{
		Portal: o.Portal,
	}

	manager := portal.NewPortalManager(dynamicClient)

	err = manager.DeleteTrainingPortal(&config)

	if err != nil {
		return err
	}

	return nil
}

func (p *ProjectInfo) NewClusterPortalDeleteCmd() *cobra.Command {
	var o ClusterPortalDeleteOptions

	var c = &cobra.Command{
		Args:  cobra.NoArgs,
		Use:   "delete",
		Short: "Delete portal from Kubernetes",
		RunE:  func(_ *cobra.Command, _ []string) error { return o.Run() },
		Example: clusterPortalDeleteExample,
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
	c.Flags().StringVarP(
		&o.Portal,
		"portal",
		"p",
		constants.DefaultPortalName,
		"name to be used for training portal and workshop name prefixes",
	)

	return c
}
