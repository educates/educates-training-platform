package cmd

import (
	"github.com/educates/educates-training-platform/client-programs/pkg/cluster"
	"github.com/educates/educates-training-platform/client-programs/pkg/secrets"
	"github.com/pkg/errors"
	"github.com/spf13/cobra"
)

const localSecretsSyncExample = `
  # Sync secrets to the cluster
  educates local secrets sync

  # Sync secrets to the cluster using a custom kubeconfig file and context
  educates local secrets sync --kubeconfig /path/to/kubeconfig --context my-context
`

type LocalSecretsSyncOptions struct {
	KubeconfigOptions
}

func (o *LocalSecretsSyncOptions) Run() error {
	clusterConfig, err := cluster.NewClusterConfigIfAvailable(o.Kubeconfig, o.Context)

	if err != nil {
		return err
	}

	client, err := clusterConfig.GetClient()

	if err != nil {
		return errors.Wrapf(err, "unable to create Kubernetes client")
	}

	return secrets.SyncLocalCachedSecretsToCluster(client)
}

func (p *ProjectInfo) NewLocalSecretsSyncCmd() *cobra.Command {
	var o LocalSecretsSyncOptions

	var c = &cobra.Command{
		Args:  cobra.NoArgs,
		Use:   "sync",
		Short: "Copy secrets to cluster",
		RunE:  func(_ *cobra.Command, _ []string) error { return o.Run() },
		Example: localSecretsSyncExample,
	}

	c.Flags().StringVar(
		&o.Kubeconfig,
		"kubeconfig",
		"",
		"kubeconfig file to use instead of $KUBECONFIG or $HOME/.kube/config",
	)

	c.Flags().StringVar(
		&o.Context,
		"context",
		"",
		"Context to use from Kubeconfig",
	)

	return c
}
