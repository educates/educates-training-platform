package localclusters

import (
	"cmp"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"slices"
	"strings"

	"github.com/educates/educates-training-platform/client-programs/pkg/config/v1alpha1"
)

// ResolverInput is the config of a local cluster as a caller of
// Inventory.ResolverConfig gives it, in place of the cluster's config in
// the data home.
type ResolverInput struct {
	// LoadedConfig is the cluster's config. A named local cluster's has
	// been loaded with LoadNamedConfig.
	LoadedConfig

	// ResolverDomainOverride, when set, is the domain the local DNS
	// resolver serves for the cluster in place of the ingress domain its
	// config declares or derives. It changes only what the resolver
	// serves.
	ResolverDomainOverride string
}

// ResolverConfig returns the dnsmasq configuration of the local DNS
// resolver, which serves the domains of every local cluster, each
// pointing at its own cluster's target address, or the error that two
// clusters claim the same domain.
//
// The local clusters served are the default local cluster, when the data
// home holds its config, every named Educates cluster that has a config
// in the data home, so from its create to its delete, and every cluster
// given holds a config for. A config given replaces the one in the data
// home: it is the config of a cluster being created, or the default local
// cluster's config as the resolver commands are given it. A named
// cluster's config follows the rules of LoadNamedConfig.
//
// A config given that the resolver can't serve is an error. A config in
// the data home that does not load, or that the resolver can't serve, is
// another cluster's, so it is left out rather than failing every other
// cluster: ResolverConfig also returns an error for each one it leaves
// out, naming the cluster and, through why, its file, for the caller to
// warn about.
//
// Each cluster's ingress domain, declared or derived as
// LoadedConfig.Domain gives it with the observed host IP address, and
// its resolver.extraDomains point at its target address. The default
// local cluster's target address is resolver.targetAddress, or else the
// observed host IP address, or else 127.0.0.1. A named local cluster's
// target address is resolver.targetAddress, or else its listen address.
func (inv Inventory) ResolverConfig(given ...ResolverInput) ([]byte, []error, error) {
	byName := map[string]ResolverInput{}
	for _, c := range given {
		byName[c.Identity.Name] = c
	}

	var clusters []resolverCluster
	var leftOut []error
	for _, id := range inv.servedClusters(given) {
		in, isGiven := byName[id.Name]
		if !isGiven {
			loaded, err := LoadConfig(id)
			if errors.Is(err, fs.ErrNotExist) {
				continue
			}
			if err != nil {
				leftOut = append(leftOut, leftOutConfig(id, err))
				continue
			}
			in = ResolverInput{LoadedConfig: loaded}
		}
		rc, err := newResolverCluster(in, inv.observed.HostIP)
		if err != nil {
			if isGiven {
				return nil, nil, err
			}
			leftOut = append(leftOut, leftOutConfig(id, err))
			continue
		}
		clusters = append(clusters, rc)
	}
	if err := checkDomainsClaimedOnce(clusters); err != nil {
		return nil, nil, err
	}
	return renderDnsmasqConfig(clusters), leftOut, nil
}

// servedClusters returns the local clusters the resolver serves, as
// ResolverConfig says, when they have a config: the default local
// cluster first, then the named Educates clusters and the named clusters
// of given, by name.
func (inv Inventory) servedClusters(given []ResolverInput) []Identity {
	named := map[string]Identity{}
	for _, id := range inv.clusters {
		if !id.IsDefault() {
			named[id.Name] = id
		}
	}
	for _, c := range given {
		if !c.Identity.IsDefault() {
			named[c.Identity.Name] = c.Identity
		}
	}
	ids := append([]Identity{Default(inv.dataHome)}, slices.Collect(maps.Values(named))...)
	slices.SortFunc(ids, func(a, b Identity) int { return cmp.Compare(a.Name, b.Name) })
	return ids
}

// leftOutConfig is the error for the local DNS resolver leaving out the
// config of the local cluster id in the data home, which it can't serve
// because of err. err names the config's file.
func leftOutConfig(id Identity, err error) error {
	return fmt.Errorf("the local DNS resolver leaves out %s, whose config it can't use: %w", id.Description(), err)
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
					first.id.Description(), c.id.Description(), first.domain)
			}
			claims[key] = claim{id: c.id, domain: domain}
		}
	}
	return nil
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
func newResolverCluster(given ResolverInput, hostIP string) (resolverCluster, error) {
	id, source := given.Identity, given.Source
	fields, ok := clusterFieldsOf(given.Config)
	if !ok {
		if _, isEducatesConfig := given.Config.(*v1alpha1.EducatesConfig); isEducatesConfig {
			return resolverCluster{}, fmt.Errorf("%s: EducatesConfig has no target block, which the local DNS resolver takes target.resolver from", source)
		}
		return resolverCluster{}, fmt.Errorf("%s: unsupported kind %q for the local DNS resolver", source, given.Config.GetKind())
	}

	// A named local cluster is reached at its listen address, and the
	// default local cluster at the host's IP address, or at the loopback
	// address when that is not known.
	address := fields.listenAddress
	if id.IsDefault() {
		address = cmp.Or(hostIP, loopbackTargetAddress)
	}
	domain := cmp.Or(given.ResolverDomainOverride, given.Domain(hostIP))
	return resolverCluster{
		id:            id,
		domains:       servedDomains(domain, fields.resolver.ExtraDomains),
		targetAddress: cmp.Or(fields.resolver.TargetAddress, address),
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
