package cmd

import (
	"github.com/spf13/cobra"

	"github.com/educates/educates-training-platform/client-programs/pkg/config"
	"github.com/educates/educates-training-platform/client-programs/pkg/resolver"
)

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
	}

	c.Flags().StringVar(
		&o.Config,
		"config",
		"",
		"path to the installation config file for Educates",
	)

	return c
}
