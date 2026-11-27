package localclusters

import "fmt"

// updateRouterCommand is the command that creates, re-creates or reloads
// the router.
const updateRouterCommand = "educates local router update"

// RouterState is whether the router container exists and runs, as local
// router status reports it.
type RouterState string

const (
	// RouterRunning is a router container that runs.
	RouterRunning RouterState = "running"

	// RouterStopped is a router container that exists and does not run.
	RouterStopped RouterState = "stopped"

	// RouterMissing is no router container at all.
	RouterMissing RouterState = "missing"
)

// RouterTLS is how the router serves a route's TLS, on port 443.
type RouterTLS string

const (
	// RouterTLSPassthrough passes TLS through to the cluster untouched,
	// chosen by the server name of the TLS handshake: the cluster presents
	// its own certificate, and the router holds no key for it.
	RouterTLSPassthrough RouterTLS = "passthrough"

	// RouterTLSTerminated ends TLS at the router, with the certificate of
	// the cluster's domain, and sends the requests on to the cluster as
	// plain HTTP: the domain of a cluster with
	// ingress.externalTLSTermination.
	RouterTLSTerminated RouterTLS = "terminated"

	// RouterTLSNone serves the name on port 80 only, and closes its TLS
	// connections: an extra domain of a cluster whose TLS ends at the
	// router, which the router's certificate for the cluster's domain does
	// not cover.
	RouterTLSNone RouterTLS = "none"
)

// RouterStatus is what local router status shows: the router address,
// the router container, and each name the router sends to a local
// cluster.
type RouterStatus struct {
	// Address is the router address the router file of the data home,
	// router.yaml, declares, in its canonical form, empty when the file is
	// missing or does not load.
	Address string

	// AddressError says why the router address can't serve local
	// clusters: the router file is missing or does not load, or its
	// address breaks a rule of CheckRouterAddress against the host's
	// interfaces. It is nil when the address can serve them.
	AddressError error

	// State is whether the router container exists and runs.
	State RouterState

	// Image is the image the router container was created from, or the
	// image the CLI pins when there is no router container.
	Image string

	// PublishedAddress is the host address the router container is
	// configured to publish ports 80 and 443 on, empty when there is no
	// router container or it publishes neither.
	PublishedAddress string

	// PublishedPorts are the router ports, of RouterPorts, the router
	// container is configured to publish on PublishedAddress, in order,
	// empty when PublishedAddress is.
	PublishedPorts []uint16

	// Advice says what to run, and why, when the router does not serve
	// the local clusters as it should, and is empty otherwise.
	Advice string

	// Routes is a row for each domain the router sends to a local cluster,
	// in the order of RouterRoutes, and each route's domains in the order
	// of RouterRoute.Domains.
	Routes []RouterStatusRow

	// LeftOut is an error for each local cluster the router leaves out,
	// as RouterRoutes returns them.
	LeftOut []error
}

// RouterStatusRow is what local router status shows about one domain the
// router sends to a local cluster.
type RouterStatusRow struct {
	// Domain is the domain, which the router sends to the cluster with
	// every name under it. It is empty for a cluster with no domain to
	// route.
	Domain string

	// Cluster is the identity of the local cluster the domain reaches.
	Cluster Identity

	// TLS is how the router serves the domain's TLS.
	TLS RouterTLS

	// ClusterState is whether the cluster's node containers run.
	ClusterState State
}

// RouterAddress returns the router address the router file of the data
// home, router.yaml, declares, in its canonical form: the one host address
// every local cluster's names resolve to, and the router publishes ports
// 80, 443 and 6443 on. A file that is missing or does not load, or an address
// the rules of CheckRouterAddress that need no host interface refuse, is
// an error naming the file and how to set the address.
func (inv Inventory) RouterAddress() (string, error) {
	return configuredRouterAddress(inv.dataHome)
}

// RouterStatus returns the status of the router, given image, the router
// image the CLI pins: the router address, the router container as
// observed, what to run when it does not serve the local clusters as it
// should, and each name it routes, from the same inputs as RouterRoutes.
// It returns the error RouterRoutes returns for two clusters claiming the
// same domain.
func (inv Inventory) RouterStatus(image string) (RouterStatus, error) {
	routes, leftOut, err := inv.RouterRoutes()
	if err != nil {
		return RouterStatus{}, err
	}
	status := RouterStatus{State: RouterMissing, Image: image, LeftOut: leftOut}

	if status.Address, status.AddressError = inv.RouterAddress(); status.AddressError == nil {
		_, status.AddressError = LoadRouterAddress(inv.dataHome, inv.observed.InterfaceAddresses)
	}

	switch router := inv.observed.Router; {
	case router == nil && len(inv.clusters) == 0:
		status.Advice = "There is no local cluster, so there is no router: 'educates local cluster create' " +
			"creates it with the first local cluster."
	case router == nil:
		status.Advice = fmt.Sprintf("The router container is missing: '%s' creates it.", updateRouterCommand)
	default:
		status.State = RouterStopped
		if router.Running {
			status.State = RouterRunning
		}
		status.Image = router.Image
		if published, ok := router.publishedAddress(); ok {
			status.PublishedAddress = published.String()
			status.PublishedPorts = router.publishedPorts(published)
		}
		// The router can be re-created only on a router address that can
		// serve local clusters: AddressError says how to fix one that
		// can't.
		if why := router.recreateReason(image, status.Address); status.AddressError == nil && why != "" {
			status.Advice = fmt.Sprintf("The router has to be re-created, since %s: '%s' re-creates it.", why, updateRouterCommand)
		}
	}

	for _, r := range routes {
		domains := r.Domains()
		if len(domains) == 0 {
			domains = []string{""}
		}
		for _, domain := range domains {
			status.Routes = append(status.Routes, RouterStatusRow{Domain: domain, Cluster: r.Cluster, TLS: r.domainTLS(domain), ClusterState: r.State})
		}
	}
	return status, nil
}

// domainTLS returns how the router serves the TLS of domain, one of the
// domains of the route r: for a route whose TLS ends at the router, it
// ends there for the route's ingress domain, and its extra domains have
// no TLS.
func (r RouterRoute) domainTLS(domain string) RouterTLS {
	switch {
	case !r.TLSEndsAtRouter:
		return RouterTLSPassthrough
	case domain == r.IngressDomain:
		return RouterTLSTerminated
	default:
		return RouterTLSNone
	}
}
