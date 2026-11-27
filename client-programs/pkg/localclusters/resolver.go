package localclusters

import (
	"cmp"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"path/filepath"
	"slices"
	"strings"

	"github.com/educates/educates-training-platform/client-programs/pkg/config/v1alpha1"
)

// ResolverInput is the config of a local cluster as a caller of
// Inventory.ResolverConfig gives it, in place of the cluster's config in
// the data home.
type ResolverInput struct {
	// LoadedConfig is the cluster's config. A named local cluster's has
	// been loaded with LoadConfigFile.
	LoadedConfig

	// ResolverDomainOverride, when set, is the domain the local DNS
	// resolver serves for the cluster in place of the ingress domain its
	// config declares or derives. It changes only what the resolver
	// serves.
	ResolverDomainOverride string
}

// ResolverConfig returns the dnsmasq configuration of the local DNS
// resolver, which serves the domains of every local cluster, each
// pointing at the router address, or the error that two clusters claim
// the same domain.
//
// The local clusters served are the default local cluster, when the data
// home holds its config, every named Educates cluster that has a config
// in the data home, so from its create to its delete, and every cluster
// given holds a config for. A config given replaces the one in the data
// home: it is the config of a cluster being created, or the default local
// cluster's config as the resolver commands are given it. Every config
// follows the rules of LoadConfigFile.
//
// A config given that the resolver can't serve is an error. A config in
// the data home that does not load, or that the resolver can't serve, is
// another cluster's, so it is left out rather than failing every other
// cluster: ResolverConfig also returns an error for each one it leaves
// out, naming the cluster and, through why, its file, for the caller to
// warn about.
//
// Each cluster's ingress domain, declared or derived as
// LoadedConfig.Domain gives it, and its resolver.extraDomains point at the
// router address, which the router file of the data home, router.yaml,
// declares: the one host address the router publishes ports 80 and 443
// on, for every local cluster. With a cluster to serve, a router file that
// is missing or does not load, or an address the router address rules
// refuse, is an error; a missing file wraps fs.ErrNotExist. With none, the
// configuration serves the user's own entries only, and needs no router
// file.
func (inv Inventory) ResolverConfig(given ...ResolverInput) ([]byte, []error, error) {
	clusters, leftOut, err := inv.resolverClusters(given)
	if err != nil {
		return nil, nil, err
	}
	return renderDnsmasqConfig(clusters), leftOut, nil
}

// resolverClusters returns what the local DNS resolver serves for each
// local cluster it serves, in the order ResolverConfig renders them, and
// the configs it leaves out, as ResolverConfig says.
func (inv Inventory) resolverClusters(given []ResolverInput) ([]resolverCluster, []error, error) {
	byName := map[string]ResolverInput{}
	for _, c := range given {
		byName[c.Identity.Name] = c
	}

	// The router address is needed only once a cluster is served.
	routerAddress, routerErr := configuredRouterAddress(inv.dataHome)

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
		rc, err := newResolverCluster(in, inv.dataHome, routerAddress)
		if err != nil {
			if isGiven {
				return nil, nil, err
			}
			leftOut = append(leftOut, leftOutConfig(id, err))
			continue
		}
		clusters = append(clusters, rc)
	}
	if len(clusters) > 0 && routerErr != nil {
		return nil, nil, routerErr
	}
	claims := make([]claimedNames, 0, len(clusters))
	for _, c := range clusters {
		claims = append(claims, claimedNames{id: c.id, names: c.domains()})
	}
	if err := checkDomainsClaimedOnce(claims); err != nil {
		return nil, nil, err
	}
	return clusters, leftOut, nil
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
	return leftOutBy("the local DNS resolver", id, err)
}

// leftOutBy is the error for server, what serves the local clusters'
// names, leaving out the local cluster id, whose config it can't use
// because of err. err names the config's file.
func leftOutBy(server string, id Identity, err error) error {
	return fmt.Errorf("%s leaves out %s, whose config it can't use: %w", server, id.Description(), err)
}

// claimedNames is the names one local cluster claims: its ingress domain,
// declared or derived, and its extra domains.
type claimedNames struct {
	id    Identity
	names []string
}

// checkDomainsClaimedOnce checks that no two local clusters of claims
// claim the same domain. A name leads to one local cluster only, through
// the local DNS resolver and the router alike, but each of them takes the
// most specific domain that matches, so a subdomain of another cluster's
// domain is fine.
func checkDomainsClaimedOnce(claims []claimedNames) error {
	type claim struct {
		id     Identity
		domain string
	}
	claimed := map[string]claim{}
	for _, c := range claims {
		for _, domain := range c.names {
			key := domainKey(domain)
			if first, ok := claimed[key]; ok {
				return fmt.Errorf("%s and %s both claim the domain %q, which can lead to one local cluster only, "+
					"through the local DNS resolver and the router alike: change the ingress.domain or "+
					"resolver.extraDomains of one of them",
					first.id.Description(), c.id.Description(), first.domain)
			}
			claimed[key] = claim{id: c.id, domain: domain}
		}
	}
	return nil
}

// resolverCluster is what the local DNS resolver serves for one local
// cluster: the names that point at the router address.
type resolverCluster struct {
	id      Identity
	names   []servedName
	address string

	// config names the cluster's config: its path relative to the data
	// home when it is the cluster's config there, and otherwise the path
	// it was given by.
	config string
}

