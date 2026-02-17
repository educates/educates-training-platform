package cmd

import (
	"fmt"

	"github.com/pkg/errors"
	"github.com/spf13/cobra"

	"github.com/educates/educates-training-platform/client-programs/pkg/registry"
)

const (
	localMirroListExample = `
  # List all local image registry mirrors
  educates local mirror list
`
)

type LocalMirrorListOptions struct {}

func (o *LocalMirrorListOptions) Run() error {
	list, err := registry.ListRegistryMirrors()

	if err != nil {
		return errors.Wrap(err, "failed to deploy registry mirror")
	}

	fmt.Println(list)

	return nil
}

func (p *ProjectInfo) NewLocalMirrorListCmd() *cobra.Command {
	var o LocalMirrorListOptions

	var c = &cobra.Command{
		Args:  cobra.NoArgs,
		Use:   "list",
		Short: "List all local image registry mirrors",
		RunE: func(_ *cobra.Command, _ []string) error {
			return o.Run()
		},
		Example: localMirroListExample,
	}

	return c
}
