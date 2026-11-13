package registry

import (
	"context"
	"encoding/binary"
	"fmt"
	"hash/fnv"
	"net/netip"

	cerrdefs "github.com/containerd/errdefs"
	"github.com/moby/moby/api/types/network"
	"github.com/moby/moby/client"
	"github.com/pkg/errors"

	"github.com/educates/educates-training-platform/client-programs/pkg/docker"
)

// mirrorAddressWindowMax caps how many addresses directly below the
// registry's fixed address are set aside for mirror fixed addresses. On a
// subnet too small for that, the window shrinks to a quarter of the subnet's
// usable addresses.
const mirrorAddressWindowMax = 200

// clusterNetwork is what the fixed-address layout needs to know about the
// cluster network.
type clusterNetwork struct {
	subnet  netip.Prefix
	gateway netip.Addr
	// holders maps each IPv4 address in use to the name of the container
	// holding it.
	holders map[netip.Addr]string
}

// registryFixedAddress returns the local registry's fixed address on the
// cluster network: the subnet's highest usable address, or the one below it
// when that is the gateway. Docker hands out dynamic addresses from the
// bottom of the subnet upward, so the top stays clear of kind nodes and of
// every container connected without a fixed address.
func registryFixedAddress(nw clusterNetwork) (netip.Addr, error) {
	first, last, err := usableIPv4Range(nw.subnet)
	if err != nil {
		return netip.Addr{}, err
	}

	addr := last
	if uint32ToIPv4(addr) == nw.gateway {
		addr--
	}

	if addr < first {
		return netip.Addr{}, errors.Errorf("subnet %s is too small for fixed addresses", nw.subnet)
	}

	return uint32ToIPv4(addr), nil
}

// mirrorFixedAddress returns a mirror's fixed address on the cluster network.
// Mirrors share a window of addresses directly below the registry's. A
// mirror already holding an address in the window keeps it. Otherwise it
// starts at a slot hashed from its container name, so its address does not
// depend on the order mirrors are deployed in, and moves down past any
// address the gateway or another container holds, wrapping within the
// window.
func mirrorFixedAddress(nw clusterNetwork, containerName string) (netip.Addr, error) {
	registryAddr, err := registryFixedAddress(nw)
	if err != nil {
		return netip.Addr{}, err
	}

	first, last, err := usableIPv4Range(nw.subnet)
	if err != nil {
		return netip.Addr{}, err
	}

	window := min(mirrorAddressWindowMax, (last-first+1)/4)
	if window == 0 {
		return netip.Addr{}, errors.Errorf("subnet %s is too small for mirror fixed addresses", nw.subnet)
	}

	top := ipv4ToUint32(registryAddr) - 1
	bottom := top - (window - 1)

	for addr, holder := range nw.holders {
		value := ipv4ToUint32(addr)

		if holder == containerName && value >= bottom && value <= top && addr != nw.gateway {
			return addr, nil
		}
	}

	hash := fnv.New32a()
	hash.Write([]byte(containerName))
	start := hash.Sum32() % window

	for i := uint32(0); i < window; i++ {
		addr := uint32ToIPv4(top - (start+i)%window)

		if addr == nw.gateway {
			continue
		}

		if holder, held := nw.holders[addr]; held && holder != containerName {
			continue
		}

		return addr, nil
	}

	return netip.Addr{}, errors.Errorf("no free mirror fixed address left in subnet %s", nw.subnet)
}

// usableIPv4Range returns the first and last usable addresses of an IPv4
// subnet, leaving out its network and broadcast addresses.
func usableIPv4Range(subnet netip.Prefix) (first, last uint32, err error) {
	if !subnet.IsValid() || !subnet.Addr().Is4() {
		return 0, 0, errors.Errorf("cluster network has no IPv4 subnet")
	}

	subnet = subnet.Masked()

	if subnet.Bits() > 30 {
		return 0, 0, errors.Errorf("subnet %s is too small for fixed addresses", subnet)
	}

	base := uint64(ipv4ToUint32(subnet.Addr()))
	size := uint64(1) << (32 - subnet.Bits())

	return uint32(base + 1), uint32(base + size - 2), nil
}

func ipv4ToUint32(addr netip.Addr) uint32 {
	octets := addr.As4()
	return binary.BigEndian.Uint32(octets[:])
}

func uint32ToIPv4(value uint32) netip.Addr {
	var octets [4]byte
	binary.BigEndian.PutUint32(octets[:], value)
	return netip.AddrFrom4(octets)
}

