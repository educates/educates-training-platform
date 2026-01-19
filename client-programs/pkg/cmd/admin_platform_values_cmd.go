package cmd

import (
	"fmt"

	"github.com/pkg/errors"
	"github.com/spf13/cobra"

	"github.com/educates/educates-training-platform/client-programs/pkg/config"
	"github.com/educates/educates-training-platform/client-programs/pkg/installer"
)

const (
	adminPlatformValuesExample = `
  # Show configuration values for local deployment
  educates admin platform values --local-config

  # Show configuration values for specific config file
  educates admin platform values --config config.yaml

  # Get configuration used to deploy to the current cluster
  educates admin platform values --from-cluster
  educates admin platform values --from-cluster --kubeconfig /path/to/kubeconfig --context my-cluster

  # Get configuration values using locally built educates package (version latest does the same and skips image resolution)
  educates admin platform values --config config.yaml  --package-repository localhost:5001 --version 0.0.1
  educates admin platform values --config config.yaml  --version latest

  # Get configuration values with different domain (to make copies of the config)
  educates admin platform values --local-config --domain cluster1.dev.educates.io > cluster1-config.yaml
  educates admin platform values --config config.yaml --domain cluster2.dev.educates.io > cluster2-config.yaml
  `
)

type PlatformValuesOptions struct {
	KubeconfigOptions
	Config            string
	Domain            string
	Version           string
	PackageRepository string
	LocalConfig       bool
	FromCluster       bool
	Verbose           bool
}

func (o *PlatformValuesOptions) Run() error {
	installer := installer.NewInstaller()

	if o.FromCluster {
		config, err := installer.GetValuesFromCluster(o.Kubeconfig, o.Context)
		if err != nil {
			return err
		}
		fmt.Println(config)
	} else {
		fullConfig, err := config.ConfigForLocalClusters(o.Config, o.Domain, o.LocalConfig)

		if err != nil {
			return err
		}

		if err := installer.DryRun(o.Version, o.PackageRepository, fullConfig, o.Verbose, true, true); err != nil {
			return errors.Wrap(err, "educates config could not be processed")
		}
	}

	return nil
}

func (p *ProjectInfo) NewAdminPlatformValuesCmd() *cobra.Command {
	var o PlatformValuesOptions

	var c = &cobra.Command{
		Args:  cobra.NoArgs,
		Use:   "values",
		Short: "Show values to be applied when deploying the platform",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if o.LocalConfig {
				o.Config = ""
			}
			return o.Run()
		},
		Example: adminPlatformValuesExample,
	}

	c.Flags().StringVar(
		&o.Config,
		"config",
		"",
		"path to the installation config file for Educates",
	)
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
	c.Flags().StringVar(
		&o.PackageRepository,
		"package-repository",
		p.ImageRepository,
		"image repository hosting package bundles",
	)
	c.Flags().StringVar(
		&o.Version,
		"version",
		p.Version,
		"version to be installed",
	)
	c.Flags().BoolVar(
		&o.LocalConfig,
		"local-config",
		false,
		"Use local configuration. When used, --config and --domain flags are ignored",
	)
	// TODO: From cluster
	c.Flags().BoolVar(
		&o.FromCluster,
		"from-cluster",
		false,
		"Show the configuration (from the cluster) used when the plaform was deployed",
	)

	c.MarkFlagsMutuallyExclusive("local-config", "config", "from-cluster")
	c.MarkFlagsOneRequired("config", "local-config", "from-cluster")

	return c
}
