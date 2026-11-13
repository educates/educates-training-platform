package cmd

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v2"

	"github.com/educates/educates-training-platform/client-programs/pkg/config"
	"github.com/educates/educates-training-platform/client-programs/pkg/config/v1alpha1"
	"github.com/educates/educates-training-platform/client-programs/pkg/localclusters"
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
nip.io name of cluster.listenAddress with ingress.insecure defaulted to
true. This makes the values the CLI supplies on your behalf explicit,
rather than leaving them implicit in a sparse file. A config that breaks
a rule of a local cluster's config, such as one without a specific
cluster.listenAddress, is printed with a comment saying how to fix it.

With --raw it instead prints the configuration file exactly as written,
comments included.

--local-cluster <name>, or EDUCATES_LOCAL_CLUSTER, prints the config of a
named local cluster instead, <data-home>/clusters/<name>/config.yaml.

For programmatic field reads use 'educates local config get [PATH]'.`,
		Example: localConfigViewExample,
		RunE: func(cmd *cobra.Command, _ []string) error {
			id, err := selectedLocalCluster(cmd)
			if err != nil {
				return err
			}
			return p.runLocalConfigView(&o, id, cmd.OutOrStdout())
		},
	}
	c.Flags().BoolVar(&o.Raw, "raw", false, "print the configuration file as written instead of the effective configuration")
	addLocalClusterFlag(c)
	return c
}

// runLocalConfigView prints the config of the local cluster id.
func (p *ProjectInfo) runLocalConfigView(o *LocalConfigViewOptions, id localclusters.Identity, w io.Writer) error {
	cfgPath := id.ConfigPath
	declared, err := loadLocalConfigForView(id)
	if err != nil {
		return err
	}
	body, err := os.ReadFile(cfgPath)
	if err != nil {
		return err
	}

	// --raw: the file as the user wrote it (comments preserved), already
	// carrying its own yaml-language-server modeline.
	if o.Raw {
		_, err = w.Write(body)
		return err
	}

	// Default: the effective configuration with every CLI default
	// materialised, matching what `admin platform deploy --local-config`
	// would use. Marshal the loaded config first so we can tell whether
	// the CLI-supplied defaults (image refs, nip.io domain, insecure)
	// actually add anything beyond the file.
	loaded, err := yaml.Marshal(declared)
	if err != nil {
		return fmt.Errorf("marshal configuration: %w", err)
	}
	// A config that breaks a rule of a local cluster's config, such as
	// one without a specific listen address, is shown all the same, with
	// why, so that it can be fixed.
	ruleErr := localclusters.CheckConfig(id, body, cfgPath)
	effective, _, err := p.renderDefaultedLocalConfig(declared)
	if err != nil {
		return err
	}

	if _, err := io.WriteString(w, localConfigSchemaModeline+"\n"); err != nil {
		return err
	}
	// Only explain the defaults (and point at --raw) when they actually
	// changed something, or the config breaks a rule. When the file
	// already carries every default, the effective output equals `view
	// --raw`, so the commentary is just noise.
	if !bytes.Equal(loaded, effective) || ruleErr != nil {
		header := "#\n" +
			"# Effective EducatesLocalConfig with all CLI defaults applied. Values\n" +
			"# absent from the configuration file are filled in by the CLI: static\n" +
			"# defaults, the image registry and version compiled into this CLI, and\n" +
			"# (when no ingress.domain is set) the nip.io name of cluster.listenAddress,\n" +
			"# with ingress.insecure defaulted to true.\n" +
			"#\n" +
			"# To see the configuration file as written, run:\n" +
			"#   educates local config view" + localClusterFlagArg(id) + " --raw\n" +
			"# (file: " + cfgPath + ")\n"
		if ruleErr != nil {
			header += "#\n# The local cluster can't use this config until it is fixed:\n" +
				yamlCommentLines("#   ", ruleErr.Error())
		}
		header += "#\n"
		if _, err := io.WriteString(w, header); err != nil {
			return err
		}
	}
	_, err = w.Write(effective)
	return err
}

// yamlCommentLines returns each line of s as a YAML comment line starting
// with prefix, with no trailing space on a blank one.
func yamlCommentLines(prefix, s string) string {
	var b strings.Builder
	for _, line := range strings.Split(strings.TrimRight(s, "\n"), "\n") {
		b.WriteString(strings.TrimRight(prefix+line, " ") + "\n")
	}
	return b.String()
}

// loadLocalConfigForView loads the config of the local cluster id as view
// shows it: what the file declares, with the static defaults. It returns
// an error when the file is missing, would not load at deploy time, or is
// not an EducatesLocalConfig, but not for a rule of a local cluster's
// config it breaks, so that a config can be viewed in order to fix it.
func loadLocalConfigForView(id localclusters.Identity) (*v1alpha1.EducatesLocalConfig, error) {
	if id.IsDefault() {
		if err := config.EnsureLocalConfigFile(utils.GetEducatesHomeDir()); err != nil {
			return nil, err
		}
	} else if err := localclusters.CheckConfigExists(id); err != nil {
		return nil, err
	}
	// LoadLocal validates against the schema and applies the static
	// WithDefaults; it errors if the file would not load at deploy time.
	declared, err := config.LoadLocal(id.ConfigPath)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", id.ConfigPath, err)
	}
	return declared, nil
}
