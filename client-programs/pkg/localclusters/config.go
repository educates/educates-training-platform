package localclusters

import (
	"fmt"
	"net/netip"

	"github.com/educates/educates-training-platform/client-programs/pkg/config"
	"github.com/educates/educates-training-platform/client-programs/pkg/config/hostinfo"
	"github.com/educates/educates-training-platform/client-programs/pkg/config/v1alpha1"
)

// validateClusterNodes checks a declared node list: when set, it must
// contain exactly one control-plane node and use only the roles kind
// understands. kind cannot bootstrap without a control-plane node, and the
// router sends the cluster's names to its one control-plane node, by its
// container name. An empty list keeps the default
// single control-plane cluster and needs no validation. The error gives
// the reason, without naming the field the list came from.
func validateClusterNodes(nodes []v1alpha1.ClusterNode) error {
	if len(nodes) == 0 {
		return nil
	}

	controlPlanes := 0
	for _, n := range nodes {
		switch n.Role {
		case "control-plane":
			controlPlanes++
		case "worker":
		default:
			return fmt.Errorf("invalid node role %q, which must be \"control-plane\" or \"worker\"", n.Role)
		}
	}

	switch {
	case controlPlanes == 0:
		return fmt.Errorf("one node must have role \"control-plane\"")
	case controlPlanes > 1:
		return fmt.Errorf("%d nodes have role \"control-plane\", but a local cluster has exactly one, the node the router sends the cluster's names to; give the other nodes role \"worker\"", controlPlanes)
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
// A config that declares a local cluster declares a node list, when it
// declares one, that passes validateClusterNodes. It needs no address of
// its own: every local cluster is served through the router, on the
// router address. An EducatesLocalConfig that declares no ingress.domain
// gets a nip.io name of the router address, as DeriveDomain gives it,
// with ingress.insecure set to true, so it is served over plain HTTP with
// no CA to manage. It then gets the static defaults config.Load gives it.
// An EducatesConfig gets no fallback and no defaults: its domain is the
// one its custom resources declare.
//
// A config in the data home that still declares registry mirrors in its
// cluster block has them moved to the registry mirrors file first, as
// config.ReadConfigFile does.
func LoadConfigFile(id Identity, path string) (v1alpha1.Config, error) {
	data, err := config.ReadConfigFile(path)
	if err != nil {
		return nil, err
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
	if err := validateClusterNodes(declared.nodes); err != nil {
		return nil, fmt.Errorf("%s: %s: %w", source, declared.nodesField, err)
	}
	if c, ok := cfg.(*v1alpha1.EducatesLocalConfig); ok {
		if err := DeriveDomain(id, c); err != nil {
			return nil, fmt.Errorf("%s: %w", source, err)
		}
	}
	return config.ApplyDefaults(cfg), nil
}

// DeriveDomain gives the EducatesLocalConfig c of the local cluster id,
// when it declares no ingress.domain, the nip.io name derivedDomain gives
// it from the router address, the one address every local cluster is
// served on, and ingress.insecure set to true, as LoadConfigFile does:
// <address>.nip.io for the default local cluster, and
// <name>.<address>.nip.io for a named one, under the default local
// cluster's, both with the address's dots as dashes.
//
// It returns why it can't, leaving c as it is: a router file, router.yaml
// in the data home, that is missing or does not load, a router address
// the router address rules refuse, or one with no nip.io name. A missing
// router file is not a missing config, so the error never wraps
// fs.ErrNotExist, which callers take a missing config by.
func DeriveDomain(id Identity, c *v1alpha1.EducatesLocalConfig) error {
	if c.Ingress.Domain != "" {
		return nil
	}
	routerAddress, err := configuredRouterAddress(id.DataHome)
	if err != nil {
		return fmt.Errorf("ingress.domain is not set, so the cluster takes a nip.io name of the router address: %v", err)
	}
	domain, ok := derivedDomain(id, routerAddress)
	if !ok {
		return fmt.Errorf("the router address %s, in %s, %s: set ingress.domain to a domain that resolves to it",
			routerAddress, config.LocalRouterPath(id.DataHome), noNipDomain)
	}
	c.Ingress.Domain = domain
	c.Ingress.Insecure = true
	return nil
}

// derivedDomain returns the nip.io name the local cluster id takes when
// its config declares no ingress domain, with routerAddress the router
// address: the nip.io name of the router address, with dashes, for the
// default local cluster, and the cluster's name under it for a named one.
// It reports false when the router address has no nip.io name.
func derivedDomain(id Identity, routerAddress string) (string, bool) {
	domain, ok := nipDomain(routerAddress)
	if !ok || id.IsDefault() {
		return domain, ok
	}
	return id.Name + "." + domain, true
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

// nipDomain returns the nip.io name of the IP address address, with
// dashes. nip.io names only IPv4 addresses, so an IPv6 address has none,
// and nipDomain reports false.
func nipDomain(address string) (string, bool) {
	if ip, err := netip.ParseAddr(address); err != nil || !ip.Is4() {
		return "", false
	}
	return hostinfo.NipDomain(address), true
}
