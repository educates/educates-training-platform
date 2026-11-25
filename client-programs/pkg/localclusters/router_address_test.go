package localclusters

import (
	"errors"
	"io/fs"
	"net/netip"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/educates/educates-training-platform/client-programs/pkg/config"
)

// A specific IP address of a host interface is a router address, in its
// canonical form.
func TestCheckRouterAddressAcceptsAnAddressOfAHostInterface(t *testing.T) {
	for address, want := range map[string]string{
		"10.10.10.1":          "10.10.10.1",
		"192.168.1.20":        "192.168.1.20",
		"::ffff:10.10.10.2":   "10.10.10.2",
		"fd00:0:0:0:0:0:0:20": "fd00::20",
	} {
		interfaces := append(slices.Clone(hostInterfaces), netip.MustParseAddr("fd00::20"))
		got, err := CheckRouterAddress(address, interfaces)
		if err != nil || got != want {
			t.Errorf("CheckRouterAddress(%q) = %q, %v, want %q, nil", address, got, err, want)
		}
	}
}

// routerFile writes body as the router file of the data home dataHome,
// and returns its path.
func routerFile(t *testing.T, dataHome, body string) string {
	t.Helper()
	path := config.LocalRouterPath(dataHome)
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

const routerHeader = "apiVersion: cli.educates.dev/v1alpha1\nkind: EducatesLocalRouter\n"

func TestLoadRouterAddressReturnsTheAddressOfTheRouterFile(t *testing.T) {
	dataHome := t.TempDir()
	routerFile(t, dataHome, routerHeader+"address: 10.10.10.1\n")

	got, err := LoadRouterAddress(dataHome, hostInterfaces)

	if err != nil || got != "10.10.10.1" {
		t.Errorf("LoadRouterAddress() = %q, %v, want 10.10.10.1, nil", got, err)
	}
}

// A router file whose address breaks the rules fails to load, naming the
// file, why, and how to set another.
func TestLoadRouterAddressRefusesAnAddressTheRulesRefuse(t *testing.T) {
	for address, why := range map[string]string{
		"127.0.0.1":  "loopback address",
		"0.0.0.0":    "unspecified address",
		"10.10.10":   "not an IP address",
		"10.10.10.9": "on no network interface of this host",
	} {
		t.Run(address, func(t *testing.T) {
			dataHome := t.TempDir()
			path := routerFile(t, dataHome, routerHeader+"address: "+address+"\n")

			_, err := LoadRouterAddress(dataHome, hostInterfaces)

			wantErrorMentioning(t, err, path, address, why, "educates local router update --address <ip>")
		})
	}
}

// A router file that breaks its schema fails with the schema's error.
func TestLoadRouterAddressOfAFileThatBreaksItsSchemaGivesTheSchemaError(t *testing.T) {
	dataHome := t.TempDir()
	path := routerFile(t, dataHome, routerHeader+"address: 10.10.10.1\nport: 80\n")

	_, err := LoadRouterAddress(dataHome, hostInterfaces)

	wantErrorMentioning(t, err, path, "schema validation failed", "port")
}

// A data home without the router file has no router address, which is an
// error wrapping fs.ErrNotExist that says how to set one.
func TestLoadRouterAddressWithoutTheRouterFile(t *testing.T) {
	dataHome := t.TempDir()

	_, err := LoadRouterAddress(dataHome, hostInterfaces)

	if !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("LoadRouterAddress() error = %v, want one wrapping fs.ErrNotExist", err)
	}
	wantErrorMentioning(t, err, config.LocalRouterPath(dataHome), "educates local router update --address <ip>")
}

