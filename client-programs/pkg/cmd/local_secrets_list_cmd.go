package cmd

import (
	"fmt"
	"os"
	"path"
	"strings"

	"github.com/educates/educates-training-platform/client-programs/pkg/utils"
	"github.com/pkg/errors"
	"github.com/spf13/cobra"
)

const localSecretsListExample = `
  # List all secrets in the cache
  educates local secrets list
`

type LocalSecretsListOptions struct {}

func (o *LocalSecretsListOptions) Run() error {
	secretsCacheDir := path.Join(utils.GetEducatesHomeDir(), "secrets")

	err := os.MkdirAll(secretsCacheDir, os.ModePerm)

	if err != nil {
		return errors.Wrapf(err, "unable to create secrets cache directory")
	}

	files, err := os.ReadDir(secretsCacheDir)

	if err != nil {
		return errors.Wrapf(err, "unable to read secrets cache directory")
	}

	for _, f := range files {
		if strings.HasSuffix(f.Name(), ".yaml") {
			name := strings.TrimSuffix(f.Name(), ".yaml")
			fmt.Println(name)
		}
	}

	return nil
}

func (p *ProjectInfo) NewLocalSecretsListCmd() *cobra.Command {
	var o LocalSecretsListOptions

	var c = &cobra.Command{
		Args:  cobra.NoArgs,
		Use:   "list",
		Short: "List secrets in the cache",
		RunE: func(_ *cobra.Command, _ []string) error {
			return o.Run()
		},
		Example: localSecretsListExample,
	}

	return c
}
