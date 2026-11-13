package cmd

import (
	"context"
	"os"

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

	rep := progress.New(os.Stdout, 0, isStdoutTTY(os.Stdout))
	step := rep.Start("delete registry mirror " + o.MirrorName)
	summary, err := func() (string, error) {
		inventory, err := localClusterInventory(context.Background())
		if err != nil {
			return "", err
		}
		// The mirror leaves the registry mirrors file first, so no local
		// cluster created or started later needs it.
		removal, err := inventory.RemoveMirrorRecord(o.MirrorName)
		if err != nil {
			return "", err
		}
		if !removal.HasContainer {
			return removal.Note, nil
		}
		// Every local cluster shares the mirror, so it is unlinked from
		// every node of every one of them.
		if err := registry.DeleteMirrorAndUnlinkFromCluster(mirrorConfig, inventory.RegistryNodes(), step); err != nil {
			return "", err
		}
		return "deleted", nil
	}()
	if err != nil {
		step.Fail(err)
		return err
	}
	step.Done(summary)
	return nil
}

func (p *ProjectInfo) NewLocalMirrorDeleteCmd() *cobra.Command {
	var o LocalMirrorDeleteOptions

	var c = &cobra.Command{
		Args:  exactArgs(1, "mirror name is required", "NAME"),
		Use:   "delete NAME",
		Short: "Deletes the local image registry mirror",
		Long: `Removes the registry mirror from the registry mirrors file, mirrors.yaml in
the data home, so that no local cluster needs it any more, then deletes
the mirror's container, which every local cluster shares, with the images
it cached, and unlinks the mirror from every node of every running local
cluster. The rest of mirrors.yaml is kept as it was.

A mirror the file records that has no container is removed from the file.
A mirror the file does not record that has no container is an error.`,
		RunE:    func(_ *cobra.Command, args []string) error { o.MirrorName = args[0]; return o.Run() },
		Example: localMirrorDeleteExample,
	}

	return c
}
