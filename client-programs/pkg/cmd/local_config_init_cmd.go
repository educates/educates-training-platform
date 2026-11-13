package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v2"

	"github.com/educates/educates-training-platform/client-programs/pkg/config"
	"github.com/educates/educates-training-platform/client-programs/pkg/config/v1alpha1"
	"github.com/educates/educates-training-platform/client-programs/pkg/localclusters"
	"github.com/educates/educates-training-platform/client-programs/pkg/utils"
)

// The minimal-config building blocks live in the config package so the
// `local config init` template and the `local cluster create` auto-init
// stay byte-for-byte identical.
const localConfigSchemaModeline = config.DefaultLocalConfigModeline

const defaultLocalConfigYAML = config.DefaultLocalConfigYAML

var localConfigInitExample = `
  # Write a minimal EducatesLocalConfig:
  educates local config init

  # Overwrite an existing config file:
  educates local config init --force

  # Write the fully-defaulted configuration:
  educates local config init --defaults
`

type LocalConfigInitOptions struct {
	Force    bool
	Defaults bool
}

func (p *ProjectInfo) NewLocalConfigInitCmd() *cobra.Command {
	var o LocalConfigInitOptions

	c := &cobra.Command{
		Args:  cobra.NoArgs,
		Use:   "init",
		Short: "Write an EducatesLocalConfig to <data-home>/config.yaml",
		Long: `Creates <data-home>/config.yaml. By default it writes the minimum
EducatesLocalConfig (apiVersion + kind only), leaving every other field
to take its default at deploy time. With --defaults it instead writes the
fully-defaulted configuration, with the static defaults, the CLI image
registry and version, and the <host-IP>.nip.io domain (and the insecure
default it implies) materialised into the file. Errors if the file
already exists unless --force.

<data-home> is $EDUCATES_CLI_DATA_HOME if set, otherwise
$XDG_DATA_HOME/educates.`,
		Example: localConfigInitExample,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return p.runLocalConfigInit(&o, cmd.OutOrStdout())
		},
	}
	c.Flags().BoolVar(&o.Force, "force", false, "overwrite existing config.yaml")
	c.Flags().BoolVar(&o.Defaults, "defaults", false, "write the fully-defaulted configuration instead of a minimal file")
	return c
}

func (p *ProjectInfo) runLocalConfigInit(o *LocalConfigInitOptions, w interface{ Write([]byte) (int, error) }) error {
	dataHome := utils.GetEducatesHomeDir()
	if err := os.MkdirAll(dataHome, 0o755); err != nil {
		return fmt.Errorf("create data home %q: %w", dataHome, err)
	}
	path := localclusters.Default(dataHome).ConfigPath
	if _, err := os.Stat(path); err == nil && !o.Force {
		return fmt.Errorf("%s already exists (use --force to overwrite)", path)
	}

	body := []byte(defaultLocalConfigYAML)
	if o.Defaults {
		cfg := &v1alpha1.EducatesLocalConfig{
			TypeMeta: v1alpha1.TypeMeta{
				APIVersion: v1alpha1.APIVersion,
				Kind:       v1alpha1.KindEducatesLocalConfig,
			},
		}
		effective, hostErr, err := p.renderDefaultedLocalConfig(cfg)
		if err != nil {
			return err
		}
		if hostErr != nil {
			fmt.Fprintf(w, "note: ingress.domain left unset (%v)\n", hostErr)
		}
		body = append([]byte(localConfigSchemaModeline+"\n"), effective...)
	}

	if err := os.WriteFile(path, body, 0o644); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	fmt.Fprintf(w, "wrote %s\n", path)
	return nil
}

// renderDefaultedLocalConfig applies every CLI default to cfg (the static
// WithDefaults, the CLI image registry/version, and the host-IP nip.io +
// insecure fallback) and marshals it to YAML. Host-IP detection is
// best-effort: hostErr is non-nil when it fails, in which case the
// ingress domain is left unset.
func (p *ProjectInfo) renderDefaultedLocalConfig(cfg *v1alpha1.EducatesLocalConfig) (body []byte, hostErr error, err error) {
	cfg.WithDefaults()
	cfg.ApplyCLIDefaults(p.Version, p.ImageRepository)
	_, hostErr = maybeApplyHostDomain(cfg)
	body, err = yaml.Marshal(cfg)
	if err != nil {
		return nil, hostErr, fmt.Errorf("marshal effective configuration: %w", err)
	}
	return body, hostErr, nil
}
