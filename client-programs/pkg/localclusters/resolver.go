package localclusters

import (
	"cmp"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/educates/educates-training-platform/client-programs/pkg/config"
	"github.com/educates/educates-training-platform/client-programs/pkg/config/v1alpha1"
)

// ResolverInputs is what the configuration of the local DNS resolver is
// rendered from.
type ResolverInputs struct {
	// DataHome is the data home whose local clusters the resolver serves.
	DataHome string

	// HostIP is the host's IP address, empty when it is not known. The
	// default local cluster's target address and nip.io domain default
	// to it.
	HostIP string

	// Observed are the kind node containers on the host, running or
	// stopped. A named local cluster is served while it has node
	// containers, so from its create to its delete.
	Observed []NodeContainer

	// Given are configs that local clusters are served from in place of
	// the ones in the data home, whether or not the clusters have node
	// containers: the config of a cluster being created, or the default
	// local cluster's config as the resolver commands are given it.
	Given []ClusterConfig
}

// ClusterConfig is the config of a local cluster, as a caller of
// RenderResolverConfig gives it.
type ClusterConfig struct {
	// ID is the local cluster's identity.
	ID Identity

	// Config is the cluster's config. A named local cluster's has been
	// loaded with LoadNamedConfig.
	Config v1alpha1.Config

	// Source is where the config was loaded from, named in errors.
	Source string

	// Domain, when set, is served in place of the ingress domain the
	// config declares or derives.
	Domain string
}

// RenderResolverConfig returns the dnsmasq configuration of the local DNS
// resolver, which serves the domains of every local cluster, each
// pointing at its own cluster's target address, or the error that two
// clusters claim the same domain.
//
// The local clusters served are the default local cluster, when the data
// home holds its config, every named local cluster that has a config in
// the data home and node containers among the observed ones, and every
// cluster a config is given for. A named cluster's config follows the
// rules of LoadNamedConfig.
//
// Each cluster's ingress domain, declared or derived, and its
// resolver.extraDomains point at its target address. The default local
// cluster's domain, when its EducatesLocalConfig declares none, is the
// nip.io name of the host's IP address, and its target address is
// resolver.targetAddress, or else the host's IP address, or else
// 127.0.0.1. A named local cluster's target address is
// resolver.targetAddress, or else its listen address.
func RenderResolverConfig(in ResolverInputs) ([]byte, error) {
	configs, err := servedConfigs(in)
	if err != nil {
		return nil, err
	}
	clusters := make([]resolverCluster, 0, len(configs))
	for _, c := range configs {
		rc, err := newResolverCluster(c, in.HostIP)
		if err != nil {
			return nil, err
		}
		clusters = append(clusters, rc)
	}
	if err := checkDomainsClaimedOnce(clusters); err != nil {
		return nil, err
	}
	return renderDnsmasqConfig(clusters), nil
}

// servedConfigs returns the configs of the local clusters the resolver
// serves, as RenderResolverConfig says, the default local cluster first
// and the named ones in name order.
func servedConfigs(in ResolverInputs) ([]ClusterConfig, error) {
	given := map[string]ClusterConfig{}
	for _, c := range in.Given {
		given[c.ID.Name] = c
	}

	var configs []ClusterConfig
	def := Default(in.DataHome)
	if c, ok := given[def.Name]; ok {
		configs = append(configs, c)
	} else if _, err := os.Stat(def.ConfigPath); err == nil {
		cfg, err := config.Load(def.ConfigPath)
		if err != nil {
			return nil, err
		}
		configs = append(configs, ClusterConfig{ID: def, Config: cfg, Source: def.ConfigPath})
	} else if !os.IsNotExist(err) {
		return nil, fmt.Errorf("check for the config of the default local cluster: %w", err)
	}

	withConfig, err := namedClustersWithConfig(in.DataHome)
	if err != nil {
		return nil, err
	}
	named := map[string]Identity{}
	for _, id := range withConfig {
		named[id.Name] = id
	}
	for _, c := range in.Given {
		if !c.ID.IsDefault() {
			named[c.ID.Name] = c.ID
		}
	}
	for _, name := range slices.Sorted(maps.Keys(named)) {
		if c, ok := given[name]; ok {
			configs = append(configs, c)
			continue
		}
		id := named[name]
		if len(id.nodes(in.Observed)) == 0 {
			continue
		}
		cfg, err := LoadNamedConfig(id.ConfigPath)
		if err != nil {
			return nil, err
		}
		configs = append(configs, ClusterConfig{ID: id, Config: cfg, Source: id.ConfigPath})
	}
	return configs, nil
}

// checkDomainsClaimedOnce checks that no two of clusters serve the same
// domain. dnsmasq can point a domain at one address only, but resolves
// the most specific domain it serves, so a subdomain of another cluster's
// domain is fine.
func checkDomainsClaimedOnce(clusters []resolverCluster) error {
	type claim struct {
		id     Identity
		domain string
	}
	claims := map[string]claim{}
	for _, c := range clusters {
		for _, domain := range c.domains {
			key := domainKey(domain)
			if first, ok := claims[key]; ok {
				return fmt.Errorf("%s and %s both claim the domain %q, which the local DNS resolver can point at one "+
					"local cluster only: change the ingress.domain or resolver.extraDomains of one of them",
					describeCluster(first.id), describeCluster(c.id), first.domain)
			}
			claims[key] = claim{id: c.id, domain: domain}
		}
	}
	return nil
}

