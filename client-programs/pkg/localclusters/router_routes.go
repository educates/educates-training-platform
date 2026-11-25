package localclusters

import (
	"cmp"
	"maps"
	"slices"
)

// routerServer names the router in the errors of the configs it leaves
// out.
const routerServer = "the router"

// RouterRoutes returns the route of each local cluster the router serves,
// the default local cluster first and then the named ones by name, or the
// error that two of them claim the same domain.
//
// The router serves every Educates cluster that exists, running or
// stopped, and every cluster given holds a config for. A config given
// replaces the one in the data home: it is the config of a cluster being
// created, routed before its nodes exist. Unlike the local DNS resolver,
// the router serves no cluster that does not exist, so a config in the
// data home routes nothing until its cluster is created.
//
// A cluster's names are its ingress domain, declared or derived as
// LoadedConfig.Domain gives it, then its resolver.extraDomains, each once,
// which the local DNS resolver points at the router address.
//
// A config given that the router can't serve is an error. A cluster whose
// config in the data home is missing, does not load or can't be served is
// another cluster's problem, so it is left out rather than failing every
// other cluster: RouterRoutes also returns an error for each one it leaves
// out, naming the cluster and why, for the caller to warn about.
func (inv Inventory) RouterRoutes(given ...LoadedConfig) ([]RouterRoute, []error, error) {
	byName := map[string]LoadedConfig{}
	for _, c := range given {
		byName[c.Identity.Name] = c
	}

	var routes []RouterRoute
	var leftOut []error
	for _, id := range inv.routedClusters(given) {
		loaded, isGiven := byName[id.Name]
		if !isGiven {
			var err error
			if loaded, err = LoadConfig(id); err != nil {
				leftOut = append(leftOut, leftOutBy(routerServer, id, err))
				continue
			}
		}
		fields, err := servedClusterFields(loaded, routerServer)
		if err != nil {
			if isGiven {
				return nil, nil, err
			}
			leftOut = append(leftOut, leftOutBy(routerServer, id, err))
			continue
		}
		var names []string
		for _, name := range servedNames(loaded.Domain(), "", fields.resolver.ExtraDomains, "") {
			names = append(names, name.domain)
		}
		routes = append(routes, RouterRoute{
			Cluster: id,
			Names:   names,
			Node:    id.ControlPlaneNode(),
			State:   id.State(inv.observed.Nodes),
		})
	}

	claims := make([]claimedNames, 0, len(routes))
	for _, r := range routes {
		claims = append(claims, claimedNames{id: r.Cluster, names: r.Names})
	}
	if err := checkDomainsClaimedOnce(claims); err != nil {
		return nil, nil, err
	}
	return routes, leftOut, nil
}

// routedClusters returns the local clusters the router serves, as
// RouterRoutes says: the Educates clusters and the clusters of given, the
// default local cluster first, then the named ones by name.
func (inv Inventory) routedClusters(given []LoadedConfig) []Identity {
	byName := map[string]Identity{}
	for _, id := range inv.clusters {
		byName[id.Name] = id
	}
	for _, c := range given {
		byName[c.Identity.Name] = c.Identity
	}
	ids := slices.Collect(maps.Values(byName))
	slices.SortFunc(ids, func(a, b Identity) int { return cmp.Compare(a.Name, b.Name) })
	return ids
}
