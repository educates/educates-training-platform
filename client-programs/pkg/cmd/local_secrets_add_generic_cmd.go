package cmd

import (
	"github.com/educates/educates-training-platform/client-programs/pkg/utils"
	"github.com/spf13/cobra"
)

const localSecretsAddGenericExample = `
  # Create a secret from a local file
  educates local secrets add generic my-secret --from-file /path/to/file

  # Create a secret from a local directory
  educates local secrets add generic my-secret --from-literal key=value

  # Create a secret from a local directory
  educates local secrets add generic my-secret --from-literal key=value --as-string
`

type LocalSecretsAddGenericOptions struct {
	FileSources    []string
	LiteralSources []string
	AsString       bool
}

func (o *LocalSecretsAddGenericOptions) Run(name string) error {
	return nil
}

func (p *ProjectInfo) NewLocalSecretsAddGenericCmd() *cobra.Command {
	var o LocalSecretsAddGenericOptions

	var c = &cobra.Command{
		Args: func(cmd *cobra.Command, args []string) error {
			if len(args) != 1 {
				return utils.CmdError(cmd, "name is required", "NAME")
			}
			return nil
		},

		Use:     "generic NAME",
		Short:   "Create a secret from a local file, directory, or literal value",
		RunE:    func(_ *cobra.Command, args []string) error { return o.Run(args[0]) },
		Example: localSecretsAddGenericExample,
	}

	c.Flags().StringArrayVar(
		&o.FileSources,
		"from-file",
		[]string{},
		"Key files can be specified using their file path, in which case a default name will be given to them, or optionally with a name and file path, in which case the given name will be used. Specifying a directory will iterate each named file in the directory that is avalid secret key.",
	)
	c.Flags().StringArrayVar(
		&o.LiteralSources,
		"from-literal",
		[]string{},
		"Specify a key and literal value to insert in secret (i.e. mykey=somevalue)",
	)
	c.Flags().BoolVar(
		&o.AsString,
		"as-string",
		false,
		"use stringData for value",
	)

	return c
}
