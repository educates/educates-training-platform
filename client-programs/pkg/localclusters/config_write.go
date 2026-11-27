package localclusters

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/educates/educates-training-platform/client-programs/pkg/config"
	"github.com/educates/educates-training-platform/client-programs/pkg/config/v1alpha1"
)

// Names of the flags of the commands that write a local cluster's missing
// config.
const (
	// DomainFlagName names the --domain flag, from which a local cluster's
	// missing config is written.
	DomainFlagName = "domain"
)

// ConfigFlags are the values of the flags from which a local cluster's
// missing config is written. An empty value means the flag was not given.
type ConfigFlags struct {
	// Domain is the ingress.domain to write. Empty writes none, so the
	// cluster takes a nip.io name of the router address, as DeriveDomain
	// gives it.
	Domain string
}

// IsZero reports whether no flag was given.
func (f ConfigFlags) IsZero() bool {
	return f == ConfigFlags{}
}

// MissingNamedConfigError is the error for the named local cluster id
// having no config, naming both ways to write one. It wraps
// fs.ErrNotExist.
func MissingNamedConfigError(id Identity) error {
	return &missingConfigError{message: fmt.Sprintf(`local cluster %q has no config at %s. Write one with:

  educates local config init --%s %s

or create the cluster and write its config in one step with:

  educates local cluster create --%s %s

Add --%s <domain> to either to use a domain of your own instead of
%s.<router address with dashes>.nip.io.`,
		id.Name, id.ConfigPath,
		SelectionFlagName, id.Name,
		SelectionFlagName, id.Name,
		DomainFlagName, id.Name)}
}

// ConfigFromFlags returns the config the CLI writes for the local cluster
// id when it has none: an EducatesLocalConfig that declares
// ingress.domain when flags.Domain is set, and nothing else. A cluster
// needs no address of its own, since every local cluster is served on the
// router address, and without a domain it takes a nip.io name of the
// router address.
func ConfigFromFlags(flags ConfigFlags) *v1alpha1.EducatesLocalConfig {
	return &v1alpha1.EducatesLocalConfig{
		TypeMeta: v1alpha1.TypeMeta{
			APIVersion: v1alpha1.APIVersion,
			Kind:       v1alpha1.KindEducatesLocalConfig,
		},
		Ingress: v1alpha1.LocalIngressConfig{Domain: flags.Domain},
	}
}

// ConfigYAML returns the config ConfigFromFlags returns for the local
// cluster id, as config.MarshalLocalConfig writes it, once it follows the
// rules of LoadConfigFile, so a config the cluster can't use is never
// written: one without a domain takes it from the router address, which
// the router file of the data home must then declare.
func ConfigYAML(id Identity, flags ConfigFlags) ([]byte, error) {
	body, err := config.MarshalLocalConfig(ConfigFromFlags(flags))
	if err != nil {
		return nil, err
	}
	if _, err := parseConfig(id, body, id.ConfigPath); err != nil {
		return nil, err
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
// WriteConfig, for the default local cluster and a named one alike. With
// a file there it writes nothing, and a flag whose value disagrees with
// what the file declares is an error: the flags write a missing config,
// and never change one.
func EnsureConfig(id Identity, flags ConfigFlags) (bool, error) {
	switch err := CheckConfigExists(id); {
	case err == nil:
		return false, checkFlagsAgree(id, flags)
	case !errors.Is(err, fs.ErrNotExist):
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
	if flags.Domain != "" && flags.Domain != declared.domain {
		return disagreement(id, DomainFlagName, flags.Domain, "ingress.domain", declared.domain,
			"declares none, so the cluster takes a nip.io name of the router address")
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