// namedClustersWithConfig returns the identities of the named local
// clusters that have a config in the data home dataHome.
func namedClustersWithConfig(dataHome string) ([]Identity, error) {
	dir := filepath.Join(dataHome, "clusters")
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil, nil
	} else if err != nil {
		return nil, fmt.Errorf("read the local clusters in %s: %w", dir, err)
	}
	var ids []Identity
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		id, err := Named(entry.Name(), dataHome)
		if err != nil {
			continue
		}
		if _, err := os.Stat(id.ConfigPath); err != nil {
			continue
		}
		ids = append(ids, id)
	}
	return ids, nil
}

// resolverCluster is what the local DNS resolver serves for one local
// cluster: the domains that point at the cluster's target address.
type resolverCluster struct {
	id            Identity
	domains       []string
	targetAddress string
}

// newResolverCluster returns what the resolver serves for the local
// cluster of the config given, on the host with the IP address hostIP.
//
// The config is an EducatesLocalConfig, or an EducatesConfig with a
// target block. Only an EducatesLocalConfig that declares no ingress
// domain gets a nip.io one: an EducatesConfig gets no CLI defaults, so
// its domain is the one its custom resources declare, if any.
func newResolverCluster(given ClusterConfig, hostIP string) (resolverCluster, error) {
	id, source := given.ID, given.Source
	var (
		domain        string
		listenAddress string
		resolver      v1alpha1.LocalResolverConfig
		nipFallback   bool
	)
	switch c := given.Config.(type) {
	case *v1alpha1.EducatesLocalConfig:
		domain, listenAddress, resolver, nipFallback = c.Ingress.Domain, c.Cluster.ListenAddress, c.Resolver, true
	case *v1alpha1.EducatesConfig:
		if c.Target == nil {
			return resolverCluster{}, fmt.Errorf("%s: EducatesConfig has no target block, which the local DNS resolver takes target.resolver from", source)
		}
		domain, listenAddress, resolver = c.IngressDomain(), c.Target.Cluster.ListenAddress, c.Target.Resolver
	default:
		return resolverCluster{}, fmt.Errorf("%s: unsupported kind %q for the local DNS resolver", source, given.Config.GetKind())
	}

	// A named local cluster is reached at its listen address, and the
	// default local cluster at the host's IP address, or at the loopback
	// address when that is not known.
	address, nipAddress := listenAddress, listenAddress
	if id.IsDefault() {
		address, nipAddress = cmp.Or(hostIP, loopbackTargetAddress), hostIP
	}
	if domain == "" && nipFallback {
		domain, _ = nipDomain(nipAddress)
	}
	if given.Domain != "" {
		domain = given.Domain
	}
	return resolverCluster{
		id:            id,
		domains:       servedDomains(domain, resolver.ExtraDomains),
		targetAddress: cmp.Or(resolver.TargetAddress, address),
	}, nil
}

// loopbackTargetAddress is the default local cluster's target address
// when neither its config nor the host's IP address gives one.
const loopbackTargetAddress = "127.0.0.1"

// servedDomains returns the domains a local cluster's entries point at its
// target address: its ingress domain, when it has one, then its extra
// domains, each once, leaving out empty ones.
func servedDomains(domain string, extraDomains []string) []string {
	var domains []string
	seen := map[string]bool{}
	for _, d := range append([]string{domain}, extraDomains...) {
		key := domainKey(d)
		if key == "" || seen[key] {
			continue
		}
		seen[key] = true
		domains = append(domains, d)
	}
	return domains
}

// domainKey is the form of domain two domains are compared in: DNS names
// are case-insensitive.
func domainKey(domain string) string {
	return strings.ToLower(domain)
}

// dnsmasqHeader starts the resolver's configuration: names outside the
// local clusters' domains are forwarded to public DNS servers.
const dnsmasqHeader = `#log-queries
no-resolv
server=1.0.0.1
server=1.1.1.1
strict-order
`

// renderDnsmasqConfig renders the resolver's configuration serving
// clusters.
func renderDnsmasqConfig(clusters []resolverCluster) []byte {
	var b strings.Builder
	b.WriteString(dnsmasqHeader)
	for _, c := range clusters {
		heading := "Default local cluster"
		if !c.id.IsDefault() {
			heading = "Local cluster " + c.id.Name
		}
		fmt.Fprintf(&b, "\n# %s\n", heading)
		for _, domain := range c.domains {
			fmt.Fprintf(&b, "address=/%s/%s\n", domain, c.targetAddress)
		}
	}
	return []byte(b.String())
}

// describeCluster names the local cluster id in a sentence.
func describeCluster(id Identity) string {
	if id.IsDefault() {
		return "the default local cluster"
	}
	return fmt.Sprintf("local cluster %q", id.Name)
}
