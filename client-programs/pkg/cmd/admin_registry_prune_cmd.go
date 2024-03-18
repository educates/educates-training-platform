package cmd

import (
	"github.com/pkg/errors"
	"github.com/spf13/cobra"

	"github.com/vmware-tanzu-labs/educates-training-platform/client-programs/pkg/registry"
)

type AdminRegistryPruneOptions struct {
}

func (o *AdminRegistryPruneOptions) Run() error {
	err := registry.PruneRegistry()

	if err != nil {
		return errors.Wrap(err, "failed to prune registry")
	}

	return nil
}

func (p *ProjectInfo) NewAdminRegistryPruneCmd() *cobra.Command {
	var o AdminRegistryPruneOptions

	var c = &cobra.Command{
		Args:  cobra.NoArgs,
		Use:   "prune",
		Short: "Prunes the local image registry (deletes any untagged image)",
		RunE:  func(_ *cobra.Command, _ []string) error { return o.Run() },
	}

	return c
}
