package cmd

import (
	"context"

	"github.com/pkg/errors"
	"github.com/spf13/cobra"

	"github.com/educates/educates-training-platform/client-programs/pkg/config/v1alpha1"
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
	requested := v1alpha1.RegistryMirror{
		Mirror:   o.MirrorName,
		URL:      o.MirrorURL,
		Username: o.Username,
		Password: o.Password,
	}

	err := stepOnStdout(o.Verbose, "registry mirror "+o.MirrorName, "ready", func(s progress.Step) error {
		inventory, err := localClusterInventory(context.Background())
		if err != nil {
			return err
		}
		// The mirror is recorded in the registry mirrors file before its
		// container is created, so every local cluster created or started
		// later needs it, and gets it back should the container go.
		record, err := inventory.RecordMirror(requested)
		if err != nil {
			return err
		}
		// Every local cluster shares the mirror, so it is linked into every
		// node of every one of them.
		mirrorConfig := registryMirrorFromConfig(record.Mirror)
		if err := registry.DeployMirrorAndLinkToCluster(&mirrorConfig, inventory.RegistryNodes(), s); err != nil {
			return err
		}
		if record.Note != "" {
			s.Warn(record.Note)
		}
		return nil
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
		Long: `Records the registry mirror in the registry mirrors file, mirrors.yaml in
the data home, creating the file when it is missing, then deploys the
mirror's container, which every local cluster shares, and links the mirror
into every node of every running local cluster. Every local cluster
created or started later needs the mirrors the file records, so the
mirror stays until 'educates local mirror delete' deletes it.

A mirror the file records with another --url or --username is refused:
delete it first. One it records with the same URL and username, deployed
with another --password, has the new password recorded, but its existing
container keeps the password it was created with until the mirror is
deleted and deployed again. Without --password, the recorded password is
kept.`,
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
