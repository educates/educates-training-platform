package cmd

import (
	"github.com/educates/educates-training-platform/client-programs/pkg/cluster"
	"github.com/educates/educates-training-platform/client-programs/pkg/constants"
	"github.com/educates/educates-training-platform/client-programs/pkg/portal"
	"github.com/pkg/errors"
	"github.com/spf13/cobra"
)

type ClusterConfigViewOptions struct {
	KubeconfigOptions
	Portal       string
	Hostname     string
	Repository   string
	Capacity     uint
	Password     string
	ThemeName    string
	CookieDomain string
	Labels       []string
}

const clusterPortalCreateExample = `
# Create TrainingPortal in Educates cluster with default name
educates cluster portal create

# Create TrainingPortal in Educates cluster with specific name
educates cluster portal create --portal=my-portal

# Create TrainingPortal in Educates cluster with specific name and capacity and theme
educates cluster portal create --portal=my-portal --capacity=10 --theme-name=my-theme

# Create given TrainingPortal in given Educates cluster
educates cluster portal create --portal=my-portal --kubeconfig ~/.kube/config --context=my-context
`

func (o *ClusterConfigViewOptions) Run(isPasswordSet bool) error {
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

	config := portal.TrainingPortalCreateConfig{
		Portal: o.Portal,
		Hostname: o.Hostname,
		Repository: o.Repository,
		Capacity: o.Capacity,
		Password: o.Password,
		IsPasswordSet: isPasswordSet,
		ThemeName: o.ThemeName,
		CookieDomain: o.CookieDomain,
		Labels: o.Labels,
	}

	manager := portal.NewPortalManager(dynamicClient)

	err = manager.CreateTrainingPortal(&config)

	if err != nil {
		return err
	}

	return nil
}

func (p *ProjectInfo) NewClusterPortalCreateCmd() *cobra.Command {
	var o ClusterConfigViewOptions

	var c = &cobra.Command{
		Args:  cobra.NoArgs,
		Use:   "create",
		Short: "Create portal in Kubernetes",
		RunE: func(cmd *cobra.Command, _ []string) error {
			isPasswordSet := cmd.Flags().Lookup("password").Changed

			return o.Run(isPasswordSet)
		},
		Example: clusterPortalCreateExample,
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
	c.Flags().StringVar(
		&o.Hostname,
		"hostname",
		"",
		"override hostname for training portal and workshops",
	)
	c.Flags().StringVar(
		&o.Repository,
		"image-repository",
		"",
		"the address of the default image repository",
	)
	c.Flags().UintVar(
		&o.Capacity,
		"capacity",
		constants.DefaultPortalCapacity,
		"maximum number of current sessions for the training portal",
	)
	c.Flags().StringVar(
		&o.Password,
		"password",
		"",
		"override password for training portal access",
	)
	c.Flags().StringVar(
		&o.ThemeName,
		"theme-name",
		"",
		"override theme used by training portal and workshops",
	)
	c.Flags().StringVar(
		&o.CookieDomain,
		"cookie-domain",
		"",
		"override cookie domain used by training portal and workshops",
	)
	c.Flags().StringSliceVarP(
		&o.Labels,
		"labels",
		"l",
		[]string{},
		"label overrides for portal",
	)

	return c
}
