package cmd

import (
	"github.com/spf13/cobra"

	"github.com/educates/educates-training-platform/client-programs/pkg/config"
	"github.com/educates/educates-training-platform/client-programs/pkg/resolver"
)

const localResolverUpdateExample = `
  # Update the local DNS resolver
  educates local resolver update

  # Update the local DNS resolver with a custom config
  educates local resolver update --config /path/to/config.yaml

  # Update the local DNS resolver with a custom domain
  educates local resolver update --domain test.educates.io
`

type LocalResolverUpdateOptions struct {
	Config string
	Domain string
}

func (o *LocalResolverUpdateOptions) Run() error {
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

	return resolver.UpdateResolver(fullConfig.ClusterIngress.Domain, fullConfig.LocalDNSResolver.TargetAddress, fullConfig.LocalDNSResolver.ExtraDomains)
}

func (p *ProjectInfo) NewLocalResolverUpdateCmd() *cobra.Command {
	var o LocalResolverUpdateOptions

	var c = &cobra.Command{
		Args:  cobra.NoArgs,
		Use:   "update",
		Short: "Updates the local DNS resolver",
		RunE:  func(_ *cobra.Command, _ []string) error { return o.Run() },
		Example: localResolverUpdateExample,
	}

	c.Flags().StringVar(
		&o.Config,
		"config",
		"",
		"path to the installation config file for Educates",
	)

	return c
}
