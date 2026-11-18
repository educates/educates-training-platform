package cmd

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/spf13/cobra"

	"github.com/educates/educates-training-platform/client-programs/pkg/config/v1alpha1"
	"github.com/educates/educates-training-platform/client-programs/pkg/deployer"
	"github.com/educates/educates-training-platform/client-programs/pkg/localcerts"
	"github.com/educates/educates-training-platform/client-programs/pkg/localclusters"
)

const adminPlatformDeployExample = `
  # Install Educates from a checked-in config file:
  educates admin platform deploy --config env.yaml

  # Install Educates using the default local cluster's config
  # (<data-home>/config.yaml):
  educates admin platform deploy --local-config

  # Redeploy the named local cluster test2 from its own config, into its
  # own kube context:
  educates admin platform deploy --local-config --local-cluster test2

  # Show what a redeploy from the default local cluster's config would
  # change in the cluster, without changing anything:
  educates admin platform deploy --local-config --diff
`

// PlatformDeployOptions mirrors PlatformRenderOptions plus the kubectl
// connection flags consumed by the install path.
type PlatformDeployOptions struct {
	KubeconfigOptions
	Config      string
	LocalConfig bool
	Timeout     time.Duration
	Verbose     bool

	// Diff shows what the deploy would change instead of deploying.
	Diff bool

	// LocalCluster selects the local cluster whose config LocalConfig
	// reads.
	LocalCluster localclusters.Selection
}

func (p *ProjectInfo) NewAdminPlatformDeployCmd() *cobra.Command {
	var o PlatformDeployOptions

	c := &cobra.Command{
		Args:  cobra.NoArgs,
		Use:   "deploy",
		Short: "Install Educates: helm install operator + apply 4 platform CRs",
		Long: `Drive the install end-to-end. Same translator as 'admin platform render',
then push to the cluster:

  1. helm upgrade --install educates-installer (embedded chart)
  2. apply EducatesClusterConfig → wait Ready=True
  3. apply SecretsManager → wait Ready=True
  4. apply LookupService (if configured) → wait Ready=True
     (interleaved with SessionManager — both apply, then both wait)
  5. apply SessionManager → wait Ready=True
  6. point the loopback service that 'educates local cluster create'
     makes for 'educates serve-workshop' at the ingress domain, when
     the cluster has one

--local-cluster or EDUCATES_LOCAL_CLUSTER selects a local cluster. Unless
--context names another, the install goes into that cluster's kube
context, kind-educates or kind-educates-<name>, and with --local-config it
installs that cluster's config: <data-home>/config.yaml for the default
local cluster, <data-home>/clusters/<name>/config.yaml for a named one.
With neither set, the install goes into the current context.

--diff shows what the deploy would change in the cluster and changes
nothing, in the cluster or in the local secrets cache: the cached Secrets
it would sync, the CRDs, the operator release, and the platform resources,
each dry-run by the API server or Helm. The platform resources and the
values of the operator release are shown as unified diffs, and the CRDs
and the other objects of the operator release as a line each, unless
--verbose shows their diffs as well. What the operator then changes in
the components and cluster services it installs is not shown. The command
exits with status 0 whether or not anything would change.`,
		Example: adminPlatformDeployExample,
		RunE: func(cmd *cobra.Command, _ []string) error {
			o.LocalCluster = localClusterSelection(cmd)
			return p.runDeploy(cmd.Context(), cmd.OutOrStdout(), &o)
		},
	}

	c.Flags().StringVarP(&o.Config, "config", "c", "", "path to a CLI config file (any kind)")
	c.Flags().BoolVar(&o.LocalConfig, "local-config", false,
		"use the selected local cluster's config in the data home; applies the nip.io fallback for ingress.domain")
	c.Flags().StringVar(&o.Kubeconfig, "kubeconfig", "", "kubeconfig file (defaults to $KUBECONFIG / ~/.kube/config)")
	c.Flags().StringVar(&o.Context, "context", "", "context name to use within the kubeconfig")
	addLocalClusterContextFlag(c, &o.KubeconfigOptions)
	c.Flags().DurationVar(&o.Timeout, "timeout", deployer.DefaultTimeout, "per-CR Ready=True wait timeout")
	c.Flags().BoolVar(&o.Verbose, "verbose", false, "show helm SDK debug output on stderr; with --diff, show the full diff of each CRD and operator chart object")
	c.Flags().BoolVar(&o.Diff, "diff", false, "show what the deploy would change in the cluster, without changing anything")
	c.MarkFlagsMutuallyExclusive("config", "local-config")
	c.MarkFlagsOneRequired("config", "local-config")

	return c
}

func (p *ProjectInfo) runDeploy(ctx context.Context, w io.Writer, o *PlatformDeployOptions) error {
	// Reuse the same load → default → translate path as render so the
	// two commands stay in lock-step.
	cfg, path, err := loadPlatformConfig(o.Config, o.LocalConfig, o.LocalCluster)
	if err != nil {
		return err
	}

	var plan *localcerts.Plan
	switch c := cfg.(type) {
	case *v1alpha1.EducatesLocalConfig:
		c.ApplyCLIDefaults(p.Version, p.ImageRepository)
		// The local cluster rules of --local-config have already given
		// an empty ingress.domain the nip.io name of the listen address.
		if !o.LocalConfig && c.Ingress.Domain == "" {
			return fmt.Errorf("ingress.domain is required when using --config (set it in %s)", path)
		}
		// The certificates, and the cached Secrets to sync, come from the
		// local certificate resolution step.
		if plan, err = localcerts.Resolve(c); err != nil {
			return err
		}
	case *v1alpha1.EducatesConfig:
		// Pure passthrough.
	}

	flags := deployPipelineFlags{
		Kubeconfig: o.Kubeconfig,
		Context:    o.Context,
		Timeout:    o.Timeout,
		Verbose:    o.Verbose,
	}

	source := "--config " + absPath(path)
	if o.LocalConfig {
		source = "--local-config " + absPath(path)
	}
	flags.Record = deployRevisionInput(p.Version, cfg.GetKind(), source, path)

	if o.Diff {
		return translateAndDiff(ctx, w, cfg, plan, flags)
	}

	return translateAndDeploy(ctx, w, cfg, plan, flags)
}
