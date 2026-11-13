package cmd

import (
	"context"

	"github.com/spf13/cobra"

	"github.com/educates/educates-training-platform/client-programs/pkg/deployer/progress"
	"github.com/educates/educates-training-platform/client-programs/pkg/registry"
)

var (
	localMirrorDeleteExample = `
  # Delete a local image registry mirror
  educates local mirror delete mymirror
`
)

type LocalMirrorDeleteOptions struct {
	MirrorName string
}

func (o *LocalMirrorDeleteOptions) Run() error {
	mirrorConfig := &registry.MirrorConfig{
		Mirror: o.MirrorName,
	}

	return stepOnStdout(false, "delete registry mirror "+o.MirrorName, "deleted", func(s progress.Step) error {
		// Every local cluster shares the mirror, so it is unlinked from
		// every node of every one of them.
		inventory, err := localClusterInventory(context.Background())
		if err != nil {
			return err
		}
		return registry.DeleteMirrorAndUnlinkFromCluster(mirrorConfig, inventory.RegistryNodes(), s)
	})
}

func (p *ProjectInfo) NewLocalMirrorDeleteCmd() *cobra.Command {
	var o LocalMirrorDeleteOptions

	var c = &cobra.Command{
		Args:    exactArgs(1, "mirror name is required", "NAME"),
		Use:     "delete NAME",
		Short:   "Deletes the local image registry mirror",
		RunE:    func(_ *cobra.Command, args []string) error { o.MirrorName = args[0]; return o.Run() },
		Example: localMirrorDeleteExample,
	}

	return c
}
