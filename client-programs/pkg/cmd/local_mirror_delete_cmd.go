package cmd

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/educates/educates-training-platform/client-programs/pkg/config"
	"github.com/educates/educates-training-platform/client-programs/pkg/deployer/progress"
	"github.com/educates/educates-training-platform/client-programs/pkg/registry"
	"github.com/educates/educates-training-platform/client-programs/pkg/utils"
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
		if err := registry.DeleteMirrorAndUnlinkFromCluster(mirrorConfig, inventory.RegistryNodes(), s); err != nil {
			return err
		}
		// The next create or start of any local cluster creates a mirror
		// the registry mirrors file declares again. The mirror is deleted
		// either way, so a file that does not load is only a warning.
		declared, err := config.LoadLocalMirrors(utils.GetEducatesHomeDir())
		if err != nil {
			s.Warn(fmt.Sprintf("could not tell whether the registry mirrors file declares registry mirror %s: %v",
				o.MirrorName, err))
			return nil
		}
		if warning := inventory.DeletedMirrorWarning(o.MirrorName, declared); warning != "" {
			s.Warn(warning)
		}
		return nil
	})
}

func (p *ProjectInfo) NewLocalMirrorDeleteCmd() *cobra.Command {
	var o LocalMirrorDeleteOptions

	var c = &cobra.Command{
		Args:  exactArgs(1, "mirror name is required", "NAME"),
		Use:   "delete NAME",
		Short: "Deletes the local image registry mirror",
		Long: `Deletes the registry mirror's container, which every local cluster shares,
and unlinks the mirror from every node of every running local cluster.

When the registry mirrors file, mirrors.yaml in the data home, declares
the mirror, the next create or start of any local cluster creates it
again, so the command warns, naming the file to remove the mirror from.`,
		RunE:    func(_ *cobra.Command, args []string) error { o.MirrorName = args[0]; return o.Run() },
		Example: localMirrorDeleteExample,
	}

	return c
}
