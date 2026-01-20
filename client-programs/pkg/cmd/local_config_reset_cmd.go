package cmd

import (
	"os"
	"path"

	"github.com/educates/educates-training-platform/client-programs/pkg/utils"
	"github.com/spf13/cobra"
)

const localConfigResetExample = `
  # Reset the local configuration
  educates local config reset
`

type LocalConfigResetOptions struct {}

func (o *LocalConfigResetOptions) Run() error {
	// TODO: Move "values.yaml" to a constant
	valuesFile := path.Join(utils.GetEducatesHomeDir(), "values.yaml")

	os.Remove(valuesFile)

	return nil
}

func (p *ProjectInfo) NewLocalConfigResetCmd() *cobra.Command {
	var o LocalConfigResetOptions

	var c = &cobra.Command{
		Args:  cobra.NoArgs,
		Use:   "reset",
		Short: "Reset local configuration",
		RunE: func(_ *cobra.Command, _ []string) error {
			return o.Run()
		},
		Example: localConfigResetExample,
	}

	return c
}
