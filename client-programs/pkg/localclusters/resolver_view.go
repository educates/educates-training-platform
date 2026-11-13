package localclusters

import (
	"fmt"
	"slices"
	"strings"
)

// ResolverView is what the local DNS resolver serves, as local resolver
// view shows it: the clusters it serves, every name it answers for, and
// the configs it leaves out.
type ResolverView struct {
	// Clusters is a row for each local cluster the resolver serves, in
	// the order of its configuration.
	Clusters []ResolverClusterRow

	// Names is a row for each name the resolver answers for: every local
	// cluster's names, in the order of its configuration, then the names
	// of the user's own include files.
	Names []ResolverNameRow

	// LeftOut is an error for each config of another local cluster the
	// resolver leaves out, as Inventory.ResolverConfig returns them.
	LeftOut []error
}

// ResolverClusterRow is what local resolver view shows about one local
// cluster the resolver serves.
type ResolverClusterRow struct {
	// Cluster is the cluster's name, or (default) for the default local
	// cluster.
	Cluster string

	// Address is the address the cluster's names resolve to.
	Address string

	// AddressFrom says where Address comes from: the listen address, the
	// host's IP address, or the loopback address with the host's IP
	// address unknown, and for the last two, the listen address.
	AddressFrom string

	// Config names the cluster's config, as the heading of its entries in
	// the resolver's configuration does.
	Config string
}

// ResolverNameRow is what local resolver view shows about one name the
// resolver answers for.
type ResolverNameRow struct {
	// Name is the name, which the resolver answers for with its
	// subdomains.
	Name string

	// Cluster is the local cluster the name resolves to, as
	// ResolverClusterRow.Cluster names it, or - for a name of the user's
	// own include files.
	Cluster string

	// From says where the name comes from: a field of the cluster's
	// config, the nip.io name of an address, or an include file.
	From string

	// Note says what else there is to know about the name, empty when
	// there is nothing.
	Note string
}

// IncludeFile is a file of the include folder, the user's own dnsmasq
// configuration, as local resolver view reads it.
type IncludeFile struct {
	// Name is the file's name in the include folder.
	Name string

	// Content is the file's content.
	Content []byte
}

// ResolverView returns what the local DNS resolver serves, from the same
// inputs Inventory.ResolverConfig renders its configuration from with no
// config given, and from the files of the include folder, includes. It
// returns the error ResolverConfig returns for two clusters claiming the
// same domain.
//
// resolverFileExists reports whether /etc/resolver holds a file for a
// domain, which on macOS forwards the domain's names to the resolver. It
// is nil where there is no such folder to check.
func (inv Inventory) ResolverView(includes []IncludeFile, resolverFileExists func(domain string) bool) (ResolverView, error) {
	clusters, leftOut, err := inv.resolverClusters(nil)
	if err != nil {
		return ResolverView{}, err
	}
	view := ResolverView{LeftOut: leftOut}
	var names []viewName
	for _, c := range clusters {
		view.Clusters = append(view.Clusters, ResolverClusterRow{
			Cluster:     clusterLabel(c.id),
			Address:     c.address,
			AddressFrom: addressFrom(c.addressSource, c.listenAddress),
			Config:      c.config,
		})
		owner, ownerDescription := c.id.Name, "cluster "+c.id.Name
		if c.id.IsDefault() {
			owner, ownerDescription = "default", "the default cluster"
		}
		for _, name := range c.names {
			names = append(names, viewName{
				domain: name.domain, cluster: clusterLabel(c.id), from: name.from,
				owner: owner, ownerDescription: ownerDescription,
			})
		}
	}
	for _, file := range includedFiles(includes) {
		from := includeDirName + "/" + file.Name
		for _, entry := range addressEntries(file.Content) {
			for _, domain := range entry.domains {
				names = append(names, viewName{
					domain: domain, cluster: "-", from: from,
					owner: from, ownerDescription: from, included: true,
					notes: []string{"yours, " + describeAnswer(entry.address)},
				})
			}
		}
	}
	noteOverlaps(names)
	if resolverFileExists != nil {
		for i := range names {
			if !forwardedToResolver(names[i].domain, resolverFileExists) {
				names[i].notes = append(names[i].notes, "no /etc/resolver file forwards this name")
			}
		}
	}
	for _, n := range names {
		view.Names = append(view.Names, ResolverNameRow{
			Name: n.domain, Cluster: n.cluster, From: n.from, Note: strings.Join(n.notes, "; "),
		})
	}
	return view, nil
}

// viewName is a name the local DNS resolver answers for, as
// ResolverView works on it.
type viewName struct {
	domain  string
	cluster string
	from    string
	notes   []string

	// owner names what the name comes from in a note on another name:
	// default, a named cluster's name, or the include file.
	owner string

	// ownerDescription names what the name comes from in a sentence.
	ownerDescription string

	// included reports whether the name comes from an include file.
	included bool
}

