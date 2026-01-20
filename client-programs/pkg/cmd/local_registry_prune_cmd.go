package cmd

import (
	"github.com/pkg/errors"
	"github.com/spf13/cobra"

	"github.com/educates/educates-training-platform/client-programs/pkg/registry"
)

const localRegistryPruneExample = `
  # Prune the local image registry
  educates local registry prune
`

type LocalRegistryPruneOptions struct {
}

func (o *LocalRegistryPruneOptions) Run() error {
	err := registry.PruneRegistry()

	if err != nil {
		return errors.Wrap(err, "failed to prune registry")
	}

	return nil
}

func (p *ProjectInfo) NewLocalRegistryPruneCmd() *cobra.Command {
	var o LocalRegistryPruneOptions

	var c = &cobra.Command{
		Args:  cobra.NoArgs,
		Use:   "prune",
		Short: "Prunes the local image registry (deletes any untagged image)",
		RunE:  func(_ *cobra.Command, _ []string) error { return o.Run() },
		Example: localRegistryPruneExample,
	}

	return c
}
