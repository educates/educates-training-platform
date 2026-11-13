package cmd

import (
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

  # Write the config of the named local cluster test2, on the host IP
  # address 10.10.10.2:
  educates local config init --local-cluster test2 --listen-address 10.10.10.2
`

type LocalConfigInitOptions struct {
	Force    bool
	Defaults bool

	// ListenAddress and Domain write a named local cluster's config.
	ListenAddress string
	Domain        string
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

--local-cluster <name>, or EDUCATES_LOCAL_CLUSTER, writes the config of
a named local cluster instead, to <data-home>/clusters/<name>/config.yaml.
It declares cluster.listenAddress from --listen-address, which a named
cluster needs, and ingress.domain from the optional --domain. The listen
address must be a specific IP address of the host, neither 0.0.0.0 nor a
loopback address, and with no domain the cluster takes the nip.io name of
its listen address. --defaults writes only the default local cluster's
config.

<data-home> is $EDUCATES_CLI_DATA_HOME if set, otherwise
$XDG_DATA_HOME/educates.`,
		Example: localConfigInitExample,
		RunE: func(cmd *cobra.Command, _ []string) error {
			id, err := selectedLocalCluster(cmd)
			if err != nil {
				return err
			}
			return p.runLocalConfigInit(&o, id, cmd.OutOrStdout())
		},
	}
	c.Flags().BoolVar(&o.Force, "force", false, "overwrite existing config.yaml")
	c.Flags().BoolVar(&o.Defaults, "defaults", false, "write the fully-defaulted configuration instead of a minimal file")
	addLocalClusterFlag(c)
	c.Flags().StringVar(&o.ListenAddress, localclusters.ListenAddressFlagName, "", "host IP a named local cluster publishes ports 80 and 443 on (required for a named local cluster)")
	c.Flags().StringVar(&o.Domain, localclusters.DomainFlagName, "", "ingress domain of a named local cluster (default: the nip.io name of the listen address)")
	return c
}

// runLocalConfigInit writes the config of the local cluster id.
func (p *ProjectInfo) runLocalConfigInit(o *LocalConfigInitOptions, id localclusters.Identity, w io.Writer) error {
	if !id.IsDefault() {
		return runNamedLocalConfigInit(o, id, w)
	}
	if o.ListenAddress != "" || o.Domain != "" {
		return localclusters.ConfigFlagsOnDefaultClusterError()
	}

	dataHome := utils.GetEducatesHomeDir()
	if err := os.MkdirAll(dataHome, 0o755); err != nil {
		return fmt.Errorf("create data home %q: %w", dataHome, err)
	}
	path := id.ConfigPath
	if err := refuseExistingConfig(path, o.Force); err != nil {
		return err
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

// runNamedLocalConfigInit writes the config of the named local cluster
// id from --listen-address and --domain. It writes only what they
// declare, so it takes no --defaults: 'local config view' shows the
// defaults the cluster gets.
func runNamedLocalConfigInit(o *LocalConfigInitOptions, id localclusters.Identity, w io.Writer) error {
	if o.Defaults {
		return fmt.Errorf("--defaults writes the default local cluster's config only; a named local cluster's config "+
			"declares what --%s and --%s give, and 'educates local config view%s' shows the defaults it gets",
			localclusters.ListenAddressFlagName, localclusters.DomainFlagName, localClusterFlagArg(id))
	}
	if err := refuseExistingConfig(id.ConfigPath, o.Force); err != nil {
		return err
	}
	flags := localclusters.NamedConfigFlags{ListenAddress: o.ListenAddress, Domain: o.Domain}
	if err := localclusters.WriteNamedConfig(id, flags); err != nil {
		return err
	}
	fmt.Fprintf(w, "wrote %s\n", id.ConfigPath)
	return nil
}

// refuseExistingConfig returns an error when a config already exists at
// path, unless force allows init to overwrite it.
func refuseExistingConfig(path string, force bool) error {
	if _, err := os.Stat(path); err == nil && !force {
		return fmt.Errorf("%s already exists (use --force to overwrite)", path)
	}
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
