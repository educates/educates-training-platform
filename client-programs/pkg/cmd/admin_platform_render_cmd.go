package cmd

import (
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	"github.com/educates/educates-training-platform/client-programs/pkg/config"
	"github.com/educates/educates-training-platform/client-programs/pkg/config/translator"
	"github.com/educates/educates-training-platform/client-programs/pkg/config/v1alpha1"
	"github.com/educates/educates-training-platform/client-programs/pkg/localcerts"
	"github.com/educates/educates-training-platform/client-programs/pkg/localclusters"
	"github.com/educates/educates-training-platform/client-programs/pkg/utils"
)

const adminPlatformRenderExample = `
  # Render the install for a checked-in config file (GitOps mode).
  #   - operator.image is defaulted from this CLI's compiled-in version.
  #   - ingress.domain MUST be set in the config; render errors if empty.
  educates admin platform render --config env.yaml > install.yaml

  # Render the install for laptop convenience (uses the default local
  # cluster's <data-home>/config.yaml). When ingress.domain is empty, it
  # falls back to the nip.io name of cluster.listenAddress.
  educates admin platform render --local-config

  # Render the install of the named local cluster test2 from its own config:
  educates admin platform render --local-config --local-cluster test2
`

type PlatformRenderOptions struct {
	Config      string
	LocalConfig bool

	// LocalCluster selects the local cluster whose config LocalConfig
	// reads.
	LocalCluster localclusters.Selection
}

func (p *ProjectInfo) NewAdminPlatformRenderCmd() *cobra.Command {
	var o PlatformRenderOptions

	c := &cobra.Command{
		Args:  cobra.NoArgs,
		Use:   "render",
		Short: "Render the install plan (operator chart values + 4 platform CRs) to stdout",
		Long: `Render the install plan that "educates admin platform deploy" would apply,
without touching the cluster. Useful for inspection, GitOps, and diffing.

Output is a single YAML stream with two sections, in deploy order:
  - operator chart values (consumed by 'helm install -f - educates-installer')
  - the four platform CRs (consumed by 'kubectl apply -f -')

Defaulting is flag-driven:
  --config <file>   Explicit / GitOps mode. operator.image defaults from
                    this CLI binary; ingress.domain MUST be set in the
                    config. Render is deterministic for a given (input,
                    CLI version) pair.
  --local-config    Laptop convenience mode. Same defaults, plus the
                    rules of a local cluster's config: a specific
                    cluster.listenAddress, and its nip.io name as the
                    ingress.domain fallback when empty.

--local-config reads the config of the local cluster that --local-cluster
or EDUCATES_LOCAL_CLUSTER selects: <data-home>/config.yaml for the default
local cluster, <data-home>/clusters/<name>/config.yaml for a named one.`,
		Example: adminPlatformRenderExample,
		RunE: func(cmd *cobra.Command, _ []string) error {
			o.LocalCluster = localClusterSelection(cmd)
			return p.runRender(cmd.OutOrStdout(), &o)
		},
	}

	c.Flags().StringVarP(&o.Config, "config", "c", "", "path to a CLI config file (any kind)")
	c.Flags().BoolVar(&o.LocalConfig, "local-config", false,
		"use the selected local cluster's config in the data home; applies the nip.io fallback for ingress.domain")
	addLocalClusterFlag(c)
	c.MarkFlagsMutuallyExclusive("config", "local-config")
	c.MarkFlagsMutuallyExclusive("config", localclusters.SelectionFlagName)
	c.MarkFlagsOneRequired("config", "local-config")

	return c
}

