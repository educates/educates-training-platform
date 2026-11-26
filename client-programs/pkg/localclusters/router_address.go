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
		return "", routerAddressRefused(dataHome, err)
	}
	return address, nil
}

// configuredRouterAddress returns the router address the router file of
// the data home dataHome, router.yaml, declares, in its canonical form,
// once it passes config.CheckSpecificRouterAddress, the rules of
// CheckRouterAddress that need no host interface: the address every local
// cluster's names resolve to. A file that doesn't load is that error, and
// an address the rules refuse an error naming the file, why, and how to
// set another. A missing file is an error wrapping fs.ErrNotExist that
// says how to write it.
//
// Rendering what serves the local clusters' names takes the address as it
// is configured: only where the router is created is it checked against
// the host's interfaces, with LoadRouterAddress.
func configuredRouterAddress(dataHome string) (string, error) {
	router, err := loadRouterFile(dataHome)
	if err != nil {
		return "", err
	}
	ip, err := config.CheckSpecificRouterAddress(router.Address)
	if err != nil {
		return "", routerAddressRefused(dataHome, err)
	}
	return ip.String(), nil
}

// routerAddressRefused is the error of the router file of the data home
// dataHome declaring a router address the router address rules refuse,
// for the reason err gives: it names the file, why, and how to set
// another.
func routerAddressRefused(dataHome string, err error) error {
	return fmt.Errorf("%s: %w. Set another in it, or with:\n\n  %s", config.LocalRouterPath(dataHome), err, setRouterAddressCommand)
}

// loadRouterFile loads the router file of the data home dataHome, as
// config.LoadLocalRouter loads it, once a missing file has been written
// from the default local cluster's config, as moveListenAddress writes
// it. A file still missing is an error wrapping fs.ErrNotExist that says
// how to write it.
func loadRouterFile(dataHome string) (*v1alpha1.EducatesLocalRouter, error) {
	router, err := config.LoadLocalRouter(dataHome)
	if errors.Is(err, fs.ErrNotExist) {
		if err := moveListenAddress(dataHome); err != nil {
			return nil, err
		}
		router, err = config.LoadLocalRouter(dataHome)
	}
	if errors.Is(err, fs.ErrNotExist) {
		return nil, fmt.Errorf("%w. The router address, an IP address of a network interface of this host, "+
			"must be set with:\n\n  %s", err, setRouterAddressCommand)
	}
	return router, err
}

// moveListenAddress moves the listen address the default local cluster's
// config of the data home dataHome still declares, as a data home an
// earlier release wrote has it, to the router file, as
// config.MoveClusterListenAddress moves it, with a note on standard
// error: it becomes the router address when the router file is missing.
// Whatever reads or writes the router file of a data home that has none
// moves it first, whichever local cluster it is for, so that a command
// that reads a named cluster's config, or the router address alone, finds
// the address the default config declared.
func moveListenAddress(dataHome string) error {
	return config.MoveClusterListenAddress(dataHome, config.LocalConfigPath(dataHome), os.Stderr)
}

// EnsureRouterFile writes the router file of the data home dataHome,
// router.yaml, when it is missing, with the host's IP address as the
// router address and a comment saying that an alias such as 10.10.10.1,
// added to a network interface, survives network changes. It returns the
// address it wrote, or an empty string when the file exists: an existing
// file is never overwritten, nor read.
//
// A missing file is first written from the listen address the default
// local cluster's config still declares, as moveListenAddress writes it,
// which then takes the place of the host's IP address, and the address
// returned is empty.
//
// With no IP address of the host detected, or one the router address
// rules refuse whatever the interfaces, nothing is written, and the error
// says the router address must be set and how.
func EnsureRouterFile(dataHome string) (string, error) {
	path := config.LocalRouterPath(dataHome)
	if exists, err := routerFileExists(path); err != nil || exists {
		return "", err
	}
	if err := moveListenAddress(dataHome); err != nil {
		return "", err
	}
	if exists, err := routerFileExists(path); err != nil || exists {
		return "", err
	}
	ip, err := hostinfo.DetectHostIP()
	if err == nil {
		_, err = config.CheckSpecificRouterAddress(ip)
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

// routerFileExists reports whether the router file at path exists.
func routerFileExists(path string) (bool, error) {
	switch _, err := os.Stat(path); {
	case err == nil:
		return true, nil
	case errors.Is(err, fs.ErrNotExist):
		return false, nil
	default:
		return false, fmt.Errorf("read %s: %w", path, err)
	}
}

// SetRouterAddress makes the router file of the data home, router.yaml,
// declare address as the router address, in its canonical form, once it
// passes CheckRouterAddress against the host's interfaces, as
// config.SetLocalRouterAddress writes it: keeping the file's comments, or
// creating a missing file. It returns the address the file declared
// before, in its canonical form when it is an IP address, and empty when
// the file was missing.
//
// An address the rules refuse is an error saying why, and a file that
// does not load is that error: either way nothing is written.
func (inv Inventory) SetRouterAddress(address string) (string, error) {
	canonical, err := CheckRouterAddress(address, inv.observed.InterfaceAddresses)
	if err != nil {
		return "", err
	}
	previous := ""
	switch router, err := loadRouterFile(inv.dataHome); {
	case errors.Is(err, fs.ErrNotExist):
	case err != nil:
		return "", err
	default:
		previous = router.Address
		if ip, err := netip.ParseAddr(previous); err == nil {
			previous = ip.Unmap().String()
		}
	}
	if err := config.SetLocalRouterAddress(inv.dataHome, canonical); err != nil {
		return "", err
	}
	return previous, nil
}

// CheckRouterAddress returns the router address address in its canonical
// form when it can serve local clusters, and otherwise an error saying
// why it can't. The router address must be a specific IP address, as
// config.CheckSpecificRouterAddress decides, neither the unspecified
// address nor a loopback address, and the address of one of the host's
// network interfaces, one of interfaces, since an address on none reaches
// nothing.
//
// An IPv4 address written as IPv4-mapped IPv6 is its IPv4 address.
func CheckRouterAddress(address string, interfaces []netip.Addr) (string, error) {
	ip, err := config.CheckSpecificRouterAddress(address)
	if err != nil {
		return "", err
	}
	if !slices.ContainsFunc(interfaces, func(a netip.Addr) bool { return a.Unmap() == ip }) {
		return "", fmt.Errorf("the router address %s is on no network interface of this host, and an address on "+
			"no interface reaches nothing: add it to one first, as an alias, or use an address one has", address)
	}
	return ip.String(), nil
}
