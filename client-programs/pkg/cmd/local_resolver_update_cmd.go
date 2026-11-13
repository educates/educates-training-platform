package cmd

import (
	"context"

	"github.com/spf13/cobra"

	"github.com/educates/educates-training-platform/client-programs/pkg/resolver"
)

var localResolverUpdateExample = `
  # Update the local DNS resolver from the configs of every local cluster:
  educates local resolver update

  # Update using a specific config file for the default local cluster:
  educates local resolver update --config config.yaml
`

type LocalResolverUpdateOptions struct {
	Config      string
	LocalConfig bool
}

func (o *LocalResolverUpdateOptions) Run(ctx context.Context) error {
	def, err := defaultResolverConfig(o.Config, "")
	if err != nil {
		return err
	}
	content, err := localResolverConfig(ctx, def)
	if err != nil {
		return err
	}
	return resolver.UpdateResolver(content)
}

func (p *ProjectInfo) NewLocalResolverUpdateCmd() *cobra.Command {
	var o LocalResolverUpdateOptions

	c := &cobra.Command{
		Args:    cobra.NoArgs,
		Use:     "update",
		Short:   "Updates the local DNS resolver (macOS)",
		Long:    "Serves the current configs from the running local DNS resolver, and restarts it.\n\n" + localResolverLong,
		Example: localResolverUpdateExample,
		RunE:    func(cmd *cobra.Command, _ []string) error { return o.Run(cmd.Context()) },
	}
	c.Flags().StringVarP(&o.Config, "config", "c", "", "path to a CLI config file (any kind) to serve the default local cluster from")
	c.Flags().BoolVar(&o.LocalConfig, "local-config", false, "serve the default local cluster from <data-home>/config.yaml (default when --config is not given)")
	c.MarkFlagsMutuallyExclusive("config", "local-config")
	return c
}