// A data home without the router file gets one with the host's IP
// address, commented as such, which then loads.
func TestEnsureRouterFileWritesTheHostIPWhenMissing(t *testing.T) {
	stubHostIP(t, "192.168.1.20")
	dataHome := t.TempDir()

	wrote, err := EnsureRouterFile(dataHome)

	if err != nil || wrote != "192.168.1.20" {
		t.Fatalf("EnsureRouterFile() = %q, %v, want 192.168.1.20, nil", wrote, err)
	}
	body, _ := os.ReadFile(config.LocalRouterPath(dataHome))
	for _, want := range []string{"address: 192.168.1.20\n", "An alias such as\n# 10.10.10.1"} {
		if !strings.Contains(string(body), want) {
			t.Errorf("router.yaml does not hold %q:\n%s", want, body)
		}
	}
	if got, err := LoadRouterAddress(dataHome, hostInterfaces); err != nil || got != "192.168.1.20" {
		t.Errorf("LoadRouterAddress() = %q, %v, want 192.168.1.20, nil", got, err)
	}
}

// An existing router file is never overwritten, even one that does not
// load, and needs no host IP address.
func TestEnsureRouterFileLeavesAnExistingFile(t *testing.T) {
	for name, body := range map[string]string{
		"a commented file":          routerHeader + "# My alias.\naddress: 10.10.10.1\n",
		"a file that does not load": routerHeader + "address: 127.0.0.1\nport: 80\n",
	} {
		t.Run(name, func(t *testing.T) {
			stubHostIP(t, "")
			dataHome := t.TempDir()
			path := routerFile(t, dataHome, body)

			wrote, err := EnsureRouterFile(dataHome)

			if err != nil || wrote != "" {
				t.Fatalf("EnsureRouterFile() = %q, %v, want nothing written and no error", wrote, err)
			}
			if got, _ := os.ReadFile(path); string(got) != body {
				t.Errorf("router.yaml changed to\n%s", got)
			}
		})
	}
}

// With no router file and no host IP address to write in one, the
// router address must be set by hand, and nothing is written.
func TestEnsureRouterFileWithNoHostIPSaysTheAddressMustBeSet(t *testing.T) {
	for name, hostIP := range map[string]string{
		"none detected":       "",
		"a loopback detected": "127.0.0.1",
	} {
		t.Run(name, func(t *testing.T) {
			stubHostIP(t, hostIP)
			dataHome := t.TempDir()

			_, err := EnsureRouterFile(dataHome)

			wantErrorMentioning(t, err, config.LocalRouterPath(dataHome), "no IP address of this host",
				"must be set", "educates local router update --address <ip>", "An alias such as 10.10.10.1")
			if _, statErr := os.Stat(config.LocalRouterPath(dataHome)); !errors.Is(statErr, fs.ErrNotExist) {
				t.Errorf("router.yaml was written: %v", statErr)
			}
		})
	}
}

// Each refusal says why the address can't be the router address.
func TestCheckRouterAddressRefusesWhatCantServeLocalClusters(t *testing.T) {
	cases := []struct {
		name    string
		address string
		why     []string
	}{
		{name: "empty", address: "", why: []string{"must be set"}},
		{name: "malformed", address: "10.10.10", why: []string{`"10.10.10"`, "not an IP address"}},
		{name: "a host name", address: "localhost", why: []string{`"localhost"`, "not an IP address"}},
		{name: "unspecified IPv4", address: "0.0.0.0",
			why: []string{"0.0.0.0", "unspecified address", "pods and the nodes' containerd resolve every local cluster's names"}},
		{name: "unspecified IPv6", address: "::", why: []string{"::", "unspecified address"}},
		{name: "loopback", address: "127.0.0.1",
			why: []string{"127.0.0.1", "loopback address", "pods and the nodes' containerd resolve every local cluster's names", "reach themselves"}},
		{name: "another loopback address", address: "127.8.144.1", why: []string{"127.8.144.1", "loopback address"}},
		{name: "IPv6 loopback", address: "::1", why: []string{"::1", "loopback address"}},
		{name: "on no host interface", address: "10.10.10.9",
			why: []string{"10.10.10.9", "on no network interface of this host", "reaches nothing"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := CheckRouterAddress(tc.address, hostInterfaces)

			wantErrorMentioning(t, err, tc.why...)
		})
	}
}
