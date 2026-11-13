package localclusters

import (
	"fmt"
	"net/netip"
	"os"

	"github.com/educates/educates-training-platform/client-programs/pkg/config"
	"github.com/educates/educates-training-platform/client-programs/pkg/config/hostinfo"
	"github.com/educates/educates-training-platform/client-programs/pkg/config/v1alpha1"
)

// ValidateListenAddress checks that address can be a named local
// cluster's listen address, the host address its ports 80 and 443 are
// published on: a specific IP address, neither the unspecified address
// that means every host address nor a loopback address. An IPv4-mapped
// IPv6 address is checked as the IPv4 address it maps. The error gives
// the reason, without naming where the address came from.
func ValidateListenAddress(address string) error {
	if address == "" {
		return fmt.Errorf("must be set to a specific IP address of the host, such as 10.10.10.2, " +
			"to publish the cluster's ports 80 and 443 on")
	}
	ip, err := netip.ParseAddr(address)
	if err != nil {
		return fmt.Errorf("%q is not an IP address", address)
	}
	ip = ip.Unmap()
	if ip.IsUnspecified() {
		return fmt.Errorf("%s means every host address, and a named local cluster needs a specific IP "+
			"address of the host of its own to publish its ports 80 and 443 on", address)
	}
	if ip.IsLoopback() {
		return fmt.Errorf("%s is a loopback address, which can't work for a named local cluster: "+
			"pods resolve the cluster's ingress domain to it, and reach their own loopback "+
			"interface instead of the host", address)
	}
	return nil
}

// LoadNamedConfig loads the config of a named local cluster from the file
// at path and applies the rules a named cluster's config follows. The
// config is an EducatesLocalConfig, or an EducatesConfig with
// target.provider: kind, and its listen address passes
// ValidateListenAddress.
//
// An EducatesLocalConfig that declares no ingress.domain gets the nip.io
// name of its listen address, with dashes, and ingress.insecure set to
// true, so it is served over plain HTTP with no CA to manage. It then
// gets the static defaults config.Load gives it. An EducatesConfig gets
// no fallback and no defaults: its domain is the one its custom
// resources declare.
func LoadNamedConfig(path string) (v1alpha1.Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	return parseNamedConfig(data, path)
}

// CheckNamedConfig checks that data, a named local cluster's config read
// from or to be written to source, follows the rules LoadNamedConfig
// applies, so a change to a config is refused before it is written.
func CheckNamedConfig(data []byte, source string) error {
	_, err := parseNamedConfig(data, source)
	return err
}

// parseNamedConfig is LoadNamedConfig for the config data, read from
// source.
func parseNamedConfig(data []byte, source string) (v1alpha1.Config, error) {
	cfg, err := config.LoadBytesWithoutDefaults(data, source)
	if err != nil {
		return nil, err
	}
	declared, err := declaredNamedConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", source, err)
	}
	if err := ValidateListenAddress(declared.listenAddress); err != nil {
		return nil, fmt.Errorf("%s: %s: %w", source, declared.listenAddressField, err)
	}
	if c, ok := cfg.(*v1alpha1.EducatesLocalConfig); ok && c.Ingress.Domain == "" {
		domain, ok := nipDomain(c.Cluster.ListenAddress)
		if !ok {
			return nil, fmt.Errorf("%s: cluster.listenAddress %s %s: set ingress.domain to a domain that resolves to it",
				source, c.Cluster.ListenAddress, noNipDomain)
		}
		c.Ingress.Domain = domain
		c.Ingress.Insecure = true
	}
	return config.ApplyDefaults(cfg), nil
}

// namedConfigValues is what a named local cluster's config declares
// about the cluster's host address and domain, before any rule or
// default applies.
type namedConfigValues struct {
	// listenAddressField is the path of the field that declares the
	// listen address.
	listenAddressField string

	// listenAddress is the declared listen address, empty when none is.
	listenAddress string

	// domain is the declared ingress domain, empty when none is.
	domain string
}

// declaredNamedConfig returns what cfg, loaded without defaults, declares
// about a named local cluster, or an error when cfg is of a kind a named
// local cluster can't take.
func declaredNamedConfig(cfg v1alpha1.Config) (namedConfigValues, error) {
	switch c := cfg.(type) {
	case *v1alpha1.EducatesLocalConfig:
		return namedConfigValues{
			listenAddressField: "cluster.listenAddress",
			listenAddress:      c.Cluster.ListenAddress,
			domain:             c.Ingress.Domain,
		}, nil
	case *v1alpha1.EducatesConfig:
		if c.Target != nil && c.Target.Provider == "kind" {
			return namedConfigValues{
				listenAddressField: "target.cluster.listenAddress",
				listenAddress:      c.Target.Cluster.ListenAddress,
				domain:             c.IngressDomain(),
			}, nil
		}
	}
	return namedConfigValues{}, fmt.Errorf("a named local cluster's config is an %s, or an %s with target.provider: kind, not this %s",
		v1alpha1.KindEducatesLocalConfig, v1alpha1.KindEducatesConfig, describeKind(cfg))
}

// describeKind names cfg's kind for an error, saying when an
// EducatesConfig lacks the kind target.
func describeKind(cfg v1alpha1.Config) string {
	if c, ok := cfg.(*v1alpha1.EducatesConfig); ok {
		if c.Target == nil || c.Target.Provider == "" {
			return v1alpha1.KindEducatesConfig + " without target.provider"
		}
		return v1alpha1.KindEducatesConfig + " with target.provider: " + c.Target.Provider
	}
	return cfg.GetKind()
}

// noNipDomain says why an address has no nip.io name.
const noNipDomain = "has no nip.io name, which nip.io gives only IPv4 addresses"

// nipDomain returns the nip.io name of the listen address address, with
// dashes. nip.io names only IPv4 addresses, so an IPv6 address has none,
// and nipDomain reports false.
func nipDomain(address string) (string, bool) {
	if ip, err := netip.ParseAddr(address); err != nil || !ip.Is4() {
		return "", false
	}
	return hostinfo.NipDomain(address), true
}
