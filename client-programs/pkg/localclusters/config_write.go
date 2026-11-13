package localclusters

import (
	"cmp"
	"errors"
	"fmt"
	"io/fs"
	"net/netip"
	"os"
	"path/filepath"

	"github.com/educates/educates-training-platform/client-programs/pkg/config"
	"github.com/educates/educates-training-platform/client-programs/pkg/config/hostinfo"
	"github.com/educates/educates-training-platform/client-programs/pkg/config/v1alpha1"
)

// Names of the flags a local cluster's missing config is written from.
const (
	ListenAddressFlagName = "listen-address"
	DomainFlagName        = "domain"
)

// ConfigFlags are the values of the --listen-address and --domain flags,
// from which a local cluster's missing config is written. An empty value
// means the flag was not given.
type ConfigFlags struct {
	// ListenAddress is the cluster.listenAddress to write. Empty writes
	// the host's IP address for the default local cluster, and is an
	// error for a named one.
	ListenAddress string

	// Domain is the ingress.domain to write. Empty writes none, so the
	// cluster takes the nip.io name of its listen address.
	Domain string
}

// IsZero reports whether neither flag was given.
func (f ConfigFlags) IsZero() bool {
	return f == ConfigFlags{}
}

// MissingNamedConfigError is the error for the named local cluster id
// having no config, naming both ways to write one. It wraps
// fs.ErrNotExist.
func MissingNamedConfigError(id Identity) error {
	return &missingConfigError{message: fmt.Sprintf(`local cluster %q has no config at %s. Write one with:

  educates local config init --%s %s --%s <ip>

or create the cluster and write its config in one step with:

  educates local cluster create --%s %s --%s <ip>

Add --%s <domain> to either to use a domain of your own instead of the
nip.io name of the listen address.`,
		id.Name, id.ConfigPath,
		SelectionFlagName, id.Name, ListenAddressFlagName,
		SelectionFlagName, id.Name, ListenAddressFlagName,
		DomainFlagName)}
}

// ConfigFromFlags returns the config the CLI writes for the local cluster
// id when it has none: an EducatesLocalConfig that declares
// cluster.listenAddress and, when flags.Domain is set, ingress.domain. It
// also reports whether the listen address is the host's IP address.
//
// The listen address is flags.ListenAddress. Without one, the default
// local cluster takes the host's IP address, and with none detected the
// config declares no listen address, so the cluster can't be used until
// one is set. A named local cluster needs flags.ListenAddress, which must
// be one the cluster can use: it must pass ValidateListenAddress, and
// without flags.Domain have a nip.io name.
func ConfigFromFlags(id Identity, flags ConfigFlags) (*v1alpha1.EducatesLocalConfig, bool, error) {
	listenAddress, isHostIP := flags.ListenAddress, false
	if listenAddress == "" && id.IsDefault() {
		if ip, err := hostinfo.DetectHostIP(); err == nil && ValidateListenAddress(ip) == nil {
			listenAddress, isHostIP = ip, true
		}
	}
	if listenAddress != "" || !id.IsDefault() {
		if err := ValidateListenAddress(listenAddress); err != nil {
			return nil, false, fmt.Errorf("--%s: %w", ListenAddressFlagName, err)
		}
		if _, ok := nipDomain(listenAddress); !ok && flags.Domain == "" {
			return nil, false, fmt.Errorf("--%s %s %s: pass --%s with a domain that resolves to it",
				ListenAddressFlagName, listenAddress, noNipDomain, DomainFlagName)
		}
	}
	return &v1alpha1.EducatesLocalConfig{
		TypeMeta: v1alpha1.TypeMeta{
			APIVersion: v1alpha1.APIVersion,
			Kind:       v1alpha1.KindEducatesLocalConfig,
		},
		Cluster: v1alpha1.LocalClusterConfig{ListenAddress: listenAddress},
		Ingress: v1alpha1.LocalIngressConfig{Domain: flags.Domain},
	}, isHostIP, nil
}

