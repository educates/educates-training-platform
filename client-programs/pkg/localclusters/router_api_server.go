package localclusters

import (
	"fmt"
	"net"
	"net/netip"
	"strconv"
)

// RouterAPIServer is how a client, on the host or in a pod of any local
// cluster, reaches the API server of a local cluster through the router.
type RouterAPIServer struct {
	// URL is the API server's address: https, on port RouterAPIServerPort
	// of the router address.
	URL string

	// ServerName is the TLS server name the client gives: the name of the
	// cluster's control-plane node container. The router sends the
	// connection to the cluster whose node it names, and the API server's
	// certificate names the node, so the client verifies the certificate
	// against it rather than against the router address.
	ServerName string
}

// RouterAPIServerOf returns how a client reaches the API server of the
// local cluster id through the router, as RouterAPIServer describes it.
//
// The router serves the API server of every local cluster whose names it
// serves: one whose config in the data home loads, is of a kind the router
// serves, and declares or derives an ingress domain or an extra domain.
// For any other, and when the router file of the data home is missing or
// does not load, the error says why. The router address is the one the
// router file declares, which the router publishes its ports on once a
// local cluster command or 'educates local router update' has created it
// there.
func RouterAPIServerOf(id Identity) (RouterAPIServer, error) {
	address, err := configuredRouterAddress(id.DataHome)
	if err != nil {
		return RouterAPIServer{}, err
	}
	loaded, err := LoadConfig(id)
	if err != nil {
		return RouterAPIServer{}, leftOutBy(routerServer, id, err)
	}
	route, err := routeOf(loaded)
	if err != nil {
		return RouterAPIServer{}, leftOutBy(routerServer, id, err)
	}
	if len(route.Domains()) == 0 {
		return RouterAPIServer{}, fmt.Errorf("the router serves nothing of %s, whose config declares neither an "+
			"ingress domain nor an extra domain", id.Description())
	}
	return RouterAPIServer{
		URL:        "https://" + net.JoinHostPort(address, strconv.Itoa(RouterAPIServerPort)),
		ServerName: id.ControlPlaneNode(),
	}, nil
}

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
