package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/educates/educates-training-platform/client-programs/pkg/config"
	"github.com/educates/educates-training-platform/client-programs/pkg/installer"
)

const (
	adminPlatformConfigExample = `
  # Show configuration config for local deployment
  educates admin platform config --local-config

  # Show configuration config for specific config file
  educates admin platform config --config config.yaml

  # Get configuration used to deploy to the current cluster
  educates admin platform config --from-cluster
  educates admin platform config --from-cluster --kubeconfig /path/to/kubeconfig --context my-cluster

  # Get configuration config with different domain (to make copies of the config)
  educates admin platform config --local-config --domain cluster1.dev.educates.io > cluster1-config.yaml
  `
)

type PlatformConfigOptions struct {
	KubeconfigOptions
	Domain      string
	LocalConfig bool
	FromCluster bool
	Verbose     bool
}

func (o *PlatformConfigOptions) Run() error {
	installer := installer.NewInstaller()

	// Validation: Check if domain is set when from-cluster is true
	// TODO: Maybe be able to modify the domain for the from-cluster config as well?
	if o.FromCluster && o.Domain != "" {
		return fmt.Errorf("domain must not be set when from-cluster is true")
	}

	if o.FromCluster {
		config, err := installer.GetConfigFromCluster(o.Kubeconfig, o.Context)
		if err != nil {
			return err
		}
		fmt.Println(config)
	} else {
		fullConfig, err := config.ConfigForLocalClusters("", o.Domain, o.LocalConfig)

		if err != nil {
			return err
		}

		config.PrintConfigToStdout(fullConfig)
	}

	return nil
}

func (p *ProjectInfo) NewAdminPlatformConfigCmd() *cobra.Command {
	var o PlatformConfigOptions

	var c = &cobra.Command{
		Args:  cobra.NoArgs,
		Use:   "config",
		Short: "Show config used when deploying the platform",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return o.Run()
		},
		Example: adminPlatformConfigExample,
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
	c.Flags().StringVar(
		&o.Domain,
		"domain",
		"",
		"wildcard ingress subdomain name for Educates",
	)
	c.Flags().BoolVar(
		&o.Verbose,
		"verbose",
		false,
		"print verbose output",
	)
	c.Flags().BoolVar(
		&o.LocalConfig,
		"local-config",
		false,
		"Use local configuration",
	)
	// TODO: From cluster
	c.Flags().BoolVar(
		&o.FromCluster,
		"from-cluster",
		false,
		"Show the configuration (from the cluster) used when the plaform was deployed",
	)

	c.MarkFlagsMutuallyExclusive("local-config", "from-cluster")
	c.MarkFlagsOneRequired("local-config", "from-cluster")

	return c
}
