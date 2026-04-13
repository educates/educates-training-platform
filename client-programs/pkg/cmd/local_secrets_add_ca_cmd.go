package cmd

import (
	"github.com/educates/educates-training-platform/client-programs/pkg/secrets"
	"github.com/educates/educates-training-platform/client-programs/pkg/utils"
	"github.com/spf13/cobra"
)

const localSecretsAddCaExample = `
  # Create a CA secret
  educates local secrets add ca my-ca

  # Create a CA secret with a custom domain
  educates local secrets add ca my-ca --domain my-domain.com

  # Create a CA secret with a custom certificate file
  educates local secrets add ca my-ca --cert /path/to/ca.crt

  # Create a CA secret with a custom certificate file saved as stringData
  educates local secrets add ca my-ca --cert /path/to/ca.crt --as-string
`

type LocalSecretsAddCaOptions struct {
	CertFile      string
	IngressDomain string
	AsString      bool
}

func (o *LocalSecretsAddCaOptions) Run(name string) error {
	return secrets.AddCASecret(name, o.CertFile, o.IngressDomain, o.AsString)
}

func (p *ProjectInfo) NewLocalSecretsAddCaCmd() *cobra.Command {
	var o LocalSecretsAddCaOptions

	var c = &cobra.Command{
		Args: func(cmd *cobra.Command, args []string) error {
			if len(args) != 1 {
				return utils.CmdError(cmd, "name is required", "NAME")
			}
			return nil
		},
		Use:     "ca NAME",
		Short:   "Create a CA secret",
		RunE:    func(_ *cobra.Command, args []string) error { return o.Run(args[0]) },
		Example: localSecretsAddCaExample,
	}

	c.Flags().StringVar(
		&o.CertFile,
		"cert",
		"",
		"path to PEM encoded CA certificate",
	)
	c.Flags().StringVar(
		&o.IngressDomain,
		"domain",
		"",
		"wildcard ingress domain matching certificate",
	)
	c.Flags().BoolVar(
		&o.AsString,
		"as-string",
		false,
		"use stringData for value",
	)
	c.MarkFlagsRequiredTogether("cert")

	return c
}
