package cmd

import (
	"github.com/spf13/cobra"

	"github.com/educates/educates-training-platform/client-programs/pkg/registry"
)

const localRegistryDeleteExample = `
  # Delete the local image registry
  educates local registry delete
`

func (p *ProjectInfo) NewLocalRegistryDeleteCmd() *cobra.Command {
	var c = &cobra.Command{
		Args:  cobra.NoArgs,
		Use:   "delete",
		Short: "Deletes the local image registry",
		RunE:  func(_ *cobra.Command, _ []string) error { return registry.DeleteRegistry() },
		Example: localRegistryDeleteExample,
	}

	return c
}
