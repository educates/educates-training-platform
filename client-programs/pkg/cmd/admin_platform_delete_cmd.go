package cmd

import (
	"fmt"

	"github.com/pkg/errors"
	"github.com/spf13/cobra"

	"github.com/educates/educates-training-platform/client-programs/pkg/cluster"
	"github.com/educates/educates-training-platform/client-programs/pkg/config"
	"github.com/educates/educates-training-platform/client-programs/pkg/installer"
)

type PlatformDeleteOptions struct {
	KubeconfigOptions
	Verbose bool
}

const adminPlatformDeleteExample = `
  # Delete Educates and related cluster services from your cluster
  educates admin platform delete

  # Delete Educates and related cluster services from your cluster for a specific cluster
  educates admin platform delete --kubeconfig /path/to/kubeconfig --context my-cluster
`

func (o *PlatformDeleteOptions) Run() error {
	fullConfig := config.NewDefaultInstallationConfig()

	installer := installer.NewInstaller()

	clusterConfig := cluster.NewClusterConfig(o.Kubeconfig, o.Context)

	err := installer.Delete(fullConfig, clusterConfig, o.Verbose)

	if err != nil {
		return errors.Wrap(err, "educates could not be deleted")
	}

	fmt.Println("\nEducates has been deleted succesfully")

	return nil
}

func (p *ProjectInfo) NewAdminPlatformDeleteCmd() *cobra.Command {
	var o PlatformDeleteOptions

	var c = &cobra.Command{
		Args:  cobra.NoArgs,
		Use:   "delete",
		Short: "Delete Educates and related cluster services from your cluster",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return o.Run()
		},
		Example: adminPlatformDeleteExample,
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
		&o.Verbose,
		"verbose",
		false,
		"print verbose output",
	)

	return c
}
