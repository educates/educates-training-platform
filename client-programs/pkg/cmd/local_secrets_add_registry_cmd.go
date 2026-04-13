package cmd

import (
	"github.com/educates/educates-training-platform/client-programs/pkg/secrets"
	"github.com/educates/educates-training-platform/client-programs/pkg/utils"
	"github.com/spf13/cobra"
)

const localSecretsAddDockerRegistryExample = `
  # Create a secret for use with Docker hub
  educates local secrets add docker-registry my-registry --docker-username my-username --docker-password my-password --docker-email my-email

  # Create a secret for use with GitHub Container Registry
  educates local secrets add docker-registry my-registry --docker-server https://ghcr.io --docker-username my-username --docker-password my-password --docker-email my-email

  # Create a secret for use with GitHub Container Registry saved as stringData
  educates local secrets add docker-registry my-registry --docker-server https://ghcr.io --docker-username my-username --docker-password my-password --docker-email my-email --as-string
`

type LocalSecretsAddDockerRegistryOptions struct {
	Server   string
	Username string
	Password string
	Email    string
	AsString bool
}

func (o *LocalSecretsAddDockerRegistryOptions) Run(name string) error {
	return secrets.AddRegistrySecret(name, o.Server, o.Username, o.Password, o.Email, o.AsString)
}

func (p *ProjectInfo) NewLocalSecretsAddDockerRegistryCmd() *cobra.Command {
	var o LocalSecretsAddDockerRegistryOptions

	var c = &cobra.Command{
		Args: func(cmd *cobra.Command, args []string) error {
			if len(args) != 1 {
				return utils.CmdError(cmd, "name is required", "NAME")
			}
			return nil
		},
		Use:     "docker-registry NAME",
		Short:   "Create a secret for use with a Docker registry",
		RunE:    func(_ *cobra.Command, args []string) error { return o.Run(args[0]) },
		Example: localSecretsAddDockerRegistryExample,
	}

	c.Flags().StringVar(
		&o.Server,
		"docker-server",
		"https://index.docker.io/v1/",
		"server location for docker registry",
	)
	c.Flags().StringVar(
		&o.Username,
		"docker-username",
		"",
		"username for docker registry authentication",
	)
	c.Flags().StringVar(
		&o.Password,
		"docker-password",
		"",
		"password for docker registry authentication",
	)
	c.Flags().StringVar(
		&o.Email,
		"docker-email",
		"",
		"email for docker registry",
	)
	c.Flags().BoolVar(
		&o.AsString,
		"as-string",
		false,
		"use stringData for value",
	)

	c.MarkFlagsRequiredTogether("docker-username", "docker-password", "docker-email")

	return c
}
