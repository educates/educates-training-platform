package cmd

import (
	"fmt"

	"github.com/pkg/errors"
	"github.com/spf13/cobra"

	"github.com/vmware-tanzu-labs/educates-training-platform/client-programs/pkg/cluster"
	"github.com/vmware-tanzu-labs/educates-training-platform/client-programs/pkg/config"
	"github.com/vmware-tanzu-labs/educates-training-platform/client-programs/pkg/installer"
)

type AdminInstallOptions struct {
	Delete             bool
	Config             string
	Kubeconfig         string
	Provider           string
	DryRun             bool
	ShowPackagesValues bool
	Version            string
	PackageRepository  string
	Verbose            bool
}

func (o *AdminInstallOptions) Run() error {
	fullConfig, err := config.NewInstallationConfigFromFile(o.Config)

	if err != nil {
		return err
	}

	// This can be set in the config file if not provided via the command line
	if o.Provider != "" {
		fullConfig.ClusterInfrastructure.Provider = o.Provider
	}

	// Although ytt does some schema validation, we do some basic validation here
	if !validateProvider(fullConfig.ClusterInfrastructure.Provider) {
		return errors.New("Invalid ClusterInsfrastructure Provider. Valid values are (eks, kind, custom)")
	}

	clusterConfig := cluster.NewClusterConfig(o.Kubeconfig)

	installer := installer.NewInstaller()

	if o.Delete {
		err := installer.Delete(fullConfig, clusterConfig, o.Verbose, o.ShowPackagesValues)
		if err != nil {
			return errors.Wrap(err, "educates could not be deleted")
		}
		fmt.Println("\nEducates has been deleted succesfully")
	} else {
		err := installer.Run(o.Version, o.PackageRepository, fullConfig, clusterConfig, o.DryRun, o.Verbose, o.ShowPackagesValues)
		if err != nil {
			return errors.Wrap(err, "educates could not be installed")
		}
		if !o.DryRun && !o.ShowPackagesValues {
			fmt.Println("\nEducates has been installed succesfully")
		}
	}

	return nil
}

func validateProvider(s string) bool {
	if s == "eks" || s == "kind" || s == "custom" {
		return true
	} else {
		return false
	}
}

func (p *ProjectInfo) NewAdminInstallCmd() *cobra.Command {
	var o AdminInstallOptions

	var c = &cobra.Command{
		Args:  cobra.NoArgs,
		Use:   "install",
		Short: "Install Educates and related cluster services onto your cluster in an imperative manner",
		RunE:  func(_ *cobra.Command, _ []string) error { return o.Run() },
	}

	c.Flags().BoolVar(
		&o.Delete,
		"delete",
		false,
		"Should educates be deleted",
	)
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
		&o.Provider,
		"provider",
		"",
		"infastructure provider deployment is being made to",
	)
	// TODO: Should we add domain (like admin_platform_deploy) or is it not needed?
	// c.Flags().StringVar(
	// 	&o.Domain,
	// 	"domain",
	// 	"",
	// 	"wildcard ingress subdomain name for Educates",
	// )
	c.Flags().BoolVar(
		&o.DryRun,
		"dry-run",
		false,
		"prints to stdout the yaml that would be deployed to the cluster",
	)
	c.Flags().BoolVar(
		&o.ShowPackagesValues,
		"show-packages-values",
		false,
		"prints values that will be passed to ytt to deploy all educates packages into the cluster",
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

	// c.MarkFlagRequired("provider")

	return c
}