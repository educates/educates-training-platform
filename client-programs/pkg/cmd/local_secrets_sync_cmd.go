package cmd

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"

	"github.com/educates/educates-training-platform/client-programs/pkg/cluster"
	"github.com/educates/educates-training-platform/client-programs/pkg/config"
	"github.com/educates/educates-training-platform/client-programs/pkg/config/v1alpha1"
	"github.com/educates/educates-training-platform/client-programs/pkg/localcerts"
	"github.com/educates/educates-training-platform/client-programs/pkg/localclusters"
	"github.com/educates/educates-training-platform/client-programs/pkg/secrets"
	"github.com/educates/educates-training-platform/client-programs/pkg/utils"
	pkgerrors "github.com/pkg/errors"
	"github.com/spf13/cobra"
)

var localSecretsSyncExample = `
  # Copy the cached secrets into the cluster:
  educates local secrets sync

  # Copy cached secrets into a specific cluster context:
  educates local secrets sync --context mycluster

  # Copy the cached secrets the named local cluster test2 needs into it:
  educates local secrets sync --local-cluster test2
`

type LocalSecretsSyncOptions struct {
	KubeconfigOptions
}

// Run pushes the cached Secrets the local certificate resolution step
// selects for the local cluster id into the cluster. CA root key pairs
// are never pushed; a CA key pair is pushed only when the config of the
// local cluster selects it, and is minted first when it is missing or
// stale. When the certificates of that config cannot be decided, it warns
// and pushes every Secret that holds no CA key.
func (o *LocalSecretsSyncOptions) Run(w io.Writer, id localclusters.Identity) error {
	plan, err := resolveLocalSecretsSync(id)

	if err != nil {
		return err
	}

	clusterConfig, err := cluster.NewClusterConfigIfAvailable(o.Kubeconfig, o.Context)

	if err != nil {
		return err
	}

	client, err := clusterConfig.GetClient()

	if err != nil {
		return pkgerrors.Wrapf(err, "unable to create Kubernetes client")
	}

	if err := plan.Commit(lineReporter{w}); err != nil {
		return err
	}

	return secrets.SyncLocalCachedSecretsToCluster(client, plan.SyncSecretNames)
}

// resolveLocalSecretsSync resolves what local secrets sync pushes for the
// local cluster id, from its config in the data home. The default local
// cluster may have no config. A named local cluster's config follows the
// rules of localclusters.LoadNamedConfig, so one with no ingress.domain
// takes the nip.io name of its listen address and is served over plain
// HTTP, and a missing one is an error that says how to write it.
func resolveLocalSecretsSync(id localclusters.Identity) (*localcerts.Plan, error) {
	var cfg *v1alpha1.EducatesLocalConfig
	if id.IsDefault() {
		local, err := config.LoadLocalConfigIfPresent(utils.GetEducatesHomeDir())
		if err != nil {
			return nil, err
		}
		cfg = local
	} else {
		if _, err := os.Stat(id.ConfigPath); errors.Is(err, fs.ErrNotExist) {
			return nil, localclusters.MissingNamedConfigError(id)
		}
		loaded, err := localclusters.LoadNamedConfig(id.ConfigPath)
		if err != nil {
			return nil, err
		}
		cfg, _ = loaded.(*v1alpha1.EducatesLocalConfig)
	}
	return localcerts.ResolveSync(cfg)
}

// lineReporter is a localcerts.Reporter that prints each note as a line.
type lineReporter struct{ w io.Writer }

func (r lineReporter) Note(msg string) {
	fmt.Fprintln(r.w, msg)
}

func (p *ProjectInfo) NewLocalSecretsSyncCmd() *cobra.Command {
	var o LocalSecretsSyncOptions

	var c = &cobra.Command{
		Args:  cobra.NoArgs,
		Use:   "sync",
		Short: "Copy secrets to cluster",
		Long: `Copy the cached secrets into the educates-secrets namespace of the cluster.

CA root key pairs are never copied. A CA key pair is copied only when the
local config's ingress.domain selects it, and is minted first from the
root CA when it is missing or stale. Without a local config, no CA key
pair is copied. When the local config's certificates cannot be decided,
such as a secure ingress.domain with neither a root CA nor a wildcard
certificate for it cached, the command warns why and copies every secret
that holds no CA key.

The local config is the config of the local cluster that --local-cluster
or EDUCATES_LOCAL_CLUSTER selects: <data-home>/config.yaml for the default
local cluster, <data-home>/clusters/<name>/config.yaml for a named one. A
named cluster with no ingress.domain is served over plain HTTP on the
nip.io name of its listen address, so no CA key pair is copied for it.

With a local cluster selected, the secrets are copied into its kube
context, kind-educates or kind-educates-<name>, unless --context names
another. With neither set, they are copied into the current context.`,
		Example: localSecretsSyncExample,
		RunE: func(cmd *cobra.Command, _ []string) error {
			id, err := selectedLocalCluster(cmd)
			if err != nil {
				return err
			}
			return o.Run(cmd.OutOrStdout(), id)
		},
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
	addLocalClusterContextFlag(c, &o.KubeconfigOptions)

	return c
}