// ConfigYAML returns the config ConfigFromFlags returns for the local
// cluster id, as config.MarshalLocalConfig writes it: with a comment on a
// listen address that is the host's IP address.
func ConfigYAML(id Identity, flags ConfigFlags) ([]byte, error) {
	cfg, isHostIP, err := ConfigFromFlags(id, flags)
	if err != nil {
		return nil, err
	}
	body, err := config.MarshalLocalConfig(cfg, isHostIP)
	if err != nil {
		return nil, err
	}
	// A config with a listen address is one LoadConfigFile takes, so a
	// config the cluster can't use is never written, unless the default
	// local cluster's has none for want of a host IP address.
	if cfg.Cluster.ListenAddress != "" {
		if _, err := parseConfig(id, body, id.ConfigPath); err != nil {
			return nil, err
		}
	}
	return body, nil
}

// WriteConfig writes the config ConfigYAML returns for the local cluster
// id to its config path. It replaces a file already there, so a caller
// that must not overwrite one checks first, as EnsureConfig does.
func WriteConfig(id Identity, flags ConfigFlags) error {
	body, err := ConfigYAML(id, flags)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(id.ConfigPath), 0o755); err != nil {
		return fmt.Errorf("create the folder of the config of %s: %w", id.Description(), err)
	}
	if err := os.WriteFile(id.ConfigPath, body, 0o644); err != nil {
		return fmt.Errorf("write %s: %w", id.ConfigPath, err)
	}
	return nil
}

// EnsureConfig makes sure the local cluster id has a config in the data
// home, writing it from flags when it has none, and reports whether it
// wrote it.
//
// With no file at the cluster's config path, it writes one with
// WriteConfig, except for a named local cluster when flags give no listen
// address: that is MissingNamedConfigError. With a file there it writes
// nothing, and a flag whose value disagrees with what the file declares
// is an error: the flags write a missing config, and never change one.
func EnsureConfig(id Identity, flags ConfigFlags) (bool, error) {
	switch err := CheckConfigExists(id); {
	case err == nil:
		return false, checkFlagsAgree(id, flags)
	case !errors.Is(err, fs.ErrNotExist), !id.IsDefault() && flags.ListenAddress == "":
		return false, err
	}
	if err := WriteConfig(id, flags); err != nil {
		return false, err
	}
	return true, nil
}

// checkFlagsAgree checks that each flag given agrees with what the
// existing config of the local cluster id declares.
func checkFlagsAgree(id Identity, flags ConfigFlags) error {
	if flags.IsZero() {
		return nil
	}
	cfg, err := config.LoadWithoutDefaults(id.ConfigPath)
	if err != nil {
		return err
	}
	declared, _, err := declaredConfig(id, cfg)
	if err != nil {
		return fmt.Errorf("%s: %w", id.ConfigPath, err)
	}
	listenAddressField := cmp.Or(declared.listenAddressField, "cluster.listenAddress")
	if flags.ListenAddress != "" && !sameAddress(flags.ListenAddress, declared.listenAddress) {
		return disagreement(id, ListenAddressFlagName, flags.ListenAddress, listenAddressField, declared.listenAddress,
			"declares no "+listenAddressField)
	}
	if flags.Domain != "" && flags.Domain != declared.domain {
		return disagreement(id, DomainFlagName, flags.Domain, "ingress.domain", declared.domain,
			"declares none, so the cluster takes the nip.io name of its listen address")
	}
	return nil
}

// disagreement is the error for the flag flag's value disagreeing with
// the field field of the existing config of the local cluster id, which
// declares value, or nothing as none says.
func disagreement(id Identity, flag, flagValue, field, value, none string) error {
	declares := fmt.Sprintf("declares %s %s", field, value)
	if value == "" {
		declares = none
	}
	return fmt.Errorf("--%s %s disagrees with the config of %s at %s, which %s. "+
		"The flag only writes a missing config and never changes one: leave it out, "+
		"or edit the config", flag, flagValue, id.Description(), id.ConfigPath, declares)
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
