package cmd

import (
	"os"
	"path"
	"regexp"

	"github.com/educates/educates-training-platform/client-programs/pkg/utils"
	"github.com/pkg/errors"
	"github.com/spf13/cobra"
)

const localSecretsRemoveExample = `
  # Remove a secret from the cache
  educates local secrets remove my-secret
`

type LocalSecretsRemoveOptions struct {}

func (o *LocalSecretsRemoveOptions) Run(name string) error {
	var err error
	var matched bool

	if matched, err = regexp.MatchString("^[a-z0-9]([.a-z0-9-]+)?[a-z0-9]$", name); err != nil {
		return errors.Wrapf(err, "regex match on secret name failed")
	}

	if !matched {
		return errors.Errorf("invalid secret name %q", name)
	}

	secretsCacheDir := path.Join(utils.GetEducatesHomeDir(), "secrets")

	secretFilePath := path.Join(secretsCacheDir, name+".yaml")

	os.Remove(secretFilePath)

	return nil
}

func (p *ProjectInfo) NewLocalSecretsRemoveCmd() *cobra.Command {
	var o LocalSecretsRemoveOptions

	var c = &cobra.Command{
		Args:  func(cmd *cobra.Command, args []string) error {
			if len(args) != 1 {
				return utils.CmdError(cmd, "name is required", "NAME")
			}
			return nil
		},
		Use:   "remove NAME",
		Short: "Remove secret from the cache",
		RunE: func(_ *cobra.Command, args []string) error {
			return o.Run(args[0])
		},
		Example: localSecretsRemoveExample,
	}

	return c
}
