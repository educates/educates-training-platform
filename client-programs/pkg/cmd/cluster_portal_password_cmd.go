package cmd

import (
	"fmt"

	"github.com/educates/educates-training-platform/client-programs/pkg/cluster"
	"github.com/educates/educates-training-platform/client-programs/pkg/constants"
	"github.com/educates/educates-training-platform/client-programs/pkg/educates/resources/portal"
	"github.com/pkg/errors"
	"github.com/spf13/cobra"
)

type ClusterPortalPasswordOptions struct {
	KubeconfigOptions
	Admin  bool
	Portal string
}

const clusterPortalPasswordExample = `
# View accesspassword for TrainingPortal in Educates cluster with default name
educates cluster portal password

# View access password for TrainingPortal in Educates cluster with specific name
educates cluster portal password --portal=my-portal

# View admin password for TrainingPortal in Educates cluster with default name
educates cluster portal password --admin

# View access password for given TrainingPortal in given Educates cluster
educates cluster portal password --portal=my-portal --kubeconfig ~/.kube/config --context=my-context --admin
`

func (o *ClusterPortalPasswordOptions) Run() error {
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

	config := portal.TrainingPortalPasswordConfig{
		Portal: o.Portal,
		Admin: o.Admin,
	}

	manager := portal.NewPortalManager(dynamicClient)

	password, err := manager.GetTrainingPortalPassword(&config)

	if err != nil {
		return err
	}

	fmt.Println(password)

	return nil
}

func (p *ProjectInfo) NewClusterPortalPasswordCmd() *cobra.Command {
	var o ClusterPortalPasswordOptions

	var c = &cobra.Command{
		Args:  cobra.NoArgs,
		Use:   "password",
		Short: "View portal credentials in Kubernetes",
		RunE:  func(_ *cobra.Command, _ []string) error { return o.Run() },
		Example: clusterPortalPasswordExample,
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
	c.Flags().BoolVar(
		&o.Admin,
		"admin",
		false,
		"view admin password for admin pages rather than access code",
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
