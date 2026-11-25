package localclusters

import (
	"errors"
	"fmt"
	"io/fs"
	"net/netip"
	"os"
	"slices"

	"github.com/educates/educates-training-platform/client-programs/pkg/config"
	"github.com/educates/educates-training-platform/client-programs/pkg/config/hostinfo"
	"github.com/educates/educates-training-platform/client-programs/pkg/config/v1alpha1"
)

// whyRouterAddressResolves is why the router address must be one the
// host is reached at from inside a cluster.
const whyRouterAddressResolves = "pods and the nodes' containerd resolve every local cluster's names to the router address"

// setRouterAddressCommand is the command that sets the router address.
const setRouterAddressCommand = "educates local router update --address <ip>"

// LoadRouterAddress returns the router address the router file of the
// data home dataHome, router.yaml, declares, in its canonical form, once
// it passes CheckRouterAddress against interfaces, the addresses of the
// host's network interfaces. A file that doesn't load, as
// config.LoadLocalRouter loads it, is that error, and an address the
// rules refuse is an error naming the file, why, and how to set another.
// A missing file is an error wrapping fs.ErrNotExist that says how to
// write it.
func LoadRouterAddress(dataHome string, interfaces []netip.Addr) (string, error) {
	router, err := loadRouterFile(dataHome)
	if err != nil {
		return "", err
	}
	address, err := CheckRouterAddress(router.Address, interfaces)
	if err != nil {
		return "", fmt.Errorf("%s: %w. Set another in it, or with:\n\n  %s",
			config.LocalRouterPath(dataHome), err, setRouterAddressCommand)
	}
	return address, nil
}

// configuredRouterAddress returns the router address the router file of
// the data home dataHome, router.yaml, declares, in its canonical form,
// once it passes the rules of CheckRouterAddress that need no host
// interface: the address every local cluster's names resolve to. A file
// that doesn't load is that error, and an address the rules refuse an
// error naming the file and why. A missing file is the error
// config.LoadLocalRouter returns, wrapping fs.ErrNotExist.
//
// Rendering what serves the local clusters' names takes the address as it
// is configured: only where the router is created is it checked against
// the host's interfaces, with LoadRouterAddress.
func configuredRouterAddress(dataHome string) (string, error) {
	router, err := loadRouterFile(dataHome)
	if err != nil {
		return "", err
	}
	ip, err := checkSpecificRouterAddress(router.Address)
	if err != nil {
		return "", fmt.Errorf("%s: %w. Set another in it, or with:\n\n  %s",
			config.LocalRouterPath(dataHome), err, setRouterAddressCommand)
	}
	return ip.String(), nil
}

// loadRouterFile loads the router file of the data home dataHome, as
// config.LoadLocalRouter loads it. A missing file is an error wrapping
// fs.ErrNotExist that says how to write it.
func loadRouterFile(dataHome string) (*v1alpha1.EducatesLocalRouter, error) {
	router, err := config.LoadLocalRouter(dataHome)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, fmt.Errorf("%w. The router address, an IP address of a network interface of this host, "+
			"must be set with:\n\n  %s", err, setRouterAddressCommand)
	}
	return router, err
}

// EnsureRouterFile writes the router file of the data home dataHome,
// router.yaml, when it is missing, with the host's IP address as the
// router address and a comment saying that an alias such as 10.10.10.1,
// added to a network interface, survives network changes. It returns the
// address it wrote, or an empty string when the file exists: an existing
// file is never overwritten, nor read.
//
// With no IP address of the host detected, or one the router address
// rules refuse whatever the interfaces, nothing is written, and the error
// says the router address must be set and how.
func EnsureRouterFile(dataHome string) (string, error) {
	path := config.LocalRouterPath(dataHome)
	switch _, err := os.Stat(path); {
	case err == nil:
		return "", nil
	case !errors.Is(err, fs.ErrNotExist):
		return "", fmt.Errorf("read %s: %w", path, err)
	}
	ip, err := hostinfo.DetectHostIP()
	if err == nil {
		_, err = checkSpecificRouterAddress(ip)
	}
	if err != nil {
		return "", fmt.Errorf("%s is missing, and no IP address of this host was detected to write in it (%v). "+
			"The router address, an IP address of a network interface of this host, must be set with:\n\n  %s\n\n"+
			"An alias such as 10.10.10.1, added to a network interface, survives network changes",
			path, err, setRouterAddressCommand)
	}
	created, err := config.CreateLocalRouter(dataHome, ip, true)
	if err != nil || !created {
		return "", err
	}
	return ip, nil
}

// CheckRouterAddress returns the router address address in its canonical
// form when it can serve local clusters, and otherwise an error saying
// why it can't. The router address must be a specific IP address of a
// network interface of the host, one of interfaces:
//
//   - not the unspecified address, 0.0.0.0 or ::, which names no address
//     to reach;
//   - not a loopback address, 127.0.0.0/8 or ::1, at which pods and nodes
//     would reach themselves instead of the host;
//   - the address of one of the host's interfaces, since an address on
//     none reaches nothing.
//
// An IPv4 address written as IPv4-mapped IPv6 is its IPv4 address.
func CheckRouterAddress(address string, interfaces []netip.Addr) (string, error) {
	ip, err := checkSpecificRouterAddress(address)
	if err != nil {
		return "", err
	}
	if !slices.ContainsFunc(interfaces, func(a netip.Addr) bool { return a.Unmap() == ip }) {
		return "", fmt.Errorf("the router address %s is on no network interface of this host, and an address on "+
			"no interface reaches nothing: add it to one first, as an alias, or use an address one has", address)
	}
	return ip.String(), nil
}

// checkSpecificRouterAddress returns the router address address as an IP
// address when it is a specific one, by the rules of CheckRouterAddress
// that need no host interface, and otherwise an error saying why it is
// not.
func checkSpecificRouterAddress(address string) (netip.Addr, error) {
	if address == "" {
		return netip.Addr{}, fmt.Errorf("the router address must be set to an IP address of a network interface of this host")
	}
	ip, err := netip.ParseAddr(address)
	if err != nil {
		return netip.Addr{}, fmt.Errorf("the router address %q is not an IP address", address)
	}
	ip = ip.Unmap()
	if ip.IsUnspecified() {
		return netip.Addr{}, fmt.Errorf("the router address %s is the unspecified address, which names no host to reach: "+
			"%s, so it must be a specific IP address of this host", address, whyRouterAddressResolves)
	}
	if ip.IsLoopback() {
		return netip.Addr{}, fmt.Errorf("the router address %s is a loopback address: %s, and at a loopback address "+
			"they reach themselves instead of this host", address, whyRouterAddressResolves)
	}
	return ip, nil
}
