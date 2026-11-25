package localclusters

import (
	"fmt"
)

// CheckCreateDomains checks, before the local cluster of the config
// created is created, that no other Educates cluster, running or stopped,
// claims one of its domains, as checkDomainsUnclaimed says.
func (inv Inventory) CheckCreateDomains(created LoadedConfig) error {
	return inv.checkDomainsUnclaimed(created)
}

// CheckStartDomains checks, before the local cluster id is started, that
// no other Educates cluster, running or stopped, claims one of the domains
// of its config in the data home, as checkDomainsUnclaimed says.
//
// A cluster whose config in the data home is missing, as for a cluster
// created with --config, or does not load, claims no name the router or
// the local DNS resolver serves: both leave it out, and say why. So it has
// no domain to check, and starts.
func (inv Inventory) CheckStartDomains(id Identity) error {
	loaded, err := LoadConfig(id)
	if err != nil {
		return nil
	}
	return inv.checkDomainsUnclaimed(loaded)
}

// checkDomainsUnclaimed checks that no Educates cluster other than the
// local cluster of the config c, running or stopped, claims one of the
// domains c claims, as claimedDomains gives them. A name leads to one
// local cluster only, through the local DNS resolver and the router
// alike, so a domain another cluster claims, in any case, is an error
// naming that cluster, its state and the domain. A subdomain of another
// cluster's domain is no clash, since both take the most specific domain
// that matches a name.
//
// Another cluster whose config in the data home is missing or does not
// load claims nothing the router or the resolver serves, so it is left
// out of the check.
func (inv Inventory) checkDomainsUnclaimed(c LoadedConfig) error {
	ours := map[string]bool{}
	for _, domain := range claimedDomains(c) {
		ours[domainKey(domain)] = true
	}
	for _, other := range inv.clusters {
		if other.Name == c.Identity.Name {
			continue
		}
		loaded, err := LoadConfig(other)
		if err != nil {
			continue
		}
		for _, domain := range claimedDomains(loaded) {
			if ours[domainKey(domain)] {
				return fmt.Errorf("%s claims the domain %q, which %s, %s, claims already: a name leads to one "+
					"local cluster only, through the local DNS resolver and the router alike. Change the "+
					"ingress.domain or resolver.extraDomains of one of them",
					c.Identity.Description(), domainKey(domain), other.Description(),
					other.State(inv.observed.Nodes))
			}
		}
	}
	return nil
}

// claimedDomains returns the domains the local cluster of the config c
// claims: its ingress domain, declared or derived as LoadedConfig.Domain
// gives it, then its resolver.extraDomains, each once, which the local DNS
// resolver points at the router address and the router sends to the
// cluster. A config of a kind that declares no local cluster claims its
// ingress domain alone.
func claimedDomains(c LoadedConfig) []string {
	fields, _ := clusterFieldsOf(c.Config)
	var domains []string
	for _, name := range servedNames(c.Domain(), "", fields.resolver.ExtraDomains, "") {
		domains = append(domains, name.domain)
	}
	return domains
}