// noteOverlaps adds to each of names a note on how it overlaps the
// others. The resolver answers a name from the most specific name it
// serves, so for a name inside a broader one, the closest broader name
// matters: one that comes from the same cluster or file covers it
// already, and one that comes from elsewhere loses to it. A name an
// include file serves that a cluster or another include file also
// serves is a warning on both, since dnsmasq then answers with every
// address the name is given.
func noteOverlaps(names []viewName) {
	for i := range names {
		n := &names[i]
		var closest *viewName
		for j := range names {
			o := &names[j]
			if isSubdomain(n.domain, o.domain) && (closest == nil || isSubdomain(o.domain, closest.domain)) {
				closest = o
			}
		}
		switch {
		case closest == nil:
		case closest.owner == n.owner:
			n.notes = append(n.notes, "covered by "+closest.domain)
		default:
			n.notes = append(n.notes, fmt.Sprintf("wins over %s (%s)", closest.domain, closest.owner))
		}
		for j := range names {
			o := &names[j]
			if i != j && o.owner != n.owner && (n.included || o.included) && domainKey(o.domain) == domainKey(n.domain) {
				n.notes = append(n.notes, fmt.Sprintf("warning: %s also serves this name", o.ownerDescription))
			}
		}
	}
}

// forwardedToResolver reports whether macOS forwards the names under
// domain to the local DNS resolver: whether resolverFileExists reports an
// /etc/resolver file for domain or for a domain it is inside. A nip.io
// name needs none, since public DNS resolves it to the address it names.
func forwardedToResolver(domain string, resolverFileExists func(domain string) bool) bool {
	key := domainKey(domain)
	if key == nipSuffix || strings.HasSuffix(key, "."+nipSuffix) {
		return true
	}
	for {
		if resolverFileExists(key) {
			return true
		}
		_, parent, found := strings.Cut(key, ".")
		if !found || parent == "" {
			return false
		}
		key = parent
	}
}

// nipSuffix is the domain under which nip.io names an address.
const nipSuffix = "nip.io"

// isSubdomain reports whether domain is a subdomain of parent, inside it
// and not parent itself.
func isSubdomain(domain, parent string) bool {
	return strings.HasSuffix(domainKey(domain), "."+domainKey(parent))
}

// includeDirName is the name of the include folder in the data home.
const includeDirName = "dnsmasq.d"

// includedFiles returns the files of files that the resolver's
// configuration reads, in the order it reads them: those whose names end
// in .conf, by name, leaving out those whose names start with a dot,
// which dnsmasq skips.
func includedFiles(files []IncludeFile) []IncludeFile {
	var included []IncludeFile
	for _, f := range files {
		if strings.HasSuffix(f.Name, ".conf") && !strings.HasPrefix(f.Name, ".") {
			included = append(included, f)
		}
	}
	slices.SortFunc(included, func(a, b IncludeFile) int { return strings.Compare(a.Name, b.Name) })
	return included
}

// describeAnswer says what dnsmasq answers for a name an address= line
// gives the address address: no address answers that there is no such
// domain, and # the null addresses.
func describeAnswer(address string) string {
	switch address {
	case "":
		return "answered as no such domain"
	case "#":
		return "resolves to 0.0.0.0 and ::"
	default:
		return "resolves to " + address
	}
}

// addressEntry is an address= line of dnsmasq configuration: the domains
// it resolves, each with its subdomains, to address.
type addressEntry struct {
	domains []string
	address string
}

// addressEntries returns the address= lines of the dnsmasq configuration
// content, address=/<domain>[/<domain>...]/<address>, in order, leaving
// out every other line.
func addressEntries(content []byte) []addressEntry {
	var entries []addressEntry
	for _, line := range strings.Split(string(content), "\n") {
		key, value, ok := strings.Cut(strings.TrimSpace(line), "=")
		if !ok || strings.TrimSpace(key) != "address" {
			continue
		}
		parts := strings.Split(strings.TrimSpace(value), "/")
		if len(parts) < 3 || parts[0] != "" {
			continue
		}
		var domains []string
		for _, d := range parts[1 : len(parts)-1] {
			if d != "" {
				domains = append(domains, d)
			}
		}
		if len(domains) > 0 {
			entries = append(entries, addressEntry{domains: domains, address: parts[len(parts)-1]})
		}
	}
	return entries
}

// clusterLabel names the local cluster id in local resolver view, as
// local cluster list does.
func clusterLabel(id Identity) string {
	if id.IsDefault() {
		return "(default)"
	}
	return id.Name
}

// addressFrom says where a local cluster's address comes from, source,
// for the cluster with the listen address listenAddress.
func addressFrom(source AddressSource, listenAddress string) string {
	listen := "no listen address"
	if listenAddress != "" {
		listen = "listen address " + listenAddress
	}
	switch source {
	case AddressFromListenAddress:
		return "listen address"
	case AddressFromHostIP:
		return fmt.Sprintf("host IP (%s)", listen)
	default:
		return fmt.Sprintf("loopback, host IP unknown (%s)", listen)
	}
}
