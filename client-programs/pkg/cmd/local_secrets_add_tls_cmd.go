package cmd

import (
	"github.com/educates/educates-training-platform/client-programs/pkg/secrets"
	"github.com/educates/educates-training-platform/client-programs/pkg/utils"
	"github.com/spf13/cobra"
)

const localSecretsAddTlsExample = `
  # Create a TLS secret
  educates local secrets add tls my-tls --cert /path/to/cert.pem --key /path/to/key.pem

  # Create a TLS secret with a custom domain
  educates local secrets add tls my-tls --cert /path/to/cert.pem --key /path/to/key.pem --domain my-domain.com

  # Create a TLS secret with a custom domain saved as stringData
  educates local secrets add tls my-tls --cert /path/to/cert.pem --key /path/to/key.pem --domain my-domain.com --as-string
`

type LocalSecretsAddTlsOptions struct {
	CertFile      string
	KeyFile       string
	IngressDomain string
	AsString      bool
}

func (o *LocalSecretsAddTlsOptions) Run(name string) error {
	return secrets.AddTLSSecret(name, o.CertFile, o.KeyFile, o.IngressDomain, o.AsString)
}

func (p *ProjectInfo) NewLocalSecretsAddTlsCmd() *cobra.Command {
	var o LocalSecretsAddTlsOptions

	var c = &cobra.Command{
		Args: func(cmd *cobra.Command, args []string) error {
			if len(args) != 1 {
				return utils.CmdError(cmd, "name is required", "NAME")
			}
			return nil
		},
		Use:     "tls NAME",
		Short:   "Create a TLS secret",
		RunE:    func(_ *cobra.Command, args []string) error { return o.Run(args[0]) },
		Example: localSecretsAddTlsExample,
	}

	c.Flags().StringVar(
		&o.CertFile,
		"cert",
		"",
		"path to PEM encoded public key certificate",
	)
	c.Flags().StringVar(
		&o.KeyFile,
		"key",
		"",
		"path to private key associated with given certificate",
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

	c.MarkFlagsRequiredTogether("cert", "key")

	return c
}
