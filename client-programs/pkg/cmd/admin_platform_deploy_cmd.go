package cmd

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/spf13/cobra"

	"github.com/educates/educates-training-platform/client-programs/pkg/config"
	"github.com/educates/educates-training-platform/client-programs/pkg/config/v1alpha1"
	"github.com/educates/educates-training-platform/client-programs/pkg/deployer"
	"github.com/educates/educates-training-platform/client-programs/pkg/localcerts"
	"github.com/educates/educates-training-platform/client-programs/pkg/utils"
)

const adminPlatformDeployExample = `
  # Install Educates from a checked-in config file:
  educates admin platform deploy --config env.yaml

  # Install Educates using the laptop config (~/.educates/config.yaml):
  educates admin platform deploy --local-config
`

// PlatformDeployOptions mirrors PlatformRenderOptions plus the kubectl
// connection flags consumed by the install path.
type PlatformDeployOptions struct {
	Config      string
	LocalConfig bool
	Kubeconfig  string
	Context     string
	Timeout     time.Duration
	Verbose     bool
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
     the cluster has one`,
		Example: adminPlatformDeployExample,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return p.runDeploy(cmd.Context(), cmd.OutOrStdout(), &o)
		},
	}

	c.Flags().StringVarP(&o.Config, "config", "c", "", "path to a CLI config file (any kind)")
	c.Flags().BoolVar(&o.LocalConfig, "local-config", false,
		"use <data-home>/config.yaml; applies host-IP nip.io fallback for ingress.domain")
	c.Flags().StringVar(&o.Kubeconfig, "kubeconfig", "", "kubeconfig file (defaults to $KUBECONFIG / ~/.kube/config)")
	c.Flags().StringVar(&o.Context, "context", "", "context name to use within the kubeconfig")
	c.Flags().DurationVar(&o.Timeout, "timeout", deployer.DefaultTimeout, "per-CR Ready=True wait timeout")
	c.Flags().BoolVar(&o.Verbose, "verbose", false, "show helm SDK debug output on stderr")
	c.MarkFlagsMutuallyExclusive("config", "local-config")
	c.MarkFlagsOneRequired("config", "local-config")

	return c
}

func (p *ProjectInfo) runDeploy(ctx context.Context, w io.Writer, o *PlatformDeployOptions) error {
	// Reuse the same load → default → translate path as render so the
	// two commands stay in lock-step. (Step-9 cleanup factors this into
	// a shared helper.)
	path, err := resolveDeployConfigPath(o)
	if err != nil {
		return err
	}
	if o.LocalConfig {
		if err := config.EnsureLocalConfigFile(utils.GetEducatesHomeDir()); err != nil {
			return err
		}
	}
	cfg, err := config.Load(path)
	if err != nil {
		return err
	}

	var plan *localcerts.Plan
	switch c := cfg.(type) {
	case *v1alpha1.EducatesLocalConfig:
		c.ApplyCLIDefaults(p.Version, p.ImageRepository)
		if o.LocalConfig {
			// Fills <host-IP>.nip.io and defaults ingress.insecure when
			// the domain was left empty; no-op when the user set one.
			if _, err := maybeApplyHostDomain(c); err != nil {
				return err
			}
		} else if c.Ingress.Domain == "" {
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

	return translateAndDeploy(ctx, w, cfg, plan, deployPipelineFlags{
		Kubeconfig: o.Kubeconfig,
		Context:    o.Context,
		Timeout:    o.Timeout,
		Verbose:    o.Verbose,
	})
}

func resolveDeployConfigPath(o *PlatformDeployOptions) (string, error) {
	if o.LocalConfig {
		return defaultLocalCluster().ConfigPath, nil
	}
	if o.Config == "" {
		return "", fmt.Errorf("internal: neither --config nor --local-config set")
	}
	return o.Config, nil
}
