package cmd

import (
	"github.com/spf13/cobra"

	"github.com/educates/educates-training-platform/client-programs/pkg/cluster"
	"github.com/educates/educates-training-platform/client-programs/pkg/localclusters"
	"github.com/educates/educates-training-platform/client-programs/pkg/utils"
)

// addLocalClusterContextFlag adds the --local-cluster flag to the command
// c, which reaches a cluster through the kubeconfig flags bound to o.
// Before c runs, o is set to the kubeconfig file and context
// resolveKubeContext picks, so a local cluster selected with the flag or
// EDUCATES_LOCAL_CLUSTER is the cluster c acts on unless --context names
// another.
func addLocalClusterContextFlag(c *cobra.Command, o *KubeconfigOptions) {
	c.Flags().String(localclusters.FlagName, "",
		"name of the local cluster whose kube context to use when --context is not given, overriding $"+
			localclusters.SelectionEnvVar+"; an empty value selects the default local cluster")

	preRun := c.PreRunE
	c.PreRunE = func(cmd *cobra.Command, args []string) error {
		resolved, err := resolveKubeContext(*o, localClusterSelection(cmd), utils.GetEducatesHomeDir())
		if err != nil {
			return err
		}
		*o = resolved
		if preRun != nil {
			return preRun(cmd, args)
		}
		return nil
	}
}

// resolveKubeContext returns the kubeconfig file and context a command
// that takes the kubeconfig flags flags reaches its cluster through. The
// --context flag wins. Otherwise, when sel names a local cluster
// explicitly, it is that local cluster's context, in the data home
// dataHome, looked up in localClusterKubeconfig. Otherwise the flags are
// used as given, which means the current context.
func resolveKubeContext(flags KubeconfigOptions, sel localclusters.Selection, dataHome string) (KubeconfigOptions, error) {
	if flags.Context != "" || !sel.IsSet() {
		return flags, nil
	}
	id, err := localclusters.Resolve(sel, dataHome)
	if err != nil {
		return KubeconfigOptions{}, err
	}
	return KubeconfigOptions{Kubeconfig: localClusterKubeconfig(flags.Kubeconfig), Context: id.Context}, nil
}

func localClusterKubeconfig(kubeconfigFlag string) string {
	return cluster.KindKubeconfigPath(kubeconfigFlag)
}
