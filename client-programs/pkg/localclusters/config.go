package localclusters

import (
	"fmt"
	"net/netip"
	"os"

	"github.com/educates/educates-training-platform/client-programs/pkg/config"
	"github.com/educates/educates-training-platform/client-programs/pkg/config/hostinfo"
	"github.com/educates/educates-training-platform/client-programs/pkg/config/v1alpha1"
)

// ValidateListenAddress checks that address can be a local cluster's
// listen address, the host address its ports 80 and 443 are published
// on: a specific IP address, neither the unspecified address that means
// every host address nor a loopback address. An IPv4-mapped IPv6 address
// is checked as the IPv4 address it maps. The error gives the reason,
// without naming where the address came from.
func ValidateListenAddress(address string) error {
	if address == "" {
		return fmt.Errorf("must be set to a specific IP address of the host, which the cluster publishes " +
			"its ports 80 and 443 on")
	}
	ip, err := netip.ParseAddr(address)
	if err != nil {
		return fmt.Errorf("%q is not an IP address", address)
	}
	ip = ip.Unmap()
	if ip.IsUnspecified() {
		return fmt.Errorf("%s means every host address, and a local cluster needs a specific IP "+
			"address of the host of its own to publish its ports 80 and 443 on", address)
	}
	if ip.IsLoopback() {
		return fmt.Errorf("%s is a loopback address, which can't work for a local cluster: "+
			"pods resolve the cluster's ingress domain to it, and reach their own loopback "+
			"interface instead of the host", address)
	}
	return nil
}

// LoadConfigFile loads the config of the local cluster id from the file
// at path and applies the rules a local cluster's config follows.
//
// A named local cluster's config is an EducatesLocalConfig, or an
// EducatesConfig with target.provider: kind. The default local cluster's
// config may also be of a kind that declares no local cluster, which is
// taken as it is.
//
// A config that declares a local cluster declares a listen address that
// passes ValidateListenAddress. An EducatesLocalConfig that declares no
// ingress.domain gets the nip.io name of its listen address, with dashes,
// and ingress.insecure set to true, so it is served over plain HTTP with
// no CA to manage. It then gets the static defaults config.Load gives it.
// An EducatesConfig gets no fallback and no defaults: its domain is the
// one its custom resources declare.
func LoadConfigFile(id Identity, path string) (v1alpha1.Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	return parseConfig(id, data, path)
}

// CheckConfig checks that data, the config of the local cluster id read
// from or to be written to source, follows the rules LoadConfigFile
// applies, so a change to a config is refused before it is written.
func CheckConfig(id Identity, data []byte, source string) error {
	_, err := parseConfig(id, data, source)
	return err
}

// parseConfig is LoadConfigFile for the config data, read from source.
func parseConfig(id Identity, data []byte, source string) (v1alpha1.Config, error) {
	cfg, err := config.LoadBytesWithoutDefaults(data, source)
	if err != nil {
		return nil, err
	}
	declared, ok, err := declaredConfig(id, cfg)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", source, err)
	}
	if !ok {
		return config.ApplyDefaults(cfg), nil
	}
	if err := ValidateListenAddress(declared.listenAddress); err != nil {
		return nil, fmt.Errorf("%s: %s: %w. %s", source, declared.listenAddressField, err,
			listenAddressAdvice(id, source, declared.listenAddressField))
	}
	if c, ok := cfg.(*v1alpha1.EducatesLocalConfig); ok {
		if err := DeriveDomain(c); err != nil {
			return nil, fmt.Errorf("%s: %w", source, err)
		}
	}
	return config.ApplyDefaults(cfg), nil
}

// DeriveDomain gives the EducatesLocalConfig c, when it declares no
// ingress.domain, the nip.io name of its listen address, with dashes, and
// ingress.insecure set to true, as LoadConfigFile does. It returns why it
// can't, leaving c as it is: a listen address ValidateListenAddress
// refuses, or one with no nip.io name.
func DeriveDomain(c *v1alpha1.EducatesLocalConfig) error {
	if c.Ingress.Domain != "" {
		return nil
	}
	if err := ValidateListenAddress(c.Cluster.ListenAddress); err != nil {
		return fmt.Errorf("cluster.listenAddress: %w", err)
	}
	domain, ok := nipDomain(c.Cluster.ListenAddress)
	if !ok {
		return fmt.Errorf("cluster.listenAddress %s %s: set ingress.domain to a domain that resolves to it",
			c.Cluster.ListenAddress, noNipDomain)
	}
	c.Ingress.Domain = domain
	c.Ingress.Insecure = true
	return nil
}

// listenAddressAdvice says how to give the local cluster id, whose config
// read from source declares its listen address in the field field, a
// listen address that passes ValidateListenAddress. The default local
// cluster is offered the host's IP address, when it is known, and a named
// one an address of its own, which the default local cluster's would not
// be. The command that sets it is offered for an EducatesLocalConfig in
// the data home, which local config set edits.
func listenAddressAdvice(id Identity, source, field string) string {
	address, which := "<ip>", "an IP address of this host of the cluster's own"
	alias := "An alias such as 10.10.10.2, added to a network interface, is one."
	if id.IsDefault() {
		which = "an IP address of this host"
		if ip, err := hostinfo.DetectHostIP(); err == nil && ValidateListenAddress(ip) == nil {
			address, which = ip, "this host's IP address"
		}
		alias = "An alias such as 10.10.10.1, added to a network interface, survives network changes, " +
			"where the host's IP address may not."
	}
	if source != id.ConfigPath || field != "cluster.listenAddress" {
		if address != "<ip>" {
			which += ", " + address
		}
		return fmt.Sprintf("Set it in %s to %s. %s", source, which, alias)
	}
	return fmt.Sprintf("Set it to %s with:\n\n  educates local config set%s cluster.listenAddress %s\n\n%s",
		which, selectionFlagArg(id), address, alias)
}

// selectionFlagArg returns the --local-cluster argument, with a leading
// space, that selects the local cluster id in a command line shown to the
// user, or nothing for the default local cluster.
func selectionFlagArg(id Identity) string {
	if id.IsDefault() {
		return ""
	}
	return " --" + SelectionFlagName + " " + id.Name
}

// declaredConfig returns what cfg, the config of the local cluster id
// loaded without defaults, declares about the cluster, before any rule or
// default applies, and reports whether it declares a local cluster: an
// EducatesLocalConfig does, and an EducatesConfig with target.provider:
// kind. A named local cluster's config must, so for another config it
// returns an error.
func declaredConfig(id Identity, cfg v1alpha1.Config) (clusterFields, bool, error) {
	fields, ok := clusterFieldsOf(cfg)
	if c, isEducatesConfig := cfg.(*v1alpha1.EducatesConfig); isEducatesConfig && ok && c.Target.Provider != "kind" {
		ok = false
	}
	if id.IsDefault() {
		return fields, ok, nil
	}
	if !ok {
		return clusterFields{}, false, fmt.Errorf("a named local cluster's config is an %s, or an %s with target.provider: kind, not this %s",
			v1alpha1.KindEducatesLocalConfig, v1alpha1.KindEducatesConfig, describeKind(cfg))
	}
	return fields, true, nil
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
