package cmd

import (
	"github.com/spf13/cobra"

	"github.com/educates/educates-training-platform/client-programs/pkg/config"
	"github.com/educates/educates-training-platform/client-programs/pkg/resolver"
)

type LocalResolverDeployOptions struct {
	Config string
	Domain string
}

func (o *LocalResolverDeployOptions) Run() error {
	var fullConfig *config.InstallationConfig
	var err error = nil

	if o.Config != "" {
		fullConfig, err = config.NewInstallationConfigFromFile(o.Config)
	} else {
		fullConfig, err = config.NewInstallationConfigFromUserFile()
	}

	if err != nil {
		return err
	}

	if o.Domain != "" {
		fullConfig.ClusterIngress.Domain = o.Domain
	}

	return resolver.DeployResolver(fullConfig.ClusterIngress.Domain, fullConfig.LocalDNSResolver.TargetAddress, fullConfig.LocalDNSResolver.ExtraDomains)
}

func (p *ProjectInfo) NewLocalResolverDeployCmd() *cobra.Command {
	var o LocalResolverDeployOptions

	var c = &cobra.Command{
		Args:  cobra.NoArgs,
		Use:   "deploy",
		Short: "Deploys a local DNS resolver",
		RunE:  func(_ *cobra.Command, _ []string) error { return o.Run() },
	}

	c.Flags().StringVar(
		&o.Config,
		"config",
		"",
		"path to the installation config file for Educates",
	)
	c.Flags().StringVar(
		&o.Domain,
		"domain",
		"",
		"wildcard ingress subdomain name for Educates",
	)

	return c
}
