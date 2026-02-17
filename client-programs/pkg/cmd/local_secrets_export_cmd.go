package cmd

import (
	"os"
	"path"

	"github.com/pkg/errors"
	"github.com/spf13/cobra"

	"github.com/educates/educates-training-platform/client-programs/pkg/utils"
)

const localSecretsExportExample = `
  # Export all secrets in the cache
  educates local secrets export

  # Export multiple secrets from the cache
  educates local secrets export my-secret-1 my-secret-2
`

type LocalSecretsExportOptions struct {}

func (o *LocalSecretsExportOptions) Run(args []string) error {
	secretsCacheDir := path.Join(utils.GetEducatesHomeDir(), "secrets")

	err := os.MkdirAll(secretsCacheDir, os.ModePerm)

	if err != nil {
		return errors.Wrapf(err, "unable to create secrets cache directory")
	}

	err = utils.PrintYamlFilesInDir(secretsCacheDir, args)
	if err != nil {
		return errors.Wrapf(err, "unable to read secrets cache directory")
	}

	return nil
}

func (p *ProjectInfo) NewLocalSecretsExportCmd() *cobra.Command {
	var o LocalSecretsExportOptions

	var c = &cobra.Command{
		Args:  cobra.ArbitraryArgs,
		Use:   "export [NAME]",
		Short: "Export secrets in the cache",
		RunE: func(_ *cobra.Command, args []string) error {
			return o.Run(args)
		},
		Example: localSecretsExportExample,
	}

	return c
}
