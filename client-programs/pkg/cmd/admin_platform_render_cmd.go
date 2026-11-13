package cmd

import (
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	"github.com/educates/educates-training-platform/client-programs/pkg/config"
	"github.com/educates/educates-training-platform/client-programs/pkg/config/hostinfo"
	"github.com/educates/educates-training-platform/client-programs/pkg/config/translator"
	"github.com/educates/educates-training-platform/client-programs/pkg/config/v1alpha1"
	"github.com/educates/educates-training-platform/client-programs/pkg/localcerts"
	"github.com/educates/educates-training-platform/client-programs/pkg/utils"
)

const adminPlatformRenderExample = `
  # Render the install for a checked-in config file (GitOps mode).
  #   - operator.image is defaulted from this CLI's compiled-in version.
  #   - ingress.domain MUST be set in the config; render errors if empty.
  educates admin platform render --config env.yaml > install.yaml

  # Render the install for laptop convenience (uses ~/.educates/config.yaml
  # or $EDUCATES_CLI_DATA_HOME/config.yaml). When ingress.domain is empty,
  # falls back to <host-IP>.nip.io with a comment header noting the
  # derivation.
  educates admin platform render --local-config
`

type PlatformRenderOptions struct {
	Config      string
	LocalConfig bool
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
  --local-config    Laptop convenience mode. Same defaults, plus a
                    <host-IP>.nip.io fallback for ingress.domain when
                    empty. Output is host-specific.`,
		Example: adminPlatformRenderExample,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return p.runRender(cmd.OutOrStdout(), &o)
		},
	}

	c.Flags().StringVarP(&o.Config, "config", "c", "", "path to a CLI config file (any kind)")
	c.Flags().BoolVar(&o.LocalConfig, "local-config", false,
		"use <data-home>/config.yaml; applies host-IP nip.io fallback for ingress.domain")
	c.MarkFlagsMutuallyExclusive("config", "local-config")
	c.MarkFlagsOneRequired("config", "local-config")

	return c
}

func (p *ProjectInfo) runRender(w io.Writer, o *PlatformRenderOptions) error {
	path, err := resolveConfigPath(o)
	if err != nil {
		return err
	}
	// EnsureLocalConfigFile composes the v3→v4 migration shim
	// (MaybeMigrateV3) with the user-actionable missing-file diagnostic
	// (MissingLocalConfigError). Returns nil when config.yaml is
	// either already present or has just been written by migration.
	if o.LocalConfig {
		if err := config.EnsureLocalConfigFile(utils.GetEducatesHomeDir()); err != nil {
			return err
		}
	}
	cfg, err := config.Load(path)
	if err != nil {
		return err
	}

	header := ""
	opts := translator.Options{}
	switch c := cfg.(type) {
	case *v1alpha1.EducatesLocalConfig:
		c.ApplyCLIDefaults(p.Version, p.ImageRepository)
		var hostNote string
		if o.LocalConfig {
			hostNote, err = maybeApplyHostDomain(c)
			if err != nil {
				return err
			}
		} else if c.Ingress.Domain == "" {
			return fmt.Errorf(`ingress.domain is required when using --config.
Set it explicitly in %s, or use --local-config to derive a <host-IP>.nip.io
fallback (output becomes host-specific and unsuitable for GitOps).`, path)
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
		header = hostNote + notes.String()
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

// resolveConfigPath picks the config file based on which flag was set.
// --local-config resolves to <data-home>/config.yaml, where <data-home>
// is $EDUCATES_CLI_DATA_HOME or $XDG_DATA_HOME/educates.
func resolveConfigPath(o *PlatformRenderOptions) (string, error) {
	if o.LocalConfig {
		return defaultLocalCluster().ConfigPath, nil
	}
	if o.Config == "" {
		return "", fmt.Errorf("internal: neither --config nor --local-config set (should have been caught by cobra)")
	}
	return o.Config, nil
}

// maybeApplyHostDomain applies <host-IP>.nip.io to ingress.domain when
// empty, and in that case also defaults ingress.insecure to true: a
// nip.io address cannot obtain a trusted TLS certificate, so the
// last-resort laptop fallback is a plain-HTTP cluster that needs no CA.
// A user-set domain is left untouched (and keeps the secure CustomCA
// path unless ingress.insecure is set explicitly). Returns the header
// comment block to emit at the top of the rendered output (empty when no
// defaulting was needed).
func maybeApplyHostDomain(c *v1alpha1.EducatesLocalConfig) (string, error) {
	if c.Ingress.Domain != "" {
		return "", nil
	}
	ip, err := hostinfo.DetectHostIP()
	if err != nil {
		return "", fmt.Errorf("auto-detect host IP for ingress.domain default: %w", err)
	}
	c.Ingress.Domain = hostinfo.NipDomain(ip)
	c.Ingress.Insecure = true
	return fmt.Sprintf(`# NOTE: ingress.domain was auto-derived from host IP %s as a last-resort
# nip.io fallback, and ingress.insecure was defaulted to true so the
# cluster is served over plain HTTP with no CA to manage (a nip.io
# address cannot obtain a trusted TLS certificate). For a secure install
# portable across networks, configure the resolver block
# (resolver.targetAddress + resolver.extraDomains), set ingress.domain to
# a fixed name like 'workshop.test', and add a CA with
# 'educates local secrets add ca'.
`, ip), nil
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
