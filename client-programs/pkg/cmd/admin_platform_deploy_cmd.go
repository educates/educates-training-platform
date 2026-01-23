package cmd

import (
	"fmt"

	"github.com/pkg/errors"
	"github.com/spf13/cobra"

	"github.com/educates/educates-training-platform/client-programs/pkg/cluster"
	"github.com/educates/educates-training-platform/client-programs/pkg/config"
	"github.com/educates/educates-training-platform/client-programs/pkg/installer"
	"github.com/educates/educates-training-platform/client-programs/pkg/secrets"
)

const (
	adminPlatformDeployExample = `
  # Deploy educates platform
  educates admin platform deploy --config config.yaml

  # Get deployment descriptors for a specific provider with provided config
  educates admin platform deploy --config config.yaml --dry-run

  # Get deployment descriptors for local cluster default installation
  educates admin platform deploy --local-config --dry-run

  # Deploy educates platform with verbose output
  educates admin platform deploy --config config.yaml --verbose

  # Deploy educates platform with an alternate domain
  educates admin platform deploy --config config.yaml --domain test.educates.io
  educates admin platform deploy --local-config --domain test.educates.io

  # Deploy educates platform without resolving images via kbld (using latest images)
  educates admin platform deploy --config config.yaml --skip-image-resolution

  # Deploy educates platform showing the changes to be applied to the cluster
  educates admin platform deploy --config config.yaml --show-changes

  # Install educates with bundle from different repository
  educates admin platform deploy --config config.yaml --package-repository ghcr.io/jorgemoralespou --version installer-clean

  # Install educates when locally built (version latest does the same and skips image resolution)
  educates admin platform deploy --config config.yaml  --package-repository localhost:5001 --version 0.0.1
  educates admin platform deploy --config config.yaml  --version latest

  # Install educates on a specific cluster
  educates admin platform deploy --config config.yaml --kubeconfig /path/to/kubeconfig --context my-cluster
  educates admin platform deploy --config config.yaml --kubeconfig /path/to/kubeconfig
  educates admin platform deploy --config config.yaml --context my-cluster
  `
)

type PlatformDeployOptions struct {
	KubeconfigOptions
	Config              string
	Domain              string
	DryRun              bool
	Version             string
	PackageRepository   string
	Verbose             bool
	LocalConfig         bool
	skipImageResolution bool
	showChanges         bool
}

func (o *PlatformDeployOptions) Run() error {
	installer := installer.NewInstaller()

	fullConfig, err := config.ConfigForLocalClusters(o.Config, o.Domain, o.LocalConfig)

	if err != nil {
		return err
	}

	if o.DryRun {
		if err = installer.DryRun(o.Version, o.PackageRepository, fullConfig, o.Verbose, false, o.skipImageResolution); err != nil {
			return errors.Wrap(err, "educates could not be installed")
		}
		return nil
	}

	clusterConfig, err := cluster.NewClusterConfigIfAvailable(o.Kubeconfig, o.Context)
	if err != nil {
		return err
	}

	client, err := clusterConfig.GetClient()
	if err != nil {
		return err
	}

	// This creates the educates-secrets namespace if it doesn't exist and creates the
	// wildcard and CA secrets in there
	if err = secrets.SyncLocalCachedSecretsToCluster(client); err != nil {
		return err
	}

	err = installer.Run(o.Version, o.PackageRepository, fullConfig, clusterConfig, o.Verbose, false, o.skipImageResolution, o.showChanges)
	if err != nil {
		return errors.Wrap(err, "educates could not be installed")
	}

	// This is for hugo livereload (educates serve-workshop). Reconfigures the loopback service
	// We do create this loopback service for all providers except vcluster, as vcluster will map
	// it's own service to the host's loopback service to use the host's single loopback service
	if fullConfig.ClusterInfrastructure.Provider != "vcluster" {
		if err = cluster.CreateLoopbackService(client, fullConfig.ClusterIngress.Domain); err != nil {
			return err
		}
	}

	fmt.Println("\n🎓 Educates has been installed succesfully")

	return nil
}

func (p *ProjectInfo) NewAdminPlatformDeployCmd() *cobra.Command {
	var o PlatformDeployOptions

	var c = &cobra.Command{
		Args:  cobra.NoArgs,
		Use:   "deploy",
		Short: "Install Educates and related cluster services onto your cluster in an imperative manner",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if o.LocalConfig {
				o.Config = ""
			}
			return o.Run()
		},
		Example: adminPlatformDeployExample,
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
		&o.DryRun,
		"dry-run",
		false,
		"prints to stdout the yaml that would be deployed to the cluster",
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
	c.Flags().BoolVar(
		&o.skipImageResolution,
		"skip-image-resolution",
		false,
		"skips resolution of referenced images so that all will be fetched from their original location",
	)
	c.Flags().BoolVar(
		&o.showChanges,
		"show-changes",
		false,
		"shows the diffs to be applied to the cluster when running the install",
	)
	c.MarkFlagsMutuallyExclusive("config", "local-config")
	c.MarkFlagsOneRequired("config", "local-config")

	return c
}
