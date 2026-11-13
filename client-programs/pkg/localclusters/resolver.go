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
// pointing at its own cluster's address, or the error that two clusters
// claim the same domain.
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
// its resolver.extraDomains point at its address, as resolverAddress
// gives it from the cluster's listen address and the observed host IP
// address. The same rule holds for the default and the named clusters.
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
		rc, err := newResolverCluster(in, inv.observed.HostIP, inv.dataHome)
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
		for _, name := range c.names {
			key := domainKey(name.domain)
			if first, ok := claims[key]; ok {
				return fmt.Errorf("%s and %s both claim the domain %q, which the local DNS resolver can point at one "+
					"local cluster only: change the ingress.domain or resolver.extraDomains of one of them",
					first.id.Description(), c.id.Description(), first.domain)
			}
			claims[key] = claim{id: c.id, domain: name.domain}
		}
	}
	return nil
}

// resolverCluster is what the local DNS resolver serves for one local
// cluster: the names that point at the cluster's address.
type resolverCluster struct {
	id      Identity
	names   []servedName
	address string

	// addressSource is where address comes from.
	addressSource AddressSource

	// listenAddress is the listen address the cluster's config declares,
	// from which address comes, empty when it declares none.
	listenAddress string

	// config names the cluster's config: its path relative to the data
	// home when it is the cluster's config there, and otherwise the path
	// it was given by.
	config string
}

// newResolverCluster returns what the resolver serves for the local
// cluster of the config given, on the host with the IP address hostIP,
// of the data home dataHome.
//
// The config is an EducatesLocalConfig, or an EducatesConfig with a
// target block. Only an EducatesLocalConfig that declares no ingress
// domain gets a nip.io one: an EducatesConfig gets no CLI defaults, so
// its domain is the one its custom resources declare, if any.
func newResolverCluster(given ResolverInput, hostIP, dataHome string) (resolverCluster, error) {
	id, source := given.Identity, given.Source
	fields, ok := clusterFieldsOf(given.Config)
	if !ok {
		if _, isEducatesConfig := given.Config.(*v1alpha1.EducatesConfig); isEducatesConfig {
			return resolverCluster{}, fmt.Errorf("%s: EducatesConfig has no target block, which the local DNS resolver takes target.resolver from", source)
		}
		return resolverCluster{}, fmt.Errorf("%s: unsupported kind %q for the local DNS resolver", source, given.Config.GetKind())
	}

	address, addressSource := resolverAddress(fields.listenAddress, hostIP)
	domain := cmp.Or(given.ResolverDomainOverride, given.Domain(hostIP))
	return resolverCluster{
		id: id,
		names: servedNames(domain, ingressDomainFrom(given, fields, domain),
			fields.resolver.ExtraDomains, fields.extraDomainsField),
		address:       address,
		addressSource: addressSource,
		listenAddress: fields.listenAddress,
		config:        configName(id, source, dataHome),
	}, nil
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
// resolver's configuration. It names the cluster, its config, its address
// and where that comes from, in one of these forms, which tools may
// parse:
//
//	# Default local cluster (config.yaml): 10.10.10.1, its listen address
//	# Local cluster test2 (clusters/test2/config.yaml): 192.168.1.10, the host's IP
func (c resolverCluster) heading() string {
	cluster := "Default local cluster"
	if !c.id.IsDefault() {
		cluster = "Local cluster " + c.id.Name
	}
	return fmt.Sprintf("# %s (%s): %s, %s", cluster, c.config, c.address, headingAddressSources[c.addressSource])
}

// headingAddressSources say in a heading where a cluster's address comes
// from.
var headingAddressSources = map[AddressSource]string{
	AddressFromListenAddress: "its listen address",
	AddressFromHostIP:        "the host's IP",
	AddressFromLoopback:      "the loopback address as the host's IP is unknown",
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
// resolver serves for the cluster of the config given, comes from.
func ingressDomainFrom(given ResolverInput, fields clusterFields, domain string) string {
	switch {
	case given.ResolverDomainOverride != "":
		return "--domain"
	case fields.domain == "" && given.Identity.IsDefault():
		return "nip.io name of the host IP"
	case fields.domain == "":
		return "nip.io name of the listen address"
	}
	// LoadNamedConfig writes the nip.io name it derives from a named
	// cluster's listen address into its ingress.domain.
	if nip, ok := nipDomain(fields.listenAddress); ok && fields.derivesDomain && !given.Identity.IsDefault() && domainKey(domain) == nip {
		return "nip.io name of the listen address"
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
