package cmd

import (
	"bytes"
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v2"

	"github.com/educates/educates-training-platform/client-programs/pkg/config"
	"github.com/educates/educates-training-platform/client-programs/pkg/utils"
)

var localConfigViewExample = `
  # Print the effective configuration with all CLI defaults applied:
  educates local config view

  # Print the configuration file exactly as written:
  educates local config view --raw
`

type LocalConfigViewOptions struct {
	Raw bool
}

func (p *ProjectInfo) NewLocalConfigViewCmd() *cobra.Command {
	var o LocalConfigViewOptions

	c := &cobra.Command{
		Args:  cobra.NoArgs,
		Use:   "view",
		Short: "Print the effective EducatesLocalConfig with defaults applied (or the raw file with --raw)",
		Long: `Reads <data-home>/config.yaml, validates it against the
EducatesLocalConfig schema, and by default prints the effective
configuration with all CLI defaults filled in: the static defaults, the
CLI's image registry and version, and (when no ingress.domain is set) the
<host-IP>.nip.io fallback with ingress.insecure defaulted to true. This
makes the values the CLI supplies on your behalf explicit, rather than
leaving them implicit in a sparse file.

With --raw it instead prints the configuration file exactly as written,
comments included.

For programmatic field reads use 'educates local config get [PATH]'.`,
		Example: localConfigViewExample,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return p.runLocalConfigView(&o, cmd.OutOrStdout())
		},
	}
	c.Flags().BoolVar(&o.Raw, "raw", false, "print the configuration file as written instead of the effective configuration")
	return c
}

func (p *ProjectInfo) runLocalConfigView(o *LocalConfigViewOptions, w io.Writer) error {
	cfgPath := defaultLocalCluster().ConfigPath
	if err := config.EnsureLocalConfigFile(utils.GetEducatesHomeDir()); err != nil {
		return err
	}
	// LoadLocal validates against the schema and applies the static
	// WithDefaults; it errors if the file would not load at deploy time.
	cfg, err := config.LoadLocal(cfgPath)
	if err != nil {
		return fmt.Errorf("%s: %w", cfgPath, err)
	}

	// --raw: the file as the user wrote it (comments preserved), already
	// carrying its own yaml-language-server modeline.
	if o.Raw {
		body, err := os.ReadFile(cfgPath)
		if err != nil {
			return err
		}
		_, err = w.Write(body)
		return err
	}

	// Default: the effective configuration with every CLI default
	// materialised, matching what `admin platform deploy --local-config`
	// would use. Marshal the loaded config first so we can tell whether
	// the CLI-supplied defaults (image refs, host-IP domain, insecure)
	// actually add anything beyond the file.
	loaded, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("marshal configuration: %w", err)
	}
	effective, hostErr, err := p.renderDefaultedLocalConfig(cfg)
	if err != nil {
		return err
	}

	if _, err := io.WriteString(w, localConfigSchemaModeline+"\n"); err != nil {
		return err
	}
	// Only explain the defaults (and point at --raw) when they actually
	// changed something. When the file already carries every default, the
	// effective output equals `view --raw`, so the commentary is just
	// noise.
	if !bytes.Equal(loaded, effective) {
		header := "#\n" +
			"# Effective EducatesLocalConfig with all CLI defaults applied. Values\n" +
			"# absent from the configuration file are filled in by the CLI: static\n" +
			"# defaults, the image registry and version compiled into this CLI, and\n" +
			"# (when no ingress.domain is set) a <host-IP>.nip.io domain with\n" +
			"# ingress.insecure defaulted to true.\n" +
			"#\n" +
			"# To see the configuration file as written, run:\n" +
			"#   educates local config view --raw\n" +
			"# (file: " + cfgPath + ")\n"
		if hostErr != nil {
			header += fmt.Sprintf("# (ingress.domain could not be defaulted: %v)\n", hostErr)
		}
		header += "#\n"
		if _, err := io.WriteString(w, header); err != nil {
			return err
		}
	}
	_, err = w.Write(effective)
	return err
}
