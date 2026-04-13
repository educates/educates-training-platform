package cmd

import (
	"github.com/educates/educates-training-platform/client-programs/pkg/config"
	"github.com/spf13/cobra"
)

const localConfigEditExample = `
  # Edit the local configuration
  educates local config edit
`

type LocalConfigEditOptions struct{}

func (o *LocalConfigEditOptions) Run() error {
	c := config.LocalConfigEditConfig{}
	return c.Edit()
}

func (p *ProjectInfo) NewLocalConfigEditCmd() *cobra.Command {
	var o LocalConfigEditOptions

	var c = &cobra.Command{
		Args:  cobra.NoArgs,
		Use:   "edit",
		Short: "Edit local configuration",
		RunE: func(_ *cobra.Command, _ []string) error {
			return o.Run()
		},
		Example: localConfigEditExample,
	}

	return c
}
