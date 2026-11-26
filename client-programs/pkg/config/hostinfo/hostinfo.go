// Package hostinfo derives runtime host information used by laptop-mode
// CLI defaulting (e.g. the host's IP address as the router address every
// local cluster is served on).
//
// Kept separate from the translator so the translator stays deterministic
// and unit-testable: anything host-derived flows in via call sites that
// explicitly fetch it.
package hostinfo

import (
	"fmt"
	"net"
	"net/netip"
	"strings"
)

// DetectHostIP returns the IPv4 address that would be used as the source
// when reaching the outside world, as detectHostIP finds it. It is a
// variable so that tests replace it, and don't depend on the network of
// the machine they run on.
var DetectHostIP = detectHostIP

// detectHostIP returns the IPv4 address that would be used as the source
// when reaching the outside world. The UDP "fake dial" trick: opening a
// UDP socket toward a routable address makes the OS populate the local
// address with the route's source IP, without sending any packet.
//
// This deliberately does NOT use net.InterfaceAddrs(): on a typical laptop
// that returns five-plus addresses (loopback, multiple interfaces, IPv6
// link-locals) and we'd guess at which one is reachable from a workshop
// container running in kind.
func detectHostIP() (string, error) {
	conn, err := net.Dial("udp", "8.8.8.8:80")
	if err != nil {
		return "", fmt.Errorf("detect host IP: %w", err)
	}
	defer conn.Close()
	local := conn.LocalAddr().(*net.UDPAddr)
	return local.IP.String(), nil
}

// InterfaceAddresses returns every IP address of the host's network
// interfaces, loopback and link-local ones included, an IPv4-mapped IPv6
// address as its IPv4 address.
func InterfaceAddresses() ([]netip.Addr, error) {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return nil, fmt.Errorf("unable to list the addresses of the host's network interfaces: %w", err)
	}
	interfaces := make([]netip.Addr, 0, len(addrs))
	for _, a := range addrs {
		ipNet, ok := a.(*net.IPNet)
		if !ok {
			continue
		}
		if addr, ok := netip.AddrFromSlice(ipNet.IP); ok {
			interfaces = append(interfaces, addr.Unmap())
		}
	}
	return interfaces, nil
}

// NipDomain converts a dotted-quad IPv4 (e.g. 192.168.1.10) into the
// nip.io wildcard subdomain shape (192-168-1-10.nip.io). nip.io serves
// both `1-2-3-4.nip.io` and `1.2.3.4.nip.io`; the dash form survives
// Kubernetes' DNS label length / character restrictions when subdomains
// are prepended (e.g. workshop names).
func NipDomain(ip string) string {
	return strings.ReplaceAll(ip, ".", "-") + ".nip.io"
}
