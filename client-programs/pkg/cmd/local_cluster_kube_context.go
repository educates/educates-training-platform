package cmd

import (
	"fmt"
	"io"
	"os"

	"al.essio.dev/pkg/shellescape"
	"github.com/spf13/cobra"

	"github.com/educates/educates-training-platform/client-programs/pkg/cluster"
	"github.com/educates/educates-training-platform/client-programs/pkg/deployer/progress"
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
	addLocalClusterFlag(c)

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

// localClusterKubeconfig returns the kubeconfig file that holds the kube
// context of a local cluster, given the value of --kubeconfig: the file
// local cluster create writes the context into, which is --kubeconfig
// when given and otherwise ~/.kube/config, whatever $KUBECONFIG says. It
// is the one place the commands decide which file to look a selected
// local cluster's context up in, and local registry deploy every local
// cluster's.
func localClusterKubeconfig(kubeconfigFlag string) string {
	return cluster.KindKubeconfigPath(kubeconfigFlag)
}

// noteKubeContext says, once local cluster create has made the local
// cluster id, that kind made its kube context the current context of the
// kubeconfig file kubeconfig, which it wrote the context into, and how to
// reach the cluster with kubectl. The command names the file when
// --kubeconfig, given as kubeconfigFlag, or $KUBECONFIG is set, since a
// kubectl without --kubeconfig may then read other files.
func noteKubeContext(rep progress.Reporter, w io.Writer, kubeconfigFlag, kubeconfig string, id localclusters.Identity) {
	namesFile := kubeconfigFlag != "" || os.Getenv("KUBECONFIG") != ""
	rep.Note(fmt.Sprintf("The current context of %s is now %s. Use the cluster with:", kubeconfig, id.Context))
	fmt.Fprintf(w, "\n    %s\n", kubeContextUsage(kubeconfig, id.Context, namesFile))
}

// kubeContextUsage returns the kubectl command that reaches a cluster
// through the kube context kubeContext, adding the kubeconfig file
// kubeconfig that holds it when namesFile is set.
func kubeContextUsage(kubeconfig, kubeContext string, namesFile bool) string {
	command := "kubectl cluster-info --context " + shellescape.Quote(kubeContext)
	if namesFile {
		command += " --kubeconfig " + shellescape.Quote(kubeconfig)
	}
	return command
}
