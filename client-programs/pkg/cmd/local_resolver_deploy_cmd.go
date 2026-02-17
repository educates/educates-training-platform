package cmd

import (
	"github.com/spf13/cobra"

	"github.com/educates/educates-training-platform/client-programs/pkg/config"
	"github.com/educates/educates-training-platform/client-programs/pkg/resolver"
)

const localResolverDeployExample = `
  # Deploy the local DNS resolver
  educates local resolver deploy

  # Deploy the local DNS resolver with a custom config
  educates local resolver deploy --config /path/to/config.yaml

  # Deploy the local DNS resolver with a custom domain
  educates local resolver deploy --domain test.educates.io
`

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
		Example: localResolverDeployExample,
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
