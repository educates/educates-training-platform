package cmd

import (
	"context"
	"io"

	"github.com/spf13/cobra"

	"github.com/educates/educates-training-platform/client-programs/pkg/cluster"
	"github.com/educates/educates-training-platform/client-programs/pkg/config"
	"github.com/educates/educates-training-platform/client-programs/pkg/config/v1alpha1"
	"github.com/educates/educates-training-platform/client-programs/pkg/deployer/progress"
	"github.com/educates/educates-training-platform/client-programs/pkg/localclusters"
	"github.com/educates/educates-training-platform/client-programs/pkg/utils"
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

Every local cluster is served through the router, the container
educates-router, which publishes ports 80, 443 and 6443 on the router
address in <data-home>/router.yaml. Start writes router.yaml when it is
missing, with this host's IP address. Before the nodes start, it creates
the router when it is missing, and re-creates it when it runs another
image than the one this CLI pins, publishes on another address than
router.yaml's, does not publish port 6443, or has stopped, which drops every connection through it. Otherwise it renders the
router's configuration again and reloads it, which keeps open connections.
On Docker Desktop 4.97 or later, creating the router refuses to run while
"Allow privileged port forwarding to external machines" is off, in
Settings > Advanced.

Before the nodes start, start refuses a domain of the cluster's config,
its ingress domain or one of its resolver.extraDomains, that another local
cluster, running or stopped, claims, naming that cluster.

Once its nodes run, every registry mirror container that exists is linked
into each of them, whichever command created it, so a mirror deployed
while the cluster was stopped reaches it too. A mirror the registry
mirrors file, mirrors.yaml in the data home, declares is created first
when it is missing. Start refuses to run when that file does not load, or
when a declared mirror's container exists with another URL or username.`,
		Example: localClusterStartExample,
		RunE: func(cmd *cobra.Command, _ []string) error {
			id, err := selectedEducatesCluster(cmd)
			if err != nil {
				return err
			}
			return runLocalClusterStart(cmd.Context(), cmd.OutOrStdout(), cmd.ErrOrStderr(), id)
		},
	}

	addLocalClusterFlag(c)

	return c
}

// runLocalClusterStart starts the stopped local cluster id, writing what
// it does to w and warnings to errW.
func runLocalClusterStart(ctx context.Context, w, errW io.Writer, id localclusters.Identity) error {
	// The router file holds the router address, which the router is
	// created on: a missing one is written as local cluster create writes
	// it.
	if _, err := ensureRouterFile(w); err != nil {
		return err
	}
	inventory, err := localClusterInventory(ctx)
	if err != nil {
		return err
	}
	// Every local cluster needs the registry mirrors the registry mirrors
	// file declares, whose containers they all share, so a file that does
	// not load, or a mirror whose container pulls from another URL or as
	// another username, is refused before the nodes start.
	declared, err := config.LoadLocalMirrors(utils.GetEducatesHomeDir())
	if err != nil {
		return err
	}
	if err := inventory.CheckMirrors(declared); err != nil {
		return err
	}
	// A name leads to one local cluster only, through the router and the
	// local DNS resolver alike, so a domain of the cluster's config that
	// another local cluster, running or stopped, claims, as an edit made
	// while the cluster was stopped can give it, is refused before the
	// nodes start.
	if err := inventory.CheckStartDomains(id); err != nil {
		return err
	}
	// The router serves the cluster's names: it is created, or re-created
	// on a new image or address, before the nodes start, as the check that
	// ports 80, 443 and 6443 are free on the router address, and otherwise
	// rendered again and reloaded. The router keeps a stopped cluster's
	// route, so a node that comes back on another address is reached
	// there.
	rep := progress.New(w, 0, isStdoutTTY(w))
	routerOps := dockerRouterOperations(utils.GetEducatesHomeDir(), errW)
	changed, err := routerOps.ensureRouter(ctx, rep, inventory)
	if err != nil {
		return err
	}
	if !changed {
		routerOps.refreshRouter(ctx, rep, func(context.Context) (localclusters.Inventory, error) {
			return inventory, nil
		})
	}
	if err := cluster.NewKindClusterConfig("", id).StartCluster(); err != nil {
		return err
	}
	return linkMirrorsToStartedCluster(ctx, w, id, inventory.MirrorsToLink(declared))
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
