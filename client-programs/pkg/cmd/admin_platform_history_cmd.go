package cmd

import (
	"context"
	"fmt"
	"io"
	"strconv"

	"github.com/spf13/cobra"
	"k8s.io/client-go/kubernetes"

	"github.com/educates/educates-training-platform/client-programs/pkg/deployer"
	"github.com/educates/educates-training-platform/client-programs/pkg/utils"
)

const adminPlatformHistoryExample = `
  # List the deploys recorded for the cluster of the current kube context:
  educates admin platform history

  # Show the configuration the original deploy applied:
  educates admin platform export --revision original
`

// PlatformHistoryOptions are the options of admin platform history.
type PlatformHistoryOptions struct {
	KubeconfigOptions
}

func (p *ProjectInfo) NewAdminPlatformHistoryCmd() *cobra.Command {
	var o PlatformHistoryOptions

	c := &cobra.Command{
		Args:  cobra.NoArgs,
		Use:   "history",
		Short: "List the deploys of Educates recorded for a cluster",
		Long: `List the deploys of Educates the CLI recorded for a cluster, one
revision each: when it deployed, whether it installed Educates or updated
it, the CLI and operator chart versions, the kind of its config and where
the config came from.

Every 'educates admin platform deploy', and the deploy of 'educates local
cluster create', records a revision in the ConfigMap ` + deployer.HistoryConfigMap + `
in namespace ` + deployer.OperatorNamespace + `, holding the config as given
and the EducatesConfig the deploy applied, which
'educates admin platform export --revision' shows. A deploy with --diff
records nothing. The history keeps revision 1, the original, and the
latest 10. On a cluster deployed before the CLI recorded deploys,
revision 1 is the first deploy recorded, and updated Educates.

--local-cluster or EDUCATES_LOCAL_CLUSTER selects a local cluster, whose
kube context the command reads unless --context names another. With
neither set, it reads the cluster of the current context.`,
		Example: adminPlatformHistoryExample,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runPlatformHistory(cmd.Context(), cmd.OutOrStdout(), &o)
		},
	}

	c.Flags().StringVar(&o.Kubeconfig, "kubeconfig", "", "kubeconfig file (defaults to $KUBECONFIG / ~/.kube/config)")
	c.Flags().StringVar(&o.Context, "context", "", "context name to use within the kubeconfig")
	addLocalClusterContextFlag(c, &o.KubeconfigOptions)

	return c
}

func runPlatformHistory(ctx context.Context, w io.Writer, o *PlatformHistoryOptions) error {
	kubeContext, revisions, err := clusterDeployHistory(ctx, o.KubeconfigOptions)
	if err != nil {
		return err
	}
	if len(revisions) == 0 {
		fmt.Fprintf(w, "The cluster of kube context %s has no deploy history: the CLI records a revision for each deploy it makes from this release on.\n", kubeContext)
		return nil
	}

	rows := make([][]string, 0, len(revisions))
	for _, rev := range revisions {
		rows = append(rows, []string{
			strconv.Itoa(rev.Number),
			rev.Deployed.UTC().Format("2006-01-02 15:04:05 MST"),
			rev.Action,
			rev.CLIVersion,
			rev.ChartVersion,
			rev.Kind,
			rev.Source,
		})
	}
	fmt.Fprintln(w, utils.PrintTable([]string{"REVISION", "DEPLOYED", "ACTION", "CLI", "CHART", "KIND", "SOURCE"}, rows))

	if first := revisions[0]; first.Action != deployer.ActionInstalled {
		fmt.Fprintf(w, "\nRevision %d is the first deploy recorded: Educates was installed before the CLI recorded deploys.\n", first.Number)
	}
	return nil
}

// clusterDeployHistory returns the kube context the kubeconfig options o
// reach the cluster through, after the cluster preflight, and the
// revisions of the cluster's deploy history, oldest first.
func clusterDeployHistory(ctx context.Context, o KubeconfigOptions) (string, []deployer.Revision, error) {
	flags := deployPipelineFlags{Kubeconfig: o.Kubeconfig, Context: o.Context}
	cf := deployConfigFlags(flags)

	kubeContext, err := reachCluster(cf, flags)
	if err != nil {
		return "", nil, err
	}
	cfg, err := cf.ToRESTConfig()
	if err != nil {
		return "", nil, err
	}
	cs, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return "", nil, err
	}
	revisions, err := deployer.History(ctx, cs)
	if err != nil {
		return "", nil, fmt.Errorf("kube context %s: %w", kubeContext, err)
	}
	return kubeContext, revisions, nil
}
