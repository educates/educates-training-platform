package cmd

import (
	"fmt"

	"github.com/educates/educates-training-platform/client-programs/pkg/config"
	"github.com/pkg/errors"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v2"
)

const (
	localConfigViewExample = `
  # View local educates cluster configuration by default. Uses nip.io wildcard domain and Kind as provider config defaults
  educates local config view --config NULL

  # View local educates cluster configuration stored. Will show the default if local config file is empty
  educates local config view

  # View local educates cluster configuration using provided config. If there's secrets for that domain, they will be used
  educates local config view --config config.yaml

  # View local educates cluster configuration using provided domain. If there's secrets for that domain, they will be used
  educates local config view --domain test.example.com
`
)

type LocalConfigViewOptions struct {
	Config string
	Domain string
}

func (o *LocalConfigViewOptions) Run() error {
	fullConfig, err := config.ConfigForLocalClusters(o.Config, o.Domain, true)
	if err != nil {
		return err
	}

	configData, err := yaml.Marshal(&fullConfig)

	if err != nil {
		return errors.Wrap(err, "failed to generate installation config")
	}

	fmt.Print(string(configData))

	return nil
}

func (p *ProjectInfo) NewLocalConfigViewCmd() *cobra.Command {
	var o LocalConfigViewOptions

	var c = &cobra.Command{
		Args:    cobra.NoArgs,
		Use:     "view",
		Short:   "View local configuration",
		Long:    "View local configuration. Uses nip.io wildcard domain and Kind as provider config defaults if not specified",
		RunE:    func(_ *cobra.Command, _ []string) error { return o.Run() },
		Example: localConfigViewExample,
	}

	c.Flags().StringVar(
		&o.Domain,
		"domain",
		"",
		"wildcard ingress subdomain name for Educates",
	)

	c.Flags().StringVar(
		&o.Config,
		"config",
		"",
		"path to the installation config file for Educates",
	)

	return c
}
