package cmd

import (
	"github.com/spf13/cobra"

	"github.com/educates/educates-training-platform/client-programs/pkg/resolver"
)

const localResolverDeleteExample = `
  # Delete the local DNS resolver
  educates local resolver delete
`

func (p *ProjectInfo) NewLocalResolverDeleteCmd() *cobra.Command {
	var c = &cobra.Command{
		Args:  cobra.NoArgs,
		Use:   "delete",
		Short: "Deletes the local DNS resolver",
		RunE:  func(_ *cobra.Command, _ []string) error { return resolver.DeleteResolver() },
		Example: localResolverDeleteExample,
	}

	return c
}
