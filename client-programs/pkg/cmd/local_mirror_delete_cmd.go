package cmd

import (
	"github.com/spf13/cobra"

	"github.com/educates/educates-training-platform/client-programs/pkg/config"
	"github.com/educates/educates-training-platform/client-programs/pkg/registry"
	"github.com/educates/educates-training-platform/client-programs/pkg/utils"
)

const (
	localMirrorDeleteExample = `
  # Delete a local image registry mirror
  educates local mirror delete mymirror
`
)

type LocalMirrorDeleteOptions struct {
	MirrorName string
}

func (o *LocalMirrorDeleteOptions) Run() error {
	mirrorConfig := &config.RegistryMirrorConfig{
		Mirror: o.MirrorName,
	}

	mirror := registry.NewMirror(mirrorConfig)
	return mirror.DeleteAndUnlinkFromCluster()
}

func (p *ProjectInfo) NewLocalMirrorDeleteCmd() *cobra.Command {
	var o LocalMirrorDeleteOptions

	var c = &cobra.Command{
		Args:    func(cmd *cobra.Command, args []string) error {
			if len(args) != 1 {
				return utils.CmdError(cmd, "name is required", "NAME")
			}
			return nil
		},
		Use:     "delete NAME",
		Short:   "Deletes the local image registry mirror",
		RunE:    func(_ *cobra.Command, args []string) error { o.MirrorName = args[0]; return o.Run() },
		Example: localMirrorDeleteExample,
	}

	return c
}
