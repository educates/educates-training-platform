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
	"github.com/educates/educates-training-platform/client-programs/pkg/utils"
)

// localConfigSchemaModeline is the yaml-language-server modeline every
// EducatesLocalConfig the command line tool writes starts with.
const localConfigSchemaModeline = config.DefaultLocalConfigModeline

var localConfigInitExample = `
  # Write a minimal EducatesLocalConfig:
  educates local config init

  # Write it on a domain of your own:
  educates local config init --domain educates.test

  # Overwrite an existing config file:
  educates local config init --force

  # Write the fully-defaulted configuration:
  educates local config init --defaults

  # Write the config of the named local cluster test2:
  educates local config init --local-cluster test2
`

type LocalConfigInitOptions struct {
	Force    bool
	Defaults bool

	// Domain is the value of --domain.
	Domain string
}

func (p *ProjectInfo) NewLocalConfigInitCmd() *cobra.Command {
	var o LocalConfigInitOptions

	c := &cobra.Command{
		Args:  cobra.NoArgs,
		Use:   "init",
		Short: "Write an EducatesLocalConfig to <data-home>/config.yaml",
		Long: `Creates <data-home>/config.yaml. By default it writes the minimum
EducatesLocalConfig: apiVersion and kind, and ingress.domain when --domain
gives one, leaving every other field to take its default at deploy time.
With --defaults it instead writes the fully-defaulted configuration, with
the static defaults, the CLI image registry and version, and the nip.io
name of the router address as the domain (and the insecure default it
implies) materialised into the file. Errors if the file already exists
unless --force.

A local cluster has no address of its own: every local cluster is served
through the router, on the router address. Without --domain the default
local cluster takes the nip.io name of the router address, such as
10-10-10-1.nip.io, and a named one its name under it, such as
test2.10-10-10-1.nip.io.

--local-cluster <name>, or EDUCATES_LOCAL_CLUSTER, writes the config of
a named local cluster instead, to <data-home>/clusters/<name>/config.yaml.
--defaults writes only the default local cluster's config.

Before the config, init writes <data-home>/router.yaml when it is
missing: the router address, the one host address every local cluster
shares, set to the host's IP address, with a comment saying that an
alias such as 10.10.10.1, added to a network interface, survives network
changes. An existing router.yaml is left as it is. With no IP address of
the host detected, init writes nothing, and says how to set the router
address.

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
	c.Flags().StringVar(&o.Domain, localclusters.DomainFlagName, "", "ingress domain of the local cluster (default: the nip.io name of the router address for the default local cluster, and the cluster's name under it for a named one)")
	return c
}

// runLocalConfigInit writes the config of the local cluster id from
// --domain, as localclusters.WriteConfig does. The default local cluster
// with --defaults gets every CLI default written too. A named local
// cluster's config declares only what --domain gives, so it takes no
// --defaults: 'local config view' shows the defaults it gets. A missing
// router file is written first, as ensureRouterFile writes it.
func (p *ProjectInfo) runLocalConfigInit(o *LocalConfigInitOptions, id localclusters.Identity, w io.Writer) error {
	if o.Defaults && !id.IsDefault() {
		return fmt.Errorf("--defaults writes the default local cluster's config only; a named local cluster's config "+
			"declares what --%s gives, and 'educates local config view%s' shows the defaults it gets",
			localclusters.DomainFlagName, localClusterFlagArg(id))
	}
	if err := refuseExistingConfig(id.ConfigPath, o.Force); err != nil {
		return err
	}
	// The router file comes first, so that no config is written without
	// it, and init can be run again once the router address is set.
	if _, err := ensureRouterFile(w); err != nil {
		return err
	}
	flags := localclusters.ConfigFlags{Domain: o.Domain}
	write := localclusters.WriteConfig
	if o.Defaults {
		write = p.writeDefaultedLocalConfig
	}
	if err := write(id, flags); err != nil {
		return err
	}
	fmt.Fprintf(w, "wrote %s\n", id.ConfigPath)
	return nil
}

// ensureRouterFile writes the router file of the data home, router.yaml,
// when it is missing, with the host's IP address as the router address,
// as localclusters.EnsureRouterFile writes it, and tells w when it does,
// reporting whether it did. An existing router file is left as it is.
//
// The default local cluster's config, as an earlier release wrote it, may
// still declare the address its cluster was served on, which becomes the
// router address in place of the host's IP address: it is moved first,
// as config.MoveClusterListenAddress moves it, with a note on standard
// error, whichever local cluster the command is for.
func ensureRouterFile(w io.Writer) (written bool, err error) {
	dataHome := utils.GetEducatesHomeDir()
	if err := config.MoveClusterListenAddress(dataHome, config.LocalConfigPath(dataHome), os.Stderr); err != nil {
		return false, err
	}
	address, err := localclusters.EnsureRouterFile(dataHome)
	if err != nil {
		return false, err
	}
	if address == "" {
		return false, nil
	}
	fmt.Fprintf(w, "wrote %s, with this host's IP address %s as the router address\n",
		config.LocalRouterPath(dataHome), address)
	return true, nil
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
// default applied, as applyLocalConfigDefaults applies them. A domain it
// can't derive, as from a router address with no nip.io name, is an
// error, and nothing is written.
func (p *ProjectInfo) writeDefaultedLocalConfig(id localclusters.Identity, flags localclusters.ConfigFlags) error {
	cfg := localclusters.ConfigFromFlags(flags)
	if err := p.applyLocalConfigDefaults(id, cfg); err != nil {
		return fmt.Errorf("%s: %w", id.ConfigPath, err)
	}
	body, err := config.MarshalLocalConfig(cfg)
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

// applyLocalConfigDefaults applies every CLI default to cfg, the config
// of the local cluster id: the static WithDefaults, the CLI image
// registry and version, and, when no ingress.domain is set, the nip.io
// name localclusters.DeriveDomain derives, with ingress.insecure.
// domainErr says why the domain could not be derived, in which case it is
// left unset.
func (p *ProjectInfo) applyLocalConfigDefaults(id localclusters.Identity, cfg *v1alpha1.EducatesLocalConfig) (domainErr error) {
	cfg.WithDefaults()
	cfg.ApplyCLIDefaults(p.Version, p.ImageRepository)
	return localclusters.DeriveDomain(id, cfg)
}
