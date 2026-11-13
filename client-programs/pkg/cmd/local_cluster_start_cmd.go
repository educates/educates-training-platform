package cmd

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"

	"github.com/spf13/cobra"

	"github.com/educates/educates-training-platform/client-programs/pkg/cluster"
	"github.com/educates/educates-training-platform/client-programs/pkg/config/v1alpha1"
	"github.com/educates/educates-training-platform/client-programs/pkg/deployer/progress"
	"github.com/educates/educates-training-platform/client-programs/pkg/localclusters"
)

var localClusterStartExample = `
  # Start a stopped local cluster:
  educates local cluster start

  # Start the stopped named local cluster test2:
  educates local cluster start --local-cluster test2
`

func (p *ProjectInfo) NewLocalClusterStartCmd() *cobra.Command {
	var c = &cobra.Command{
		Args:  cobra.NoArgs,
		Use:   "start",
		Short: "Start the local Kubernetes cluster",
		Long: `Starts the node containers of a stopped local cluster.

Once its nodes run, every registry mirror container that exists is linked
into each of them, whichever cluster's config or command created it, so a
mirror deployed while the cluster was stopped reaches it too. A mirror the
cluster's config declares in cluster.registryMirrors is created first when
it is missing, and start refuses to run when its container exists with
another URL or username.`,
		Example: localClusterStartExample,
		RunE: func(cmd *cobra.Command, _ []string) error {
			id, err := selectedEducatesCluster(cmd)
			if err != nil {
				return err
			}
			// Refuse to start a cluster whose listen address another
			// Educates cluster, running or stopped, also binds, or that
			// is on no host interface.
			inventory, err := localClusterInventory(cmd.Context())
			if err != nil {
				return err
			}
			if err := inventory.CheckStart(id); err != nil {
				return err
			}
			// The cluster needs the registry mirrors its config declares,
			// whose containers every local cluster shares, so one whose
			// container pulls from another URL or as another username is
			// refused before the nodes start.
			declared := declaredRegistryMirrors(cmd.ErrOrStderr(), id)
			if err := inventory.CheckMirrors(id, declared); err != nil {
				return err
			}
			// Docker Desktop 4.97 and later publishes ports 80 and 443 on
			// a non-loopback address only with its privileged port
			// setting on.
			if err := checkDockerDesktopPrivilegedPorts(cmd.Context(), cmd.ErrOrStderr()); err != nil {
				return err
			}
			if err := cluster.NewKindClusterConfig("", id).StartCluster(); err != nil {
				return err
			}
			return linkMirrorsToStartedCluster(cmd.Context(), cmd.OutOrStdout(), id, inventory.MirrorsToLink(declared))
		},
	}

	addLocalClusterFlag(c)

	return c
}

// declaredRegistryMirrors returns the registry mirrors the config of the
// local cluster id in the data home declares the cluster needs. A missing
// config declares none. A config that does not load declares none either,
// with a warning on w, so start still links the mirror containers that
// exist.
func declaredRegistryMirrors(w io.Writer, id localclusters.Identity) []v1alpha1.RegistryMirror {
	loaded, err := localclusters.LoadConfig(id)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return nil
	case err != nil:
		fmt.Fprintf(w, "Warning: the registry mirrors the config of %s declares are not created, since the config does not load: %v\n",
			id.Description(), err)
		return nil
	}
	return loaded.RegistryMirrors()
}

// linkMirrorsToStartedCluster links each of mirrors into every node of
// the local cluster id, whose nodes run, creating first a mirror whose
// container is missing.
func linkMirrorsToStartedCluster(ctx context.Context, w io.Writer, id localclusters.Identity, mirrors []v1alpha1.RegistryMirror) error {
	if len(mirrors) == 0 {
		return nil
	}
	nodes, err := registryNodes(ctx, id)
	if err != nil {
		return err
	}
	return linkRegistryMirrors(progress.New(w, 0, isStdoutTTY(w)), mirrors, nodes)
}
