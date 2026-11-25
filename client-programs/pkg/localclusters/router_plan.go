package localclusters

import (
	"fmt"
	"net/netip"
	"slices"
	"strings"
)

// RouterContainer is the router container as Docker reports it, running
// or not.
type RouterContainer struct {
	// Running reports whether the container is running.
	Running bool

	// Image is the image reference the container was created from.
	Image string

	// PortBindings are the host ports the container is configured to
	// publish, whether it runs or not.
	PortBindings []PortBinding
}

// publishedAddress returns the host address the router container is
// configured to publish port 80 or 443 on, the unspecified IPv4 address
// for every host address, and false when it publishes neither.
func (r RouterContainer) publishedAddress() (netip.Addr, bool) {
	for _, b := range r.PortBindings {
		if !slices.Contains(ingressPorts, b.HostPort) {
			continue
		}
		addr := b.HostIP.Unmap()
		if !addr.IsValid() {
			addr = netip.IPv4Unspecified()
		}
		return addr, true
	}
	return netip.Addr{}, false
}

// recreateReason says why the router container has to be re-created to
// run image and publish ports 80 and 443 on the router address address,
// and is empty when it needs no re-creating.
func (r RouterContainer) recreateReason(image, address string) string {
	published, publishes := r.publishedAddress()
	switch {
	case r.Image != image:
		return fmt.Sprintf("it runs the image %s, not %s, the one this CLI pins", r.Image, image)
	case !publishes:
		return fmt.Sprintf("it publishes neither port 80 nor port 443, which it publishes on the router address %s", address)
	case published.String() != address:
		return fmt.Sprintf("it publishes ports 80 and 443 on %s, not on the router address %s",
			describeAddress(published), address)
	case !r.Running:
		return "it is not running"
	default:
		return ""
	}
}

// RouterAction is what local cluster create and start do to the router
// container before they reach the cluster's nodes.
type RouterAction string

const (
	// RouterCreate creates the missing router container.
	RouterCreate RouterAction = "create"

	// RouterRecreate removes the router container and creates it again,
	// which drops every open connection through it.
	RouterRecreate RouterAction = "re-create"

	// RouterReload keeps the router container, and reloads it with the
	// configuration rendered again, which keeps open connections.
	RouterReload RouterAction = "reload"
)

// RouterPlan is what to do to the router container, and where it
// publishes ports 80 and 443.
type RouterPlan struct {
	// Action is what to do to the router container.
	Action RouterAction

	// Address is the router address, the host address the router
	// publishes ports 80 and 443 on: in its canonical form, and checked
	// against the router address rules when the router is created or
	// re-created.
	Address string

	// Why says why the router is re-created, empty otherwise.
	Why string
}

// PlanRouter returns what local cluster create and start do to the router
// container, given image, the router image the CLI pins:
//
//   - a missing router is created;
//   - a router created from another image, configured to publish ports
//     80 and 443 on another address than the router address, or not
//     running, is re-created, which drops every open connection through
//     it;
//   - any other router is reloaded with the configuration rendered again,
//     which keeps open connections, such as terminals.
//
// The router address is the one the router file of the data home,
// router.yaml, declares. Where the router is created or re-created it must
// pass CheckRouterAddress against the host's interfaces, and otherwise is
// an error naming the file, why, and how to set another, as
// LoadRouterAddress returns it.
func (inv Inventory) PlanRouter(image string) (RouterPlan, error) {
	address, err := configuredRouterAddress(inv.dataHome)
	if err != nil {
		return RouterPlan{}, err
	}
	plan := RouterPlan{Action: RouterReload, Address: address}
	switch router := inv.observed.Router; {
	case router == nil:
		plan.Action = RouterCreate
	default:
		if plan.Why = router.recreateReason(image, address); plan.Why == "" {
			return plan, nil
		}
		plan.Action = RouterRecreate
	}
	if plan.Address, err = LoadRouterAddress(inv.dataHome, inv.observed.InterfaceAddresses); err != nil {
		return RouterPlan{}, err
	}
	return plan, nil
}

// ClustersPublishingIngressPorts returns the Educates clusters, running or
// stopped, whose first control-plane node is configured to publish port 80
// or 443 on the host: clusters created by an earlier release of the CLI,
// before the router served every local cluster, which the router can't
// publish its ports beside. kind fixes the ports a node publishes when it
// creates the cluster, so such a cluster is deleted and created again.
func (inv Inventory) ClustersPublishingIngressPorts() []Identity {
	var ids []Identity
	for _, id := range inv.clusters {
		if len(inv.ingressAddresses(id)) > 0 {
			ids = append(ids, id)
		}
	}
	return ids
}

// RouterStartError returns the error of the router container failing,
// with err, Docker's error, to start while publishing ports 80 and 443 on
// the router address address. Its start is the check that the ports are
// free.
//
// When Educates clusters made by an earlier release of the CLI are
// configured to publish port 80 or 443 from their node, as
// ClustersPublishingIngressPorts says, the error names each, whether it
// runs, and the commands that delete it and create it again. Otherwise it
// says that another process holds the ports. Either way it wraps err.
func (inv Inventory) RouterStartError(address string, err error) error {
	failed := fmt.Errorf("the router can't publish ports 80 and 443 on %s: %w", address, err)
	publishing := inv.ClustersPublishingIngressPorts()
	if len(publishing) == 0 {
		return fmt.Errorf("%w. Another process, such as an ingress controller, a web server or a Docker port forward, "+
			"holds them: free them, then run the command again", failed)
	}

	described := make([]string, 0, len(publishing))
	var commands strings.Builder
	for _, id := range publishing {
		described = append(described, fmt.Sprintf("%s, which is %s", id.Description(), id.State(inv.observed.Nodes)))
		fmt.Fprintf(&commands, "\n  educates local cluster delete%s\n  educates local cluster create%s",
			selectionFlagArg(id), selectionFlagArg(id))
	}
	which := "this local cluster, created by an earlier release of the CLI, publishes them from its node"
	if len(publishing) > 1 {
		which = "these local clusters, created by an earlier release of the CLI, publish them from their nodes"
	}
	return fmt.Errorf("%w. Every local cluster is now served through the router, but %s: %s. "+
		"Delete each and create it again:\n%s\n", failed, which, joinDescriptions(described), commands.String())
}

// joinDescriptions joins descriptions in a sentence, as "a, b and c".
func joinDescriptions(descriptions []string) string {
	switch n := len(descriptions); n {
	case 0:
		return ""
	case 1:
		return descriptions[0]
	default:
		return strings.Join(descriptions[:n-1], ", ") + " and " + descriptions[n-1]
	}
}
