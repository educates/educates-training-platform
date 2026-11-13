package cmd

import (
	"context"

	"github.com/pkg/errors"
	"github.com/spf13/cobra"

	"github.com/educates/educates-training-platform/client-programs/pkg/deployer/progress"
	"github.com/educates/educates-training-platform/client-programs/pkg/registry"
)

var (
	localMirrorDeployExample = `
  # Mirror DockerHub anonymously (may be subject to rate limits):
  educates local mirror deploy docker.io

  # Mirror DockerHub with credentials (recommended to avoid throttling):
  educates local mirror deploy docker.io --username <DOCKERHUB_USER> --password <DOCKERHUB_PASS>
  
  # Mirror a private registry:
  educates local mirror deploy myprivateregistry.com --username <USER> --password <PASS>
  
  # Mirror a registry with a different remote URL:
  educates local mirror deploy mymirror --url registry.example.com
`
)

type LocalMirrorDeployOptions struct {
	MirrorName string
	MirrorURL  string
	Username   string
	Password   string
	Verbose    bool
}

func (o *LocalMirrorDeployOptions) Run() error {
	mirrorConfig := &registry.MirrorConfig{
		Mirror:   o.MirrorName,
		URL:      o.MirrorURL,
		Username: o.Username,
		Password: o.Password,
	}

	err := stepOnStdout(o.Verbose, "registry mirror "+o.MirrorName, "ready", func(s progress.Step) error {
		nodes, err := registryNodes(context.Background(), defaultLocalCluster())
		if err != nil {
			return err
		}
		return registry.DeployMirrorAndLinkToCluster(mirrorConfig, nodes, s)
	})

	if err != nil {
		return errors.Wrap(err, "failed to deploy registry mirror")
	}

	return nil
}

func (p *ProjectInfo) NewLocalMirrorDeployCmd() *cobra.Command {
	var o LocalMirrorDeployOptions

	var c = &cobra.Command{
		Args:  exactArgs(1, "mirror name is required", "NAME"),
		Use:   "deploy NAME",
		Short: "Deploys a local image registry mirror",
		RunE: func(cmd *cobra.Command, args []string) error {
			o.MirrorName = args[0]
			return o.Run()
		},
		Example: localMirrorDeployExample,
	}

	c.Flags().StringVar(
		&o.MirrorURL,
		"url",
		"",
		"URL of the registry mirror",
	)

	c.Flags().StringVar(
		&o.Username,
		"username",
		"",
		"Username for the registry mirror",
	)

	c.Flags().StringVar(
		&o.Password,
		"password",
		"",
		"Password for the registry mirror",
	)

	c.Flags().BoolVar(
		&o.Verbose,
		"verbose",
		false,
		"show per-step detail instead of a single collapsed line",
	)

	return c
}
