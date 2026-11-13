package cmd

import (
	"context"
	"os"

	"github.com/spf13/cobra"

	"github.com/educates/educates-training-platform/client-programs/pkg/cluster"
	"github.com/educates/educates-training-platform/client-programs/pkg/localclusters"
	"github.com/educates/educates-training-platform/client-programs/pkg/utils"
)

// defaultLocalCluster returns the identity of the default local cluster of
// the data home: the cluster the local commands that take no
// --local-cluster flag act on.
func defaultLocalCluster() localclusters.Identity {
	return localclusters.Default(utils.GetEducatesHomeDir())
}

// addLocalClusterFlag adds the --local-cluster flag, which selects the
// local cluster the command acts on, to the command c. Every command that
// takes the flag adds it here, so it reads the same in each one's help.
func addLocalClusterFlag(c *cobra.Command) {
	c.Flags().String(localclusters.SelectionFlagName, "",
		"name of the local cluster to act on, overriding $"+localclusters.SelectionEnvVar+
			"; an empty value selects the default local cluster")
}

// localClusterFlagArg returns the --local-cluster argument, with a
// leading space, that selects the local cluster id in a command line
// shown to the user, or nothing for the default local cluster.
func localClusterFlagArg(id localclusters.Identity) string {
	if id.IsDefault() {
		return ""
	}
	return " --" + localclusters.SelectionFlagName + " " + id.Name
}

// selectedLocalCluster returns the identity of the local cluster the
// command c acts on: the one its --local-cluster flag names when given,
// an empty value meaning the default local cluster, otherwise the one
// EDUCATES_LOCAL_CLUSTER names, and otherwise the default local cluster.
func selectedLocalCluster(c *cobra.Command) (localclusters.Identity, error) {
	return localclusters.Resolve(localClusterSelection(c), utils.GetEducatesHomeDir())
}

// selectedEducatesCluster returns the identity of the local cluster the
// command c acts on, as selectedLocalCluster selects it, for a command
// that acts on an existing local cluster. A named local cluster whose
// kind cluster is not an Educates cluster, since the data home holds no
// folder for it, is refused with an error saying why, so the command
// leaves alone a kind cluster educates-<name> made by hand.
func selectedEducatesCluster(c *cobra.Command) (localclusters.Identity, error) {
	id, err := selectedLocalCluster(c)
	if err != nil {
		return localclusters.Identity{}, err
	}
	if err := localclusters.CheckEducatesCluster(id); err != nil {
		return localclusters.Identity{}, err
	}
	return id, nil
}

// explicitlySelectedLocalCluster returns the identity of the local
// cluster the command c's --local-cluster flag or EDUCATES_LOCAL_CLUSTER
// selects, and true. With neither set it returns false and no identity:
// the command acts on no local cluster in particular.
func explicitlySelectedLocalCluster(c *cobra.Command) (localclusters.Identity, bool, error) {
	sel := localClusterSelection(c)
	if !sel.IsSet() {
		return localclusters.Identity{}, false, nil
	}
	id, err := localclusters.Resolve(sel, utils.GetEducatesHomeDir())
	return id, err == nil, err
}

// localClusterSelection returns what the command c was told about which
// local cluster to act on: its --local-cluster flag, when it takes one,
// and EDUCATES_LOCAL_CLUSTER.
func localClusterSelection(c *cobra.Command) localclusters.Selection {
	sel := localclusters.Selection{Env: os.Getenv(localclusters.SelectionEnvVar)}
	if f := c.Flags().Lookup(localclusters.SelectionFlagName); f != nil && f.Changed {
		sel.FlagSet, sel.Flag = true, f.Value.String()
	}
	return sel
}

// registryNodes returns the names of the node containers of the local
// cluster id that get the containerd host config of the local registry and
// the registry mirrors: every node of the cluster that runs a kubelet. It is
// empty when the cluster has no node containers.
func registryNodes(ctx context.Context, id localclusters.Identity) ([]string, error) {
	observed, err := cluster.ObserveNodeContainers(ctx)
	if err != nil {
		return nil, err
	}
	return id.RegistryNodes(observed), nil
}
