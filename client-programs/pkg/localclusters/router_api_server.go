package localclusters

import (
	"fmt"
	"net/netip"
)

// CheckCreateAPIServerPort checks, before the local cluster of the config
// created is created, that kind does not publish its API server on port
// RouterAPIServerPort of the router address, which the router publishes
// to serve every local cluster's API server. A config whose apiServer
// block sets that port, on the router address or on every host address,
// is an error naming the fields. kind publishes the API server on
// 127.0.0.1 when the config names no address, which leaves the router
// address free.
//
// A router file that is missing or does not load is no error here: the
// router's own checks say what is wrong with it.
func (inv Inventory) CheckCreateAPIServerPort(created LoadedConfig) error {
	fields, ok := clusterFieldsOf(created.Config)
	if !ok || fields.apiServer.Port != RouterAPIServerPort || fields.apiServer.Address == "" {
		return nil
	}
	address, err := configuredRouterAddress(inv.dataHome)
	if err != nil {
		return nil
	}
	published, err := netip.ParseAddr(fields.apiServer.Address)
	if err != nil {
		return nil
	}
	published = published.Unmap()
	if !published.IsUnspecified() && published.String() != address {
		return nil
	}
	return fmt.Errorf("%s publishes its API server on port %d of %s, as %s.port and %s.address ask, which takes "+
		"port %d of the router address %s: the router publishes that port to serve every local cluster's API "+
		"server. Set %s.port to another port, or leave it out for kind to pick a free one",
		created.Identity.Description(), RouterAPIServerPort, describeAddress(published), fields.apiServerField,
		fields.apiServerField, RouterAPIServerPort, address, fields.apiServerField)
}
