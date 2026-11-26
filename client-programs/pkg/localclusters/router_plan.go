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

	// Image is the image reference the container was created from, as
	// Docker reports it: as it was given, with its name normalised, such
	// as docker.io/library/haproxy, reduced to its digest, or as an image
	// ID.
	Image string

	// ImageID is the ID of the image the container runs.
	ImageID string

	// PinnedImageID is the ID the Docker host gives the router image the
	// CLI pins, empty when the host does not have that image.
	PinnedImageID string

	// PortBindings are the host ports the container is configured to
	// publish, whether it runs or not.
	PortBindings []PortBinding
}

// RouterUser returns the user, as uid:gid, the router container runs
// HAProxy as on a Docker engine whose security options, as Docker's system
// information reports them, are securityOptions, given uid and gid, the
// IDs of the CLI's own user and group. HAProxy must read the keys of the
// router folder, which only the CLI's user may read:
//
//   - on a rootless engine, whose security options include name=rootless,
//     only root in a container maps to the host user who runs the engine
//     and owns the keys, so it is 0:0;
//   - on any other engine it is the CLI's own uid and gid, since a Linux
//     engine's bind mounts keep the host's owners, and Docker Desktop
//     serves the folder as the CLI's user;
//   - where the host has no such IDs, as on Windows, it is empty, and the
//     image's own user, haproxy, runs it.
func RouterUser(securityOptions []string, uid, gid int) string {
	for _, option := range securityOptions {
		if slices.Contains(strings.Split(option, ","), "name=rootless") {
			return "0:0"
		}
	}
	if uid < 0 || gid < 0 {
		return ""
	}
	return fmt.Sprintf("%d:%d", uid, gid)
}

// Router returns the router container observed, and reports whether there
// is one.
func (inv Inventory) Router() (RouterContainer, bool) {
	if inv.observed.Router == nil {
		return RouterContainer{}, false
	}
	return *inv.observed.Router, true
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
	case !r.runsImage(image):
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

// runsImage reports whether the router container runs image, the router
// image the CLI pins, however Docker reports the image it was created
// from. When the Docker host gives an ID to both the container's image and
// the pinned one, the two IDs tell. Otherwise the container runs the
// pinned image when the digest its image reference names, or its image's
// ID, is the digest the pinned reference names: the containerd image
// store gives an image the digest of its image index as its ID.
func (r RouterContainer) runsImage(image string) bool {
	if r.ImageID != "" && r.PinnedImageID != "" {
		return r.ImageID == r.PinnedImageID
	}
	pinned := imageDigest(image)
	if pinned == "" {
		return r.Image == image
	}
	return imageDigest(r.Image) == pinned || r.ImageID == pinned
}

// imageDigest returns the digest the image reference ref names: the one
// after its @, the reference itself when it is a digest alone, as an
// image ID is, and empty when it names none, as a reference by its tag
// alone does.
func imageDigest(ref string) string {
	if i := strings.LastIndex(ref, "@"); i >= 0 {
		return ref[i+1:]
	}
	if strings.HasPrefix(ref, "sha256:") {
		return ref
	}
	return ""
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

// describeAddress names the host address addr in a message, saying when
// it means every host address.
func describeAddress(addr netip.Addr) string {
	if addr.IsUnspecified() {
		return addr.String() + ", every host address"
	}
	return addr.String()
}

// capitalize returns s with its first letter in upper case, to start a
// sentence.
func capitalize(s string) string {
	if s == "" || s[0] < 'a' || s[0] > 'z' {
		return s
	}
	return string(s[0]-'a'+'A') + s[1:]
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
