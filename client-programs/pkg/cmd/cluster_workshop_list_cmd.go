package cmd

import (
	"fmt"

	"github.com/educates/educates-training-platform/client-programs/pkg/cluster"
	"github.com/educates/educates-training-platform/client-programs/pkg/constants"
	"github.com/educates/educates-training-platform/client-programs/pkg/educates/resources/workshops"
	"github.com/pkg/errors"
	"github.com/spf13/cobra"
)

const (
	clusterWorkshopListExample = `
  # List Educates workshops deployed to Kubernetes cluster
  educates cluster workshop list

  # List Educates workshops deployed to Kubernetes cluster with a specific portal
  educates cluster workshop list --portal=my-portal

  # List Educates workshops deployed to alternateKubernetes cluster
  educates cluster workshop list --kubeconfig ~/.kube/config --context=my-context
`
)

type ClusterWorkshopsListOptions struct {
	KubeconfigOptions
	Portal string
}

func (o *ClusterWorkshopsListOptions) Run() error {
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

	manager := workshops.NewWorkshopManager(dynamicClient)

	list, err := manager.ListWorkshopResources(&workshops.ListWorkshopResourcesConfig{
		Portal: o.Portal,
	})

	if err != nil {
		return err
	}

	fmt.Println(list)

	return nil
}

func (p *ProjectInfo) NewClusterWorkshopListCmd() *cobra.Command {
	var o ClusterWorkshopsListOptions

	var c = &cobra.Command{
		Args:  cobra.NoArgs,
		Use:   "list",
		Short: "List workshops deployed to Kubernetes",
		RunE:  func(_ *cobra.Command, _ []string) error { return o.Run() },
		Example: clusterWorkshopListExample,
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