// inspectClusterNetwork reads the cluster network's IPv4 subnet, gateway and
// the addresses its containers hold. found is false when the network does
// not exist, which is the case until a kind cluster has been created.
func inspectClusterNetwork(ctx context.Context, cli *client.Client) (nw clusterNetwork, found bool, err error) {
	result, err := cli.NetworkInspect(ctx, ClusterNetworkName, client.NetworkInspectOptions{})

	if cerrdefs.IsNotFound(err) {
		return clusterNetwork{}, false, nil
	}

	if err != nil {
		return clusterNetwork{}, false, errors.Wrapf(err, "unable to inspect %s network", ClusterNetworkName)
	}

	for _, config := range result.Network.IPAM.Config {
		if config.Subnet.IsValid() && config.Subnet.Addr().Is4() {
			nw.subnet = config.Subnet.Masked()
			nw.gateway = config.Gateway
			break
		}
	}

	nw.holders = map[netip.Addr]string{}

	for _, endpoint := range result.Network.Containers {
		if endpoint.IPv4Address.IsValid() {
			nw.holders[endpoint.IPv4Address.Addr()] = endpoint.Name
		}
	}

	return nw, true, nil
}

// linkToClusterNetwork connects a registry or mirror container to the
// cluster network at the fixed address fixedAddress picks for it. Without a
// fixed address, Docker can hand the container a different address each time
// it restarts, leaving the registry Service pointing at whichever container
// took the old one, and letting the container take an address a kind node
// expects to get back.
//
// A container already holding its fixed address is left alone. When no
// fixed address can be worked out, or Docker refuses it, the container is
// connected without one, so it stays reachable, and a warning is reported.
// Docker Engine 29.0.0 to 29.0.3 always refuse a fixed address on kind's
// network, and older engines refuse it until Docker restarts after the
// network is created, because kind configures only the network's IPv6
// subnet.
//
// Nothing is linked when the cluster network does not exist yet; creating a
// cluster links the registry and mirrors then.
func linkToClusterNetwork(containerName string, fixedAddress func(clusterNetwork) (netip.Addr, error), p Progress) error {
	ctx := context.Background()

	report(p, "linking to cluster network")

	cli, err := docker.NewDockerClient()

	if err != nil {
		return errors.Wrap(err, "unable to create docker client")
	}

	nw, found, err := inspectClusterNetwork(ctx, cli)

	if err != nil {
		return err
	}

	if !found {
		report(p, fmt.Sprintf("no %s network yet, not linking to cluster network", ClusterNetworkName))
		return nil
	}

	containerInfo, err := cli.ContainerInspect(ctx, containerName, client.ContainerInspectOptions{})

	if err != nil {
		return errors.Wrapf(err, "unable to inspect container %s", containerName)
	}

	endpoint, connected := containerInfo.Container.NetworkSettings.Networks[ClusterNetworkName]
	connected = connected && endpoint != nil

	addr, err := fixedAddress(nw)

	if err != nil {
		warn(p, fmt.Sprintf("no fixed address for %s: %v", containerName, err))

		if connected {
			return nil
		}

		return connectToClusterNetwork(ctx, cli, containerName, netip.Addr{})
	}

	if connected && endpoint.IPAMConfig != nil && endpoint.IPAMConfig.IPv4Address == addr {
		return nil
	}

	if connected {
		cli.NetworkDisconnect(ctx, ClusterNetworkName, client.NetworkDisconnectOptions{Container: containerName})
	}

	connect := func(a netip.Addr) error {
		return connectToClusterNetwork(ctx, cli, containerName, a)
	}

	refused, err := connectWithFallback(connect, addr)

	if err != nil {
		return err
	}

	if refused != nil {
		warn(p, fmt.Sprintf("Docker refused fixed address %s for %s (%v), so a Docker restart can still change its address (Docker Engine 29.0.4 or later avoids this)", addr, containerName, refused))
	}

	return nil
}

// connectWithFallback connects through connect at the fixed address addr,
// and when that fails, at an address Docker picks. refused is the fixed
// address's error when only the fallback succeeded. When the fallback fails
// too, err is its error and refused is nil: a container that can't join at
// any address says nothing about the fixed one.
func connectWithFallback(connect func(netip.Addr) error, addr netip.Addr) (refused error, err error) {
	fixedErr := connect(addr)

	if fixedErr == nil {
		return nil, nil
	}

	if err := connect(netip.Addr{}); err != nil {
		return nil, err
	}

	return fixedErr, nil
}

// connectToClusterNetwork connects a container to the cluster network, at
// addr when it is valid and at an address Docker picks otherwise.
func connectToClusterNetwork(ctx context.Context, cli *client.Client, containerName string, addr netip.Addr) error {
	endpointSettings := &network.EndpointSettings{}

	if addr.IsValid() {
		endpointSettings.IPAMConfig = &network.EndpointIPAMConfig{IPv4Address: addr}
	}

	_, err := cli.NetworkConnect(ctx, ClusterNetworkName, client.NetworkConnectOptions{
		Container:      containerName,
		EndpointConfig: endpointSettings,
	})

	if err != nil {
		return errors.Wrapf(err, "unable to connect %s to %s network", containerName, ClusterNetworkName)
	}

	return nil
}