func (p *ProjectInfo) runRender(w io.Writer, o *PlatformRenderOptions) error {
	cfg, path, err := loadPlatformConfig(o.Config, o.LocalConfig, o.LocalCluster)
	if err != nil {
		return err
	}

	header := ""
	opts := translator.Options{}
	switch c := cfg.(type) {
	case *v1alpha1.EducatesLocalConfig:
		c.ApplyCLIDefaults(p.Version, p.ImageRepository)
		// The local cluster rules of --local-config have already given
		// an empty ingress.domain the nip.io name of the listen address.
		if !o.LocalConfig && c.Ingress.Domain == "" {
			return fmt.Errorf(`ingress.domain is required when using --config.
Set it explicitly in %s, or use --local-config to fall back to the nip.io
name of cluster.listenAddress.`, path)
		}
		// The certificates come from the local certificate resolution
		// step. Failing here (before any rendering) gives the user a
		// remediation path before they pipe output to GitOps. Render
		// only reports the plan: it never commits it, so the local
		// secrets cache is left untouched.
		plan, err := localcerts.Resolve(c)
		if err != nil {
			return err
		}
		opts = plan.TranslatorOptions()
		var notes yamlComments
		plan.Report(&notes)
		header = notes.String()
	case *v1alpha1.EducatesConfig:
		// Escape-hatch: pure passthrough. No CLI-side defaults. User owns
		// the full surface; missing required fields are caught by the
		// CRD-derived schema at load time or by the apiserver at apply.
	}

	out, err := translator.Translate(cfg, opts)
	if err != nil {
		return err
	}

	return writeRender(w, out, header)
}

// yamlComments is a localcerts.Reporter that collects each reported line
// as a YAML comment, for render to put above its output.
type yamlComments struct{ strings.Builder }

func (c *yamlComments) Note(msg string) {
	for _, line := range strings.Split(msg, "\n") {
		c.WriteString(strings.TrimRight("# "+line, " ") + "\n")
	}
}

// loadPlatformConfig loads the config admin platform render and deploy
// install from, and returns it with the path it was read from: the file
// configFile when localConfig is not set, otherwise the config of the
// local cluster sel selects, in the data home, which is
// $EDUCATES_CLI_DATA_HOME or $XDG_DATA_HOME/educates.
//
// A missing default local cluster's config.yaml is migrated from a v3
// install when there is one, and is otherwise an error that says how to
// write it. A named local cluster's config follows the rules of
// localclusters.LoadNamedConfig.
func loadPlatformConfig(configFile string, localConfig bool, sel localclusters.Selection) (v1alpha1.Config, string, error) {
	if !localConfig {
		if configFile == "" {
			return nil, "", fmt.Errorf("internal: neither --config nor --local-config set (should have been caught by cobra)")
		}
		cfg, err := config.Load(configFile)
		return cfg, configFile, err
	}

	id, err := localclusters.Resolve(sel, utils.GetEducatesHomeDir())
	if err != nil {
		return nil, "", err
	}
	if id.IsDefault() {
		if err := config.EnsureLocalConfigFile(utils.GetEducatesHomeDir()); err != nil {
			return nil, id.ConfigPath, err
		}
	}
	loaded, err := localclusters.LoadConfig(id)
	return loaded.Config, id.ConfigPath, err
}

// writeRender emits the rendered install plan in deploy order:
//   - optional host-defaulting header
//   - "# === operator chart values ===" + values YAML
//   - "# === platform CRs ===" + multi-doc CR YAML
func writeRender(w io.Writer, out *translator.Output, hostHeader string) error {
	values, err := translator.RenderOperatorValues(out)
	if err != nil {
		return fmt.Errorf("render operator values: %w", err)
	}
	crs, err := translator.RenderCRs(out)
	if err != nil {
		return fmt.Errorf("render CRs: %w", err)
	}

	if hostHeader != "" {
		if _, err := fmt.Fprint(w, hostHeader); err != nil {
			return err
		}
	}
	if _, err := fmt.Fprintln(w, "# === operator chart values (helm install -f - educates-installer ...) ==="); err != nil {
		return err
	}
	if _, err := w.Write(values); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(w, "# === platform CRs (kubectl apply -f - ...) ==="); err != nil {
		return err
	}
	_, err = w.Write(crs)
	return err
}
