package cmd

import (
	"fmt"
	"net"
	"net/netip"

	"github.com/spf13/cobra"

	"github.com/educates/educates-training-platform/client-programs/pkg/localclusters"
)

// Flags of cluster workshop serve whose defaults depend on the local
// cluster selected.
const (
	localHostFlagName = "local-host"
	hugoPortFlagName  = "hugo-port"
)

// serveListen is where cluster workshop serve listens: the host address
// and port of its proxy, and the port of its Hugo server.
type serveListen struct {
	LocalHost string
	LocalPort int
	HugoPort  int
}

// listenForSelectedCluster sets where o listens, as chooseServeListen
// says, for the local cluster the command c selects with --local-cluster
// or EDUCATES_LOCAL_CLUSTER.
func (o *ClusterWorkshopServeOptions) listenForSelectedCluster(c *cobra.Command) error {
	id, selected, err := explicitlySelectedLocalCluster(c)
	if err != nil {
		return err
	}
	listen, err := chooseServeListen(
		serveListen{LocalHost: o.LocalHost, LocalPort: o.LocalPort, HugoPort: o.HugoPort},
		c.Flags().Changed, selected,
		func() (string, error) { return localclusters.ConfiguredListenAddress(id) },
		freeLoopbackPort)
	if err != nil {
		return err
	}
	o.LocalHost, o.LocalPort, o.HugoPort = listen.LocalHost, listen.LocalPort, listen.HugoPort
	return nil
}

// chooseServeListen returns where cluster workshop serve listens. flags
// holds the values of --local-host, --local-port and --hugo-port, and
// given reports whether the flag it names was given on the command line.
//
// With no local cluster selected, it returns flags as they are. With one
// selected, two defaults change, so that a serve for each of several
// local clusters can run at once. The proxy listens on the selected
// cluster's listen address, which listenAddress returns, when that is a
// specific IP address: the cluster's loopback Service reaches the host
// there, so each cluster's serve can keep port 10081 on an address of its
// own. Otherwise it keeps --local-host. The Hugo server listens on the
// free port freePort returns, instead of every serve wanting 1313. A flag
// given on the command line still wins, and what it overrides is never
// consulted.
func chooseServeListen(flags serveListen, given func(flag string) bool, selected bool,
	listenAddress func() (string, error), freePort func() (int, error)) (serveListen, error) {
	if !selected {
		return flags, nil
	}
	listen := flags
	if !given(localHostFlagName) {
		addr, err := listenAddress()
		if err != nil {
			return serveListen{}, fmt.Errorf("%w; pass --%s to choose the address the proxy listens on", err, localHostFlagName)
		}
		if host, ok := specificListenHost(addr); ok {
			listen.LocalHost = host
		}
	}
	if !given(hugoPortFlagName) {
		port, err := freePort()
		if err != nil {
			return serveListen{}, fmt.Errorf("choose a free port for the Hugo server: %w; pass --%s to choose one", err, hugoPortFlagName)
		}
		listen.HugoPort = port
	}
	return listen, nil
}

// freeLoopbackPort returns a port that no listener holds on 127.0.0.1,
// where the Hugo server listens, as the operating system picks one for a
// listener that asks for any port.
func freeLoopbackPort() (int, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port, nil
}

// specificListenHost returns the host part of a listen address on the
// IP address address, and reports whether address is a specific IP
// address, as localclusters.ValidateListenAddress accepts one: neither
// one meaning every address nor a loopback address. An IPv4-mapped IPv6
// address is given as IPv4, and an IPv6 address is bracketed, as a host
// joined with a port needs it to be.
func specificListenHost(address string) (string, bool) {
	if localclusters.ValidateListenAddress(address) != nil {
		return "", false
	}
	ip := netip.MustParseAddr(address).Unmap()
	if ip.Is6() {
		return "[" + ip.String() + "]", true
	}
	return ip.String(), true
}
