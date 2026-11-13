package cmd

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/educates/educates-training-platform/client-programs/pkg/config"
	"github.com/educates/educates-training-platform/client-programs/pkg/config/v1alpha1"
	"github.com/educates/educates-training-platform/client-programs/pkg/localclusters"
)

// localConfigSchemaModeline is the yaml-language-server modeline every
// EducatesLocalConfig the command line tool writes starts with.
const localConfigSchemaModeline = config.DefaultLocalConfigModeline

var localConfigInitExample = `
  # Write a minimal EducatesLocalConfig, on this host's IP address:
  educates local config init

  # Write it on an alias of a network interface, which survives network
  # changes:
  educates local config init --listen-address 10.10.10.1

  # Overwrite an existing config file:
  educates local config init --force

  # Write the fully-defaulted configuration:
  educates local config init --defaults

  # Write the config of the named local cluster test2, on the IP address
  # 10.10.10.2 of the host:
  educates local config init --local-cluster test2 --listen-address 10.10.10.2
`

type LocalConfigInitOptions struct {
	Force    bool
	Defaults bool

	// ListenAddress and Domain are the values of --listen-address and
	// --domain.
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
EducatesLocalConfig: apiVersion, kind and cluster.listenAddress, leaving
every other field to take its default at deploy time. With --defaults it
instead writes the fully-defaulted configuration, with the static
defaults, the CLI image registry and version, and the nip.io name of
cluster.listenAddress as the domain (and the insecure default it
implies) materialised into the file. Errors if the file already exists
unless --force.

Every local cluster publishes its ports 80 and 443 on its
cluster.listenAddress, which must be a specific IP address of the host,
neither 0.0.0.0 nor a loopback address. --listen-address sets it, and
without it the default local cluster takes the host's IP address, with a
comment saying so: an alias such as 10.10.10.1, added to a network
interface, survives network changes, where the host's IP address may not.
With no IP address of the host detected, the config is written without
one, and says what to set. --domain sets ingress.domain, and without it
the cluster takes the nip.io name of its listen address.

--local-cluster <name>, or EDUCATES_LOCAL_CLUSTER, writes the config of
a named local cluster instead, to <data-home>/clusters/<name>/config.yaml,
which needs --listen-address. --defaults writes only the default local
cluster's config.

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
	c.Flags().StringVar(&o.ListenAddress, localclusters.ListenAddressFlagName, "", "host IP the local cluster publishes ports 80 and 443 on (default: this host's IP address for the default local cluster; required for a named one)")
	c.Flags().StringVar(&o.Domain, localclusters.DomainFlagName, "", "ingress domain of the local cluster (default: the nip.io name of the listen address)")
	return c
}

// runLocalConfigInit writes the config of the local cluster id from
// --listen-address and --domain, as localclusters.WriteConfig does. The
// default local cluster takes the host's IP address without
// --listen-address, and with --defaults gets every CLI default written
// too. A named local cluster's config declares only what the flags give,
// so it takes no --defaults: 'local config view' shows the defaults it
// gets.
func (p *ProjectInfo) runLocalConfigInit(o *LocalConfigInitOptions, id localclusters.Identity, w io.Writer) error {
	if o.Defaults && !id.IsDefault() {
		return fmt.Errorf("--defaults writes the default local cluster's config only; a named local cluster's config "+
			"declares what --%s and --%s give, and 'educates local config view%s' shows the defaults it gets",
			localclusters.ListenAddressFlagName, localclusters.DomainFlagName, localClusterFlagArg(id))
	}
	if err := refuseExistingConfig(id.ConfigPath, o.Force); err != nil {
		return err
	}
	flags := localclusters.ConfigFlags{ListenAddress: o.ListenAddress, Domain: o.Domain}
	write := localclusters.WriteConfig
	if o.Defaults {
		write = p.writeDefaultedLocalConfig
	}
	if err := write(id, flags); err != nil {
		return err
	}
	fmt.Fprintf(w, "wrote %s\n", id.ConfigPath)
	// The default local cluster's config is written without a listen
	// address when the host's IP address can't be detected: say what to
	// set.
	if _, err := localclusters.LoadConfig(id); err != nil {
		fmt.Fprintf(w, "note: %v\n", err)
	}
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

// writeDefaultedLocalConfig writes the config of the local cluster id
// that localclusters.WriteConfig writes from flags, with every CLI
// default applied, as applyLocalConfigDefaults applies them.
func (p *ProjectInfo) writeDefaultedLocalConfig(id localclusters.Identity, flags localclusters.ConfigFlags) error {
	cfg, isHostIP, err := localclusters.ConfigFromFlags(id, flags)
	if err != nil {
		return err
	}
	// Only a config with no listen address gets no domain, and
	// runLocalConfigInit says what to set then.
	_ = p.applyLocalConfigDefaults(cfg)
	body, err := config.MarshalLocalConfig(cfg, isHostIP)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(id.ConfigPath), 0o755); err != nil {
		return fmt.Errorf("create the folder of %s: %w", id.ConfigPath, err)
	}
	if err := os.WriteFile(id.ConfigPath, body, 0o644); err != nil {
		return fmt.Errorf("write %s: %w", id.ConfigPath, err)
	}
	return nil
}

// applyLocalConfigDefaults applies every CLI default to cfg: the static
// WithDefaults, the CLI image registry and version, and, when no
// ingress.domain is set, the nip.io name of cluster.listenAddress with
// ingress.insecure. domainErr says why the domain could not be derived,
// in which case it is left unset.
func (p *ProjectInfo) applyLocalConfigDefaults(cfg *v1alpha1.EducatesLocalConfig) (domainErr error) {
	cfg.WithDefaults()
	cfg.ApplyCLIDefaults(p.Version, p.ImageRepository)
	return localclusters.DeriveDomain(cfg)
}
