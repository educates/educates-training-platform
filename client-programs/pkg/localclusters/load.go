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

// LoadedConfig is the config of a local cluster, loaded from its file in
// the data home or from a file given for the cluster, with what it
// declares about the cluster on the host.
type LoadedConfig struct {
	// Identity is the local cluster's identity.
	Identity Identity

	// Config is the cluster's config, which follows the rules of
	// LoadConfigFile.
	Config v1alpha1.Config

	// Source is the file the config was loaded from, named in errors.
	Source string
}

// LoadConfig loads the config of the local cluster id from its file in
// the data home, by the rules of LoadConfigFile.
//
// A cluster with no config there is an error that says how to write one
// and wraps fs.ErrNotExist, so a caller to which a missing config is no
// error tells it apart with errors.Is: MissingNamedConfigError for a
// named local cluster, and config.MissingLocalConfigError for the default
// one.
func LoadConfig(id Identity) (LoadedConfig, error) {
	if err := CheckConfigExists(id); err != nil {
		return LoadedConfig{}, err
	}
	cfg, err := LoadConfigFile(id, id.ConfigPath)
	if err != nil {
		return LoadedConfig{}, err
	}
	return LoadedConfig{Identity: id, Config: cfg, Source: id.ConfigPath}, nil
}

// CheckConfigExists returns nil when the local cluster id has a config in
// the data home, and otherwise the error LoadConfig returns for the
// missing config, or why the file could not be checked for.
func CheckConfigExists(id Identity) error {
	_, err := os.Stat(id.ConfigPath)
	switch {
	case err == nil:
		return nil
	case errors.Is(err, fs.ErrNotExist):
		if id.IsDefault() {
			return &missingConfigError{message: config.MissingLocalConfigError(filepath.Dir(id.ConfigPath)).Error()}
		}
		return MissingNamedConfigError(id)
	default:
		return fmt.Errorf("check for the config of %s: %w", id.Description(), err)
	}
}

// missingConfigError is the error for a local cluster with no config in
// the data home. Its message says how to write one, and it wraps
// fs.ErrNotExist.
type missingConfigError struct {
	message string
}

func (e *missingConfigError) Error() string {
	return e.message
}

func (e *missingConfigError) Unwrap() error {
	return fs.ErrNotExist
}

// ListenAddress returns the host address the config declares the
// cluster publishes its ports 80 and 443 on: cluster.listenAddress of an
// EducatesLocalConfig, or target.cluster.listenAddress of an
// EducatesConfig. It is empty when the config declares none, or is of
// neither kind.
func (c LoadedConfig) ListenAddress() string {
	fields, _ := clusterFieldsOf(c.Config)
	return fields.listenAddress
}

// Domain returns the cluster's ingress domain, declared or derived: the
// ingress domain the config declares, which for an EducatesLocalConfig
// that declares none LoadConfigFile sets to the nip.io name of its listen
// address. An EducatesConfig gets no CLI defaults, so its domain is the
// one its custom resources declare, and empty when they declare none.
func (c LoadedConfig) Domain() string {
	fields, _ := clusterFieldsOf(c.Config)
	return fields.domain
}

// clusterFields is what a local cluster's config declares about the
// cluster on the host, which the two kinds a local cluster takes declare
// in different fields.
type clusterFields struct {
	// listenAddressField is the path of the field that declares the
	// listen address.
	listenAddressField string

	// listenAddress is the declared listen address, empty when none is.
	listenAddress string

	// domain is the declared ingress domain, empty when none is.
	domain string

	// domainField is the path of the field that declares the ingress
	// domain.
	domainField string

	// resolver is what the config declares about the local DNS resolver.
	resolver v1alpha1.LocalResolverConfig

	// extraDomainsField is the path of the field that declares the
	// cluster's extra domains in the local DNS resolver.
	extraDomainsField string

	// derivesDomain reports whether a cluster whose config declares no
	// ingress domain takes the nip.io name of its address: an
	// EducatesLocalConfig does, and an EducatesConfig, which gets no CLI
	// defaults, does not.
	derivesDomain bool
}

// clusterFieldsOf returns what cfg declares about its local cluster, and
// reports whether cfg is of a kind a local cluster takes: an
// EducatesLocalConfig, or an EducatesConfig with a target block. An
// EducatesConfig without one still declares the domain of its custom
// resources.
func clusterFieldsOf(cfg v1alpha1.Config) (clusterFields, bool) {
	switch c := cfg.(type) {
	case *v1alpha1.EducatesLocalConfig:
		return clusterFields{
			listenAddressField: "cluster.listenAddress",
			listenAddress:      c.Cluster.ListenAddress,
			domain:             c.Ingress.Domain,
			domainField:        "ingress.domain",
			resolver:           c.Resolver,
			extraDomainsField:  "resolver.extraDomains",
			derivesDomain:      true,
		}, true
	case *v1alpha1.EducatesConfig:
		fields := clusterFields{domain: c.IngressDomain(), domainField: "educatesClusterConfig.ingress.domain"}
		if mode, _ := c.EducatesClusterConfig["mode"].(string); mode == "Inline" {
			fields.domainField = "educatesClusterConfig.inline.ingress.domain"
		}
		if c.Target == nil {
			return fields, false
		}
		fields.listenAddressField = "target.cluster.listenAddress"
		fields.listenAddress = c.Target.Cluster.ListenAddress
		fields.resolver = c.Target.Resolver
		fields.extraDomainsField = "target.resolver.extraDomains"
		return fields, true
	default:
		return clusterFields{}, false
	}
}
