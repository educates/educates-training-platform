package localclusters

import (
	"fmt"
	"net/netip"
	"os"
	"slices"

	"github.com/educates/educates-training-platform/client-programs/pkg/config"
)

// CheckCreate returns an error when creating the local cluster id with
// its ports 80 and 443 published on the host address listenAddress would
// clash, naming what it clashes with:
//
//   - another Educates cluster, running or stopped, is configured to
//     publish them on the same address;
//   - either that cluster or id publishes them on every host address
//     (0.0.0.0 or ::), which clashes with any other cluster;
//   - listenAddress is the address of no host network interface.
//
// An empty listenAddress means every host address, as it does to kind.
// Kind clusters that are not Educates clusters are left out: whatever
// else holds the ports is for a check that binds them.
func (inv Inventory) CheckCreate(id Identity, listenAddress string) error {
	addr := netip.IPv4Unspecified()
	if listenAddress != "" {
		var err error
		if addr, err = netip.ParseAddr(listenAddress); err != nil {
			return fmt.Errorf("listen address %q of %s is not an IP address", listenAddress, id.Description())
		}
	}
	return inv.checkListenAddress(id, addr.Unmap(),
		"set cluster.listenAddress to a specific IP of this host")
}

// CheckStart returns an error when starting the local cluster id would
// clash, in the cases CheckCreate describes, at the host addresses its
// control-plane node is configured to publish ports 80 and 443 on. A
// cluster with no such node has nothing to check.
func (inv Inventory) CheckStart(id Identity) error {
	for _, addr := range inv.ingressAddresses(id) {
		if err := inv.checkListenAddress(id, addr, recreateAdvice(id)); err != nil {
			return err
		}
	}
	return nil
}

// checkListenAddress returns an error when the local cluster id
// publishing its ports 80 and 443 on the host address addr clashes with
// another Educates cluster, or addr is the address of no host network
// interface. everyAddressAdvice is what id has to do when addr is every
// host address and so clashes with every other cluster.
func (inv Inventory) checkListenAddress(id Identity, addr netip.Addr, everyAddressAdvice string) error {
	for _, other := range inv.clusters {
		if other.KindName == id.KindName {
			continue
		}
		for _, otherAddr := range inv.ingressAddresses(other) {
			var advice string
			switch {
			case otherAddr.IsUnspecified():
				advice = recreateAdvice(other)
			case addr.IsUnspecified():
				advice = everyAddressAdvice
			case otherAddr == addr:
				advice = "give each local cluster a host IP of its own"
			default:
				continue
			}
			return fmt.Errorf("%s can't publish ports 80 and 443 on %s: %s, which is %s, publishes them on %s. %s",
				id.Description(), describeAddress(addr), other.Description(), other.State(inv.observed.Nodes),
				describeAddress(otherAddr), capitalize(advice))
		}
	}
	if !addr.IsUnspecified() && !slices.ContainsFunc(inv.observed.InterfaceAddresses, func(a netip.Addr) bool {
		return a.Unmap() == addr
	}) {
		return fmt.Errorf("%s can't publish ports 80 and 443 on %s: no network interface of this host has that address. "+
			"Add the address to one first, as the documentation on running several local clusters describes",
			id.Description(), addr)
	}
	return nil
}

// ConfiguredWithListenAddress returns the local clusters of the data home
// dataHome, other than id, whose config there declares the listen address
// listenAddress: the default local cluster first, then the named ones by
// name. A config that is missing or does not load declares none.
//
// It reads configs, not Docker, so it finds a local cluster another create
// is setting up before its kind cluster exists, but also a deleted one,
// whose config stays. It explains a failure to publish on listenAddress;
// CheckCreate decides what clashes.
func ConfiguredWithListenAddress(dataHome string, id Identity, listenAddress string) []Identity {
	addr, err := netip.ParseAddr(listenAddress)
	if err != nil {
		return nil
	}

	candidates := []Identity{Default(dataHome)}
	entries, _ := os.ReadDir(config.LocalClustersDir(dataHome))
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		if named, err := Named(entry.Name(), dataHome); err == nil {
			candidates = append(candidates, named)
		}
	}

	var configured []Identity
	for _, other := range candidates {
		if other.KindName == id.KindName {
			continue
		}
		loaded, err := LoadConfig(other)
		if err != nil {
			continue
		}
		if otherAddr, err := netip.ParseAddr(loaded.ListenAddress()); err == nil && otherAddr.Unmap() == addr.Unmap() {
			configured = append(configured, other)
		}
	}
	return configured
}

// recreateAdvice is what to do about the local cluster id publishing its
// ports 80 and 443 on every host address, which its kind cluster fixes
// when it is created.
func recreateAdvice(id Identity) string {
	return fmt.Sprintf("re-create %s on a specific IP of this host: delete it, set cluster.listenAddress "+
		"in its config to that IP, and create it again", id.Description())
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
