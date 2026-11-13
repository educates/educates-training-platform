package cmd

import (
	"os"

	"github.com/spf13/cobra"

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
// local cluster the command acts on, to the command c.
func addLocalClusterFlag(c *cobra.Command) {
	c.Flags().String(localclusters.FlagName, "",
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
	return " --" + localclusters.FlagName + " " + id.Name
}

// selectedLocalCluster returns the identity of the local cluster the
// command c acts on: the one its --local-cluster flag names when given,
// an empty value meaning the default local cluster, otherwise the one
// EDUCATES_LOCAL_CLUSTER names, and otherwise the default local cluster.
func selectedLocalCluster(c *cobra.Command) (localclusters.Identity, error) {
	sel := localclusters.Selection{Env: os.Getenv(localclusters.SelectionEnvVar)}
	if f := c.Flags().Lookup(localclusters.FlagName); f != nil && f.Changed {
		sel.FlagSet, sel.Flag = true, f.Value.String()
	}
	return localclusters.Resolve(sel, utils.GetEducatesHomeDir())
}
