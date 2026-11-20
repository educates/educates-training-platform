package cmd

import (
	"context"
	"fmt"
	"io"

	"github.com/spf13/cobra"

	"github.com/educates/educates-training-platform/client-programs/pkg/deployer"
)

const adminPlatformExportExample = `
  # Export the configuration deployed to the cluster of the current kube
  # context:
  educates admin platform export > educates.yaml

  # Export the configuration deployed to the default local cluster:
  educates admin platform export --local-cluster ""

  # Check that the export deploys back unchanged:
  educates admin platform deploy --config educates.yaml --diff

  # Show what the deploy that installed Educates applied, and the config
  # it was given:
  educates admin platform export --revision original
  educates admin platform export --revision original --source
`

// PlatformExportOptions are the options of admin platform export.
type PlatformExportOptions struct {
	KubeconfigOptions

	// Revision selects a revision of the deploy history to show in place
	// of the live state: latest, original or a number.
	Revision string

	// Source shows the revision's config as given in place of what it
	// applied.
	Source bool
}

func (p *ProjectInfo) NewAdminPlatformExportCmd() *cobra.Command {
	var o PlatformExportOptions

	c := &cobra.Command{
		Args:  cobra.NoArgs,
		Use:   "export",
		Short: "Write the configuration deployed to a cluster as an EducatesConfig",
		Long: `Write the configuration of Educates deployed to a cluster to standard
output, as an EducatesConfig: the spec of each platform resource as the
cluster holds it, including the defaults the API server added and what
another tool, such as kubectl edit, changed, and the values of the
operator release as the operator block.

The file deploys back with 'educates admin platform deploy --config',
to the same cluster, where '--diff' shows nothing would change, or to
another one. It holds no Secret: a comment at the top names the Secrets
the platform resources reference, which a cluster needs before the file
is deployed there. A cluster's kind cluster and DNS resolver settings,
the target block, are not stored in the cluster, so the file has none.
Operator chart values the operator block cannot express, such as ones a
helm install of the chart set, are named in a comment and a warning.

--revision shows, in place of the live state, the EducatesConfig a
deploy recorded in the deploy history applied, and with --source the
config that deploy was given, as given: latest for the last deploy,
original for the first one the history keeps, or a revision number, as
'educates admin platform history' lists them.

--local-cluster or EDUCATES_LOCAL_CLUSTER selects a local cluster, whose
kube context the command reads unless --context names another. With
neither set, it reads the cluster of the current context.`,
		Example: adminPlatformExportExample,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if o.Source && o.Revision == "" {
				return fmt.Errorf("--source shows the config a revision of the deploy history was given, so it needs --revision")
			}
			if o.Revision != "" {
				return runRevisionExport(cmd.Context(), cmd.OutOrStdout(), &o)
			}
			return runPlatformExport(cmd.Context(), cmd.OutOrStdout(), cmd.ErrOrStderr(), &o)
		},
	}

	c.Flags().StringVar(&o.Revision, "revision", "", "show what a revision of the deploy history applied: latest, original or a number")
	c.Flags().BoolVar(&o.Source, "source", false, "with --revision, show the config the revision was given, as given")
	c.Flags().StringVar(&o.Kubeconfig, "kubeconfig", "", "kubeconfig file (defaults to $KUBECONFIG / ~/.kube/config)")
	c.Flags().StringVar(&o.Context, "context", "", "context name to use within the kubeconfig")
	addLocalClusterContextFlag(c, &o.KubeconfigOptions)

	return c
}

func runPlatformExport(ctx context.Context, w, errw io.Writer, o *PlatformExportOptions) error {
	flags := deployPipelineFlags{Kubeconfig: o.Kubeconfig, Context: o.Context}
	cf := deployConfigFlags(flags)

	kubeContext, err := reachCluster(cf, flags)
	if err != nil {
		return err
	}

	exported, err := deployer.ExportLive(ctx, cf)
	if err != nil {
		return fmt.Errorf("kube context %s: %w", kubeContext, err)
	}

	header := []string{fmt.Sprintf("The configuration of Educates deployed to the cluster of kube context %s,", kubeContext)}
	if exported.ChartVersion != "" {
		header = append(header, fmt.Sprintf("running operator chart %s, as 'educates admin platform export' read it.", exported.ChartVersion))
	} else {
		header = append(header, "as 'educates admin platform export' read it.")
	}
	header = append(header, "")

	data, err := exported.YAML(header)
	if err != nil {
		return err
	}

	if len(exported.UnmappedValues) > 0 {
		fmt.Fprintf(errw, "warning: the operator release sets values the operator block cannot express, which a deploy of the export leaves unset: %v\n", exported.UnmappedValues)
	}
	if exported.NoOperatorRelease {
		fmt.Fprintf(errw, "warning: the cluster has no operator release %s in namespace %s, so the export has no operator block\n", deployer.OperatorReleaseName, deployer.OperatorNamespace)
	}

	_, err = w.Write(data)
	return err
}

// runRevisionExport writes what the revision of the deploy history
// o.Revision selects applied, or with o.Source the config it was given,
// after a comment saying which deploy it was.
func runRevisionExport(ctx context.Context, w io.Writer, o *PlatformExportOptions) error {
	kubeContext, revisions, err := clusterDeployHistory(ctx, o.KubeconfigOptions)
	if err != nil {
		return err
	}
	rev, err := deployer.SelectRevision(revisions, o.Revision)
	if err != nil {
		return fmt.Errorf("kube context %s: %w", kubeContext, err)
	}

	what, body := "The EducatesConfig it applied.", rev.Applied
	if o.Source {
		what, body = "The config it was given, as given.", rev.Config
	}

	deployedBy := "CLI " + rev.CLIVersion
	if rev.ChartVersion != "" {
		deployedBy += ", operator chart " + rev.ChartVersion
	}
	header := []string{
		fmt.Sprintf("Revision %d of the deploy history of the cluster of kube context %s.", rev.Number, kubeContext),
		what,
		fmt.Sprintf("It %s Educates on %s.", rev.Action, rev.Deployed.UTC().Format("2006-01-02 15:04:05 MST")),
		fmt.Sprintf("Deployed by %s, through kube context %s,", deployedBy, rev.KubeContext),
		fmt.Sprintf("from %s (%s).", rev.Source, rev.Kind),
		"",
	}
	for _, line := range header {
		fmt.Fprint(w, commentLine(line))
	}
	_, err = io.WriteString(w, body)
	return err
}

func commentLine(line string) string {
	if line == "" {
		return "#\n"
	}
	return "# " + line + "\n"
}