// newResolverCluster returns what the resolver serves for the local
// cluster of the config given, of the data home dataHome, whose router
// address is routerAddress.
//
// The config is an EducatesLocalConfig, or an EducatesConfig with a
// target block. Only an EducatesLocalConfig that declares no ingress
// domain gets a nip.io one: an EducatesConfig gets no CLI defaults, so
// its domain is the one its custom resources declare, if any.
func newResolverCluster(given ResolverInput, dataHome, routerAddress string) (resolverCluster, error) {
	id, source := given.Identity, given.Source
	fields, err := servedClusterFields(given.LoadedConfig, "the local DNS resolver")
	if err != nil {
		return resolverCluster{}, err
	}

	domain := cmp.Or(given.ResolverDomainOverride, given.Domain())
	return resolverCluster{
		id: id,
		names: servedNames(domain, ingressDomainFrom(given, fields, domain, routerAddress),
			fields.resolver.ExtraDomains, fields.extraDomainsField),
		address: routerAddress,
		config:  configName(id, source, dataHome),
	}, nil
}

// servedClusterFields returns what the config given declares about its
// local cluster, for server, what serves the cluster's names: the local
// DNS resolver or the router. Both take the cluster's names from an
// EducatesLocalConfig, or an EducatesConfig with a target block, and any
// other config is an error naming its file.
func servedClusterFields(given LoadedConfig, server string) (clusterFields, error) {
	fields, ok := clusterFieldsOf(given.Config)
	if ok {
		return fields, nil
	}
	if _, isEducatesConfig := given.Config.(*v1alpha1.EducatesConfig); isEducatesConfig {
		return clusterFields{}, fmt.Errorf("%s: EducatesConfig has no target block, which %s takes target.resolver from",
			given.Source, server)
	}
	return clusterFields{}, fmt.Errorf("%s: unsupported kind %q for %s", given.Source, given.Config.GetKind(), server)
}

// domains returns the domains the resolver serves for the cluster, in
// order.
func (c resolverCluster) domains() []string {
	domains := make([]string, 0, len(c.names))
	for _, name := range c.names {
		domains = append(domains, name.domain)
	}
	return domains
}

// configName names the config of the local cluster id, loaded from
// source, of the data home dataHome: by its path relative to the data
// home when it is the cluster's config there, and otherwise by source,
// the path it was given by.
func configName(id Identity, source, dataHome string) string {
	if source != id.ConfigPath {
		return source
	}
	if rel, err := filepath.Rel(dataHome, source); err == nil {
		return rel
	}
	return source
}

// heading is the comment that starts the cluster's entries in the
// resolver's configuration. It names the cluster, its config and the
// address its names point at, the router address, in this form, which
// tools may parse:
//
//	# Default local cluster (config.yaml): 10.10.10.1, the router address
//	# Local cluster test2 (clusters/test2/config.yaml): 10.10.10.1, the router address
func (c resolverCluster) heading() string {
	cluster := "Default local cluster"
	if !c.id.IsDefault() {
		cluster = "Local cluster " + c.id.Name
	}
	return fmt.Sprintf("# %s (%s): %s, the router address", cluster, c.config, c.address)
}

// servedName is a name a local cluster's entries point at its address,
// with where it comes from.
type servedName struct {
	domain string

	// from says where domain comes from: the field of the cluster's
	// config that declares it, the nip.io name of an address, or the
	// option that overrides it.
	from string
}

// servedNames returns the names a local cluster's entries point at its
// address: its ingress domain, when it has one, coming from domainFrom,
// then its extra domains, coming from the field extraDomainsField, each
// once, leaving out empty ones.
func servedNames(domain, domainFrom string, extraDomains []string, extraDomainsField string) []servedName {
	var names []servedName
	seen := map[string]bool{}
	add := func(d, from string) {
		key := domainKey(d)
		if key == "" || seen[key] {
			return
		}
		seen[key] = true
		names = append(names, servedName{domain: d, from: from})
	}
	add(domain, domainFrom)
	for _, d := range extraDomains {
		add(d, extraDomainsField)
	}
	return names
}

// ingressDomainFrom says where domain, the ingress domain the local DNS
// resolver serves for the cluster of the config given, comes from, with
// routerAddress the router address.
func ingressDomainFrom(given ResolverInput, fields clusterFields, domain, routerAddress string) string {
	if given.ResolverDomainOverride != "" {
		return "--domain"
	}
	if !fields.derivesDomain {
		return fields.domainField
	}
	// LoadConfigFile writes the nip.io name it derives from the router
	// address into an EducatesLocalConfig's ingress.domain: the default
	// local cluster's, and a named one's under it.
	if nip, ok := derivedDomain(given.Identity, routerAddress); ok && domainKey(domain) == nip {
		return "nip.io name of the router address"
	}
	return fields.domainField
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

// ResolverIncludeDir is where the local DNS resolver container mounts the
// include folder of the data home, config.DnsmasqIncludeDir, read-only.
// The rendered configuration reads the *.conf files there, the user's own
// entries. The resolver's image has an empty folder at this path, so a
// container created without the mount still starts.
const ResolverIncludeDir = "/etc/dnsmasq.d"

// includeSection ends the resolver's configuration: it reads the user's
// own entries from the *.conf files of the include folder.
const includeSection = "\n# Your own entries: dnsmasq.d/*.conf\nconf-dir=" + ResolverIncludeDir + "/,*.conf\n"

// renderDnsmasqConfig renders the resolver's configuration serving
// clusters, then the user's own entries.
func renderDnsmasqConfig(clusters []resolverCluster) []byte {
	var b strings.Builder
	b.WriteString(dnsmasqHeader)
	for _, c := range clusters {
		fmt.Fprintf(&b, "\n%s\n", c.heading())
		for _, name := range c.names {
			fmt.Fprintf(&b, "address=/%s/%s\n", name.domain, c.address)
		}
	}
	b.WriteString(includeSection)
	return []byte(b.String())
}
