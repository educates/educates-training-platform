package cmd

import (
	"context"
	"fmt"
	"io"

	"github.com/spf13/cobra"

	"github.com/educates/educates-training-platform/client-programs/pkg/deployer"
)

const adminPlatformDriftExample = `
  # Show what changed in the cluster since the last deploy:
  educates admin platform drift

  # Show everything that changed since Educates was installed:
  educates admin platform drift --revision original
`

// PlatformDriftOptions are the options of admin platform drift.
type PlatformDriftOptions struct {
	KubeconfigOptions

	// Revision selects the revision of the deploy history to compare the
	// cluster with: latest, original or a number.
	Revision string
}

func (p *ProjectInfo) NewAdminPlatformDriftCmd() *cobra.Command {
	var o PlatformDriftOptions

	c := &cobra.Command{
		Args:  cobra.NoArgs,
		Use:   "drift",
		Short: "Show how a cluster drifted from a deploy of Educates",
		Long: `Show how the configuration of Educates in a cluster has drifted from what
a revision of its deploy history applied, and change nothing.

For the operator release it compares the chart version and values the
revision recorded with the release's. For each platform resource it
shows a unified diff of what a redeploy of the revision would leave
against the resource in the cluster, from a server-side dry run, so the
defaults the API server adds show as no drift. A field another tool such
as kubectl edit added survives a redeploy, so it shows in no diff: the
command lists the spec fields each field manager other than the CLI owns,
with when it wrote them. A resource the revision applied that the cluster
no longer has, or one it has that the revision did not apply, is said to
be so.

--revision takes latest, the default, for what changed since the last
deploy, original for what changed since the first deploy the history
keeps, redeploys included, or a revision number, as
'educates admin platform history' lists them.

--local-cluster or EDUCATES_LOCAL_CLUSTER selects a local cluster, whose
kube context the command reads unless --context names another. With
neither set, it reads the cluster of the current context.`,
		Example: adminPlatformDriftExample,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runPlatformDrift(cmd.Context(), cmd.OutOrStdout(), &o)
		},
	}

	c.Flags().StringVar(&o.Revision, "revision", "latest", "the revision of the deploy history to compare with: latest, original or a number")
	c.Flags().StringVar(&o.Kubeconfig, "kubeconfig", "", "kubeconfig file (defaults to $KUBECONFIG / ~/.kube/config)")
	c.Flags().StringVar(&o.Context, "context", "", "context name to use within the kubeconfig")
	addLocalClusterContextFlag(c, &o.KubeconfigOptions)

	return c
}

func runPlatformDrift(ctx context.Context, w io.Writer, o *PlatformDriftOptions) error {
	kubeContext, revisions, err := clusterDeployHistory(ctx, o.KubeconfigOptions)
	if err != nil {
		return err
	}
	rev, err := deployer.SelectRevision(revisions, o.Revision)
	if err != nil {
		return fmt.Errorf("kube context %s: %w", kubeContext, err)
	}

	flags := deployPipelineFlags{Kubeconfig: o.Kubeconfig, Context: o.Context}
	return deployer.Drift(ctx, rev, deployer.DriftOptions{
		Getter:  deployConfigFlags(flags),
		Out:     w,
		Context: kubeContext,
	})
}
