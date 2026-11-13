package localclusters

import (
	"errors"
	"io/fs"
	"net/netip"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/educates/educates-training-platform/client-programs/pkg/config"
	"github.com/educates/educates-training-platform/client-programs/pkg/config/v1alpha1"
)

// Observed is the state of the host the inventory of local clusters
// answers from, as plain values.
type Observed struct {
	// Nodes is every kind node container on the Docker host, running or
	// stopped, with the configured port bindings of the control-plane
	// nodes.
	Nodes []NodeContainer

	// InterfaceAddresses is every address of the host's network
	// interfaces.
	InterfaceAddresses []netip.Addr

	// Mirrors is every registry mirror container on the Docker host,
	// running or stopped, which every local cluster shares.
	Mirrors []MirrorContainer
}

// Inventory is what the CLI knows about the Educates clusters of one data
// home: the kind clusters the CLI manages among those observed.
type Inventory struct {
	dataHome string
	observed Observed
	clusters []Identity
}

// NewInventory returns the inventory of the local clusters of the data
// home dataHome in the observed state of the host.
//
// An Educates cluster is the kind cluster educates, the default local
// cluster, or a kind cluster educates-<name> whose folder clusters/<name>/
// exists in the data home. Every other kind cluster, such as one made by
// hand that happens to be named educates-<something>, is left out.
func NewInventory(observed Observed, dataHome string) Inventory {
	inv := Inventory{dataHome: dataHome, observed: observed}
	seen := map[string]bool{}
	for _, n := range observed.Nodes {
		if seen[n.KindCluster] {
			continue
		}
		seen[n.KindCluster] = true
		if id, ok := educatesCluster(n.KindCluster, dataHome); ok {
			inv.clusters = append(inv.clusters, id)
		}
	}
	slices.SortFunc(inv.clusters, func(a, b Identity) int {
		return strings.Compare(a.Name, b.Name)
	})
	return inv
}

// Clusters returns the identities of the Educates clusters, the default
// local cluster first and then the named ones by name.
func (inv Inventory) Clusters() []Identity {
	return slices.Clone(inv.clusters)
}

// SharedInfrastructureUsers returns the Educates clusters, other than
// deleted, that still use the host infrastructure every local cluster
// shares: the local registry, the DNS resolver and the registry mirrors.
// With none, deleting the local cluster deleted may remove them.
func (inv Inventory) SharedInfrastructureUsers(deleted Identity) []Identity {
	var users []Identity
	for _, id := range inv.clusters {
		if id.KindName != deleted.KindName {
			users = append(users, id)
		}
	}
	return users
}

// DescribeClusters names the local clusters ids in a message, as a list
// of their descriptions.
func DescribeClusters(ids []Identity) string {
	var s strings.Builder
	for i, id := range ids {
		switch {
		case i == 0:
		case i == len(ids)-1:
			s.WriteString(" and ")
		default:
			s.WriteString(", ")
		}
		s.WriteString(id.Description())
	}
	return s.String()
}

// ListRow is what local cluster list shows about one Educates cluster.
type ListRow struct {
	// Identity is the cluster's identity, which names its kube context.
	Identity Identity

	// State is whether the cluster's node containers run.
	State State

	// ListenAddress is the host address the cluster's control-plane node
	// is configured to publish ports 80 and 443 on, empty when it
	// publishes neither.
	ListenAddress string

	// Domain is the cluster's ingress domain as its config in the data
	// home declares it, or for a named cluster the nip.io name of its
	// listen address that the config falls back to. It is empty when the
	// config declares none, is missing, or does not load.
	Domain string

	// ConfigError is why the cluster's config in the data home does not
	// load, nil when it loads or is missing.
	ConfigError error
}

// List returns a row for each Educates cluster, in the order of
// Clusters.
func (inv Inventory) List() []ListRow {
	rows := make([]ListRow, 0, len(inv.clusters))
	for _, id := range inv.clusters {
		row := ListRow{Identity: id, State: id.State(inv.observed.Nodes)}
		if addrs := inv.ingressAddresses(id); len(addrs) > 0 {
			row.ListenAddress = addrs[0].String()
		}
		row.Domain, row.ConfigError = configuredDomain(id)
		rows = append(rows, row)
	}
	return rows
}

// ingressPorts are the host ports a local cluster publishes its ingress
// on.
var ingressPorts = []uint16{80, 443}

// ingressAddresses returns the host addresses the first control-plane
// node container of the local cluster id is configured to publish ports
// 80 and 443 on, each once. An address left unset, which means every host
// address, is the unspecified IPv4 address.
func (inv Inventory) ingressAddresses(id Identity) []netip.Addr {
	var addrs []netip.Addr
	for _, n := range inv.observed.Nodes {
		if n.KindCluster != id.KindName || n.Name != id.ControlPlaneNode() {
			continue
		}
		for _, b := range n.PortBindings {
			if !slices.Contains(ingressPorts, b.HostPort) {
				continue
			}
			addr := b.HostIP.Unmap()
			if !addr.IsValid() {
				addr = netip.IPv4Unspecified()
			}
			if !slices.Contains(addrs, addr) {
				addrs = append(addrs, addr)
			}
		}
	}
	return addrs
}

// configuredDomain returns the ingress domain the config of the local
// cluster id in the data home declares, or for a named cluster the nip.io
// name of its listen address it falls back to. A missing config has no
// domain and is no error.
func configuredDomain(id Identity) (string, error) {
	if _, err := os.Stat(id.ConfigPath); errors.Is(err, fs.ErrNotExist) {
		return "", nil
	}
	var cfg v1alpha1.Config
	var err error
	if id.IsDefault() {
		cfg, err = config.Load(id.ConfigPath)
	} else {
		cfg, err = LoadNamedConfig(id.ConfigPath)
	}
	if err != nil {
		return "", err
	}
	switch c := cfg.(type) {
	case *v1alpha1.EducatesLocalConfig:
		return c.Ingress.Domain, nil
	case *v1alpha1.EducatesConfig:
		return c.IngressDomain(), nil
	default:
		return "", nil
	}
}

// educatesCluster returns the identity of the local cluster whose kind
// cluster is kindName, and reports whether that kind cluster is an
// Educates cluster of the data home dataHome.
func educatesCluster(kindName, dataHome string) (Identity, bool) {
	if kindName == defaultKindName {
		return Default(dataHome), true
	}
	name, ok := strings.CutPrefix(kindName, defaultKindName+"-")
	if !ok {
		return Identity{}, false
	}
	id, err := Named(name, dataHome)
	if err != nil {
		return Identity{}, false
	}
	if info, err := os.Stat(filepath.Dir(id.ConfigPath)); err != nil || !info.IsDir() {
		return Identity{}, false
	}
	return id, true
}
