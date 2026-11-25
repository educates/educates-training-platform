package cmd

import (
	"cmp"
	"fmt"
	"net"
	"net/netip"

	"github.com/spf13/cobra"

	"github.com/educates/educates-training-platform/client-programs/pkg/cluster"
	"github.com/educates/educates-training-platform/client-programs/pkg/config/hostinfo"
	"github.com/educates/educates-training-platform/client-programs/pkg/localclusters"
	"github.com/educates/educates-training-platform/client-programs/pkg/utils"
)

// Flags of cluster workshop serve whose defaults depend on the local
// cluster it serves.
const (
	localHostFlagName = "local-host"
	localPortFlagName = "local-port"
	proxyPortFlagName = "proxy-port"
	hugoPortFlagName  = "hugo-port"
)

// hugoHost is the address the Hugo server that cluster workshop serve
// runs listens on, Hugo's default bind address.
const hugoHost = "127.0.0.1"

// serveListen is where cluster workshop serve listens: the host address
// and port of its proxy, the port workshop sessions reach the proxy on,
// which the workshop is patched with, and the port of its Hugo server.
type serveListen struct {
	LocalHost string
	LocalPort int
	ProxyPort int
	HugoPort  int
}

// serveTarget is what says which local cluster, if any, cluster workshop
// serve serves.
type serveTarget struct {
	// selected is the local cluster --local-cluster or
	// EDUCATES_LOCAL_CLUSTER selects, when isSelected.
	selected   localclusters.Identity
	isSelected bool

	// kubeContext returns the kube context serve reaches its cluster
	// through: --context when given, and otherwise the current context,
	// empty when there is none. It is consulted only when no local
	// cluster is selected.
	kubeContext func() string

	// dataHome is the data home, whose local clusters a kube context can
	// be the context of, and whose router serves them.
	dataHome string
}

// cluster returns the local cluster serve serves, and reports whether it
// serves one: the selected local cluster, and with none selected, the
// local cluster whose context is the kube context, as
// localclusters.ClusterOfContext recognises it.
func (t serveTarget) cluster() (localclusters.Identity, bool) {
	if t.isSelected {
		return t.selected, true
	}
	return localclusters.ClusterOfContext(t.kubeContext(), t.dataHome)
}

// listenForTargetedCluster sets where o listens, as chooseServeListen
// says, for the local cluster the command c serves: the one it selects
// with --local-cluster or EDUCATES_LOCAL_CLUSTER, and otherwise the one
// whose kube context it reaches its cluster through.
func (o *ClusterWorkshopServeOptions) listenForTargetedCluster(c *cobra.Command) error {
	id, selected, err := explicitlySelectedLocalCluster(c)
	if err != nil {
		return err
	}
	target := serveTarget{
		selected:   id,
		isSelected: selected,
		kubeContext: func() string {
			return cmp.Or(o.Context, cluster.CurrentContext(o.Kubeconfig))
		},
		dataHome: utils.GetEducatesHomeDir(),
	}
	listen, err := chooseServeListen(
		serveListen{LocalHost: o.LocalHost, LocalPort: o.LocalPort, ProxyPort: o.ProxyPort, HugoPort: o.HugoPort},
		c.Flags().Changed, target, hostRouterAddress, freePortOn)
	if err != nil {
		return err
	}
	o.LocalHost, o.LocalPort, o.ProxyPort, o.HugoPort = listen.LocalHost, listen.LocalPort, listen.ProxyPort, listen.HugoPort
	return nil
}

// chooseServeListen returns where cluster workshop serve listens. flags
// holds the values of --local-host, --local-port, --proxy-port and
// --hugo-port, and given reports whether the flag it names was given on
// the command line.
//
// When serve serves no local cluster, as target says, it returns flags as
// they are. When it serves one, the defaults change, so that a serve for
// each of several local clusters can run at once:
//
//   - The proxy listens on the router address of the data home, which
//     routerAddress returns: the cluster's loopback Service names the
//     cluster's localhost.<domain>, which resolves to the router address,
//     so workshop sessions reach the host there.
//   - The proxy listens on the port freePort returns as free on the host
//     the proxy listens on, instead of every serve wanting 10081 on the
//     one router address.
//   - The workshop is patched with the port the proxy listens on, since
//     sessions reach the proxy directly.
//   - The Hugo server listens on the port freePort returns as free on
//     the loopback address it listens on, instead of every serve wanting
//     1313.
//
// A flag given on the command line still wins, and what it overrides is
// never consulted: with every one of them given, not even the cluster
// serve serves.
func chooseServeListen(flags serveListen, given func(flag string) bool, target serveTarget,
	routerAddress func(dataHome string) (string, error), freePort func(host string) (int, error)) (serveListen, error) {
	if given(localHostFlagName) && given(localPortFlagName) && given(proxyPortFlagName) && given(hugoPortFlagName) {
		return flags, nil
	}
	if _, ok := target.cluster(); !ok {
		return flags, nil
	}
	listen := flags
	if !given(localHostFlagName) {
		address, err := routerAddress(target.dataHome)
		if err != nil {
			// The error can end with a command to run, on a line of its
			// own, so the flag goes on a line after it.
			return serveListen{}, fmt.Errorf("%w\n\nOr choose the address the proxy listens on with --%s", err, localHostFlagName)
		}
		listen.LocalHost = listenHost(address)
	}
	if !given(localPortFlagName) {
		port, err := freePort(listen.LocalHost)
		if err != nil {
			return serveListen{}, fmt.Errorf("choose a free port for the proxy on %s: %w; pass --%s to choose one",
				listen.LocalHost, err, localPortFlagName)
		}
		listen.LocalPort = port
	}
	if !given(proxyPortFlagName) {
		listen.ProxyPort = listen.LocalPort
	}
	if !given(hugoPortFlagName) {
		port, err := freePort(hugoHost)
		if err != nil {
			return serveListen{}, fmt.Errorf("choose a free port for the Hugo server: %w; pass --%s to choose one", err, hugoPortFlagName)
		}
		listen.HugoPort = port
	}
	return listen, nil
}

// hostRouterAddress returns the router address of the data home
// dataHome, as localclusters.LoadRouterAddress loads it, checked against
// the addresses of the host's network interfaces.
func hostRouterAddress(dataHome string) (string, error) {
	interfaces, err := hostinfo.InterfaceAddresses()
	if err != nil {
		return "", err
	}
	return localclusters.LoadRouterAddress(dataHome, interfaces)
}

// freePortOn returns a port that no listener holds on host, as the
// operating system picks one for a listener on host that asks for any
// port. host is joined with the port as the proxy and the Hugo server
// join it, so an IPv6 address is bracketed.
func freePortOn(host string) (int, error) {
	l, err := net.Listen("tcp", fmt.Sprintf("%s:0", host))
	if err != nil {
		return 0, err
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port, nil
}

// listenHost returns the host part of a listen address on the IP address
// address, bracketed when it is an IPv6 address, as a host joined with a
// port needs it to be.
func listenHost(address string) string {
	if ip, err := netip.ParseAddr(address); err == nil && ip.Unmap().Is6() {
		return "[" + ip.Unmap().String() + "]"
	}
	return address
}
