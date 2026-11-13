package localclusters

import (
	"errors"
	"fmt"
	"net/netip"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v2"

	"github.com/educates/educates-training-platform/client-programs/pkg/config"
	"github.com/educates/educates-training-platform/client-programs/pkg/config/v1alpha1"
)

// Names of the flags a named local cluster's missing config is written
// from.
const (
	ListenAddressFlagName = "listen-address"
	DomainFlagName        = "domain"
)

// NamedConfigFlags are the values of the --listen-address and --domain
// flags, from which a named local cluster's missing config is written.
// An empty value means the flag was not given.
type NamedConfigFlags struct {
	// ListenAddress is the cluster.listenAddress to write.
	ListenAddress string

	// Domain is the ingress.domain to write. Empty writes none, so the
	// cluster takes the nip.io name of its listen address.
	Domain string
}

// IsZero reports whether neither flag was given.
func (f NamedConfigFlags) IsZero() bool {
	return f == NamedConfigFlags{}
}

// ConfigFlagsOnDefaultClusterError is the error for --listen-address or
// --domain given while the default local cluster is selected: the flags
// write only a named local cluster's config.
func ConfigFlagsOnDefaultClusterError() error {
	return fmt.Errorf("--%s and --%s write the config of a named local cluster; select one with --%s or %s",
		ListenAddressFlagName, DomainFlagName, FlagName, SelectionEnvVar)
}

// MissingNamedConfigError is the error for the named local cluster id
// having no config, naming both ways to write one.
func MissingNamedConfigError(id Identity) error {
	return fmt.Errorf(`local cluster %q has no config at %s. Write one with:

  educates local config init --%s %s --%s <ip>

or create the cluster and write its config in one step with:

  educates local cluster create --%s %s --%s <ip>

Add --%s <domain> to either to use a domain of your own instead of the
nip.io name of the listen address.`,
		id.Name, id.ConfigPath,
		FlagName, id.Name, ListenAddressFlagName,
		FlagName, id.Name, ListenAddressFlagName,
		DomainFlagName)
}

// WriteNamedConfig writes the config of the named local cluster id to
// its config path from flags: an EducatesLocalConfig that declares
// cluster.listenAddress and, when flags.Domain is set, ingress.domain. It
// replaces a file already there, so a caller that must not overwrite one
// checks first, as EnsureNamedConfig does.
//
// The listen address must pass ValidateListenAddress, and the config
// must be one LoadNamedConfig takes, so a config the cluster can't use is
// never written.
func WriteNamedConfig(id Identity, flags NamedConfigFlags) error {
	if id.IsDefault() {
		return errors.New("the default local cluster's config is not written from --" +
			ListenAddressFlagName + " and --" + DomainFlagName)
	}
	if err := ValidateListenAddress(flags.ListenAddress); err != nil {
		return fmt.Errorf("--%s: %w", ListenAddressFlagName, err)
	}
	if _, ok := nipDomain(flags.ListenAddress); !ok && flags.Domain == "" {
		return fmt.Errorf("--%s %s %s: pass --%s with a domain that resolves to it",
			ListenAddressFlagName, flags.ListenAddress, noNipDomain, DomainFlagName)
	}
	body, err := yaml.Marshal(v1alpha1.EducatesLocalConfig{
		TypeMeta: v1alpha1.TypeMeta{
			APIVersion: v1alpha1.APIVersion,
			Kind:       v1alpha1.KindEducatesLocalConfig,
		},
		Cluster: v1alpha1.LocalClusterConfig{ListenAddress: flags.ListenAddress},
		Ingress: v1alpha1.LocalIngressConfig{Domain: flags.Domain},
	})
	if err != nil {
		return fmt.Errorf("marshal the config of local cluster %q: %w", id.Name, err)
	}
	body = append([]byte(config.DefaultLocalConfigModeline+"\n"), body...)
	if _, err := parseNamedConfig(body, id.ConfigPath); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(id.ConfigPath), 0o755); err != nil {
		return fmt.Errorf("create the folder of local cluster %q: %w", id.Name, err)
	}
	if err := os.WriteFile(id.ConfigPath, body, 0o644); err != nil {
		return fmt.Errorf("write %s: %w", id.ConfigPath, err)
	}
	return nil
}

// EnsureNamedConfig makes sure the named local cluster id has a config,
// writing it from flags when it has none, and reports whether it wrote
// it.
//
// With no file at the cluster's config path, it writes one with
// WriteNamedConfig when flags give a listen address, and returns
// MissingNamedConfigError when they don't. With a file there it writes
// nothing, and a flag whose value disagrees with what the file declares
// is an error: the flags write a missing config, and never change one.
func EnsureNamedConfig(id Identity, flags NamedConfigFlags) (bool, error) {
	if _, err := os.Stat(id.ConfigPath); err == nil {
		return false, checkFlagsAgree(id, flags)
	} else if !os.IsNotExist(err) {
		return false, fmt.Errorf("check for the config of local cluster %q: %w", id.Name, err)
	}
	if flags.ListenAddress == "" {
		return false, MissingNamedConfigError(id)
	}
	if err := WriteNamedConfig(id, flags); err != nil {
		return false, err
	}
	return true, nil
}

// checkFlagsAgree checks that each flag given agrees with what the
// existing config of the named local cluster id declares.
func checkFlagsAgree(id Identity, flags NamedConfigFlags) error {
	if flags.IsZero() {
		return nil
	}
	cfg, err := config.LoadWithoutDefaults(id.ConfigPath)
	if err != nil {
		return err
	}
	declared, err := declaredNamedConfig(cfg)
	if err != nil {
		return fmt.Errorf("%s: %w", id.ConfigPath, err)
	}
	if flags.ListenAddress != "" && !sameAddress(flags.ListenAddress, declared.listenAddress) {
		return disagreement(id, ListenAddressFlagName, flags.ListenAddress, declared.listenAddressField, declared.listenAddress,
			"declares no "+declared.listenAddressField)
	}
	if flags.Domain != "" && flags.Domain != declared.domain {
		return disagreement(id, DomainFlagName, flags.Domain, "ingress.domain", declared.domain,
			"declares none, so the cluster takes the nip.io name of its listen address")
	}
	return nil
}

// disagreement is the error for the flag flag's value disagreeing with
// the field field of the existing config of the named local cluster id,
// which declares value, or nothing as none says.
func disagreement(id Identity, flag, flagValue, field, value, none string) error {
	declares := fmt.Sprintf("declares %s %s", field, value)
	if value == "" {
		declares = none
	}
	return fmt.Errorf("--%s %s disagrees with the config of local cluster %q at %s, which %s. "+
		"The flag only writes a missing config and never changes one: leave it out, "+
		"or edit the config", flag, flagValue, id.Name, id.ConfigPath, declares)
}

// sameAddress reports whether a and b are the same IP address, or the
// same string when either is not an IP address.
func sameAddress(a, b string) bool {
	ipA, errA := netip.ParseAddr(a)
	ipB, errB := netip.ParseAddr(b)
	if errA != nil || errB != nil {
		return a == b
	}
	return ipA == ipB
}
