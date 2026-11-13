package cmd

import (
	"bytes"
	"fmt"
	"io"
	"os"

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
<host-IP>.nip.io fallback with ingress.insecure defaulted to true. This
makes the values the CLI supplies on your behalf explicit, rather than
leaving them implicit in a sparse file.

With --raw it instead prints the configuration file exactly as written,
comments included.

--local-cluster <name>, or EDUCATES_LOCAL_CLUSTER, prints the config of a
named local cluster instead, <data-home>/clusters/<name>/config.yaml.
With no ingress.domain set, its effective configuration takes the nip.io
name of its cluster.listenAddress, with ingress.insecure defaulted to
true.

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
	declared, cfg, err := loadLocalConfigForView(id)
	if err != nil {
		return err
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
	loaded, err := yaml.Marshal(declared)
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
		domainDefault := "# (when no ingress.domain is set) a <host-IP>.nip.io domain with\n" +
			"# ingress.insecure defaulted to true.\n"
		if !id.IsDefault() {
			domainDefault = "# (when no ingress.domain is set) the nip.io name of cluster.listenAddress,\n" +
				"# with ingress.insecure defaulted to true.\n"
		}
		header := "#\n" +
			"# Effective EducatesLocalConfig with all CLI defaults applied. Values\n" +
			"# absent from the configuration file are filled in by the CLI: static\n" +
			"# defaults, the image registry and version compiled into this CLI, and\n" +
			domainDefault +
			"#\n" +
			"# To see the configuration file as written, run:\n" +
			"#   educates local config view" + localClusterFlagArg(id) + " --raw\n" +
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

// loadLocalConfigForView loads the config of the local cluster id as view
// shows it. declared is what the file declares, with the static defaults.
// cfg is the config the cluster is deployed from before the CLI defaults:
// for a named local cluster whose file declares no ingress domain, it
// carries the nip.io name of the listen address, and for the default
// local cluster it is declared itself. It returns an error when the file
// would not load at deploy time, or is not an EducatesLocalConfig.
func loadLocalConfigForView(id localclusters.Identity) (declared, cfg *v1alpha1.EducatesLocalConfig, err error) {
	var named v1alpha1.Config
	if id.IsDefault() {
		if err := config.EnsureLocalConfigFile(utils.GetEducatesHomeDir()); err != nil {
			return nil, nil, err
		}
	} else {
		// The named-cluster rules come first: the static defaults would
		// turn a missing listen address into a loopback one.
		loaded, err := localclusters.LoadConfig(id)
		if err != nil {
			return nil, nil, err
		}
		named = loaded.Config
	}
	// LoadLocal validates against the schema and applies the static
	// WithDefaults; it errors if the file would not load at deploy time.
	declared, err = config.LoadLocal(id.ConfigPath)
	if err != nil {
		return nil, nil, fmt.Errorf("%s: %w", id.ConfigPath, err)
	}
	if named == nil {
		return declared, declared, nil
	}
	return declared, named.(*v1alpha1.EducatesLocalConfig), nil
}
