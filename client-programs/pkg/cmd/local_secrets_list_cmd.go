package cmd

import (
	"fmt"

	"github.com/educates/educates-training-platform/client-programs/pkg/secrets"
	"github.com/spf13/cobra"
)

const localSecretsListExample = `
  # List all secrets in the cache
  educates local secrets list
`

type LocalSecretsListOptions struct{}

func (o *LocalSecretsListOptions) Run() error {
	list, _ := secrets.List()

	fmt.Println(list)

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
