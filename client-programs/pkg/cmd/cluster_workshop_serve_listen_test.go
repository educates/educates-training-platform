package cmd

import (
	"errors"
	"io/fs"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/educates/educates-training-platform/client-programs/pkg/config"
	"github.com/educates/educates-training-platform/client-programs/pkg/localclusters"
)

// serveFlagDefaults is where cluster workshop serve listens when no flag
// is given and it serves no local cluster.
var serveFlagDefaults = serveListen{LocalHost: "0.0.0.0", LocalPort: 10081, ProxyPort: 10081, HugoPort: 1313}

// givenFlags returns a function reporting whether a flag is among names,
// as cobra's Changed does for the flags given on the command line.
func givenFlags(names ...string) func(string) bool {
	return func(flag string) bool {
		for _, n := range names {
			if n == flag {
				return true
			}
		}
		return false
	}
}

// routerAddressOf returns a function giving the router address address
// of the data home dataHome, and failing the test t when asked for the
// router address of another data home.
func routerAddressOf(t *testing.T, dataHome, address string) func(string) (string, error) {
	return func(home string) (string, error) {
		if home != dataHome {
			t.Errorf("the router address of the data home %s was read, want that of %s", home, dataHome)
		}
		return address, nil
	}
}

// freePortsOn returns a function giving a free port on each host, as
// byHost holds them, and failing the test t when asked for a free port on
// a host it does not hold.
func freePortsOn(t *testing.T, byHost map[string]int) func(string) (int, error) {
	return func(host string) (int, error) {
		port, ok := byHost[host]
		if !ok {
			t.Errorf("a free port was chosen on %s", host)
		}
		return port, nil
	}
}

// routerAddressNotRead is a router address that fails the test t when it
// is read.
func routerAddressNotRead(t *testing.T) func(string) (string, error) {
	return func(string) (string, error) {
		t.Error("the router address was read")
		return "", nil
	}
}

// serveDataHome returns a data home holding the named local cluster
// test2, whose kind cluster is an Educates cluster.
func serveDataHome(t *testing.T) string {
	t.Helper()
	dataHome := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dataHome, "clusters", "test2"), 0o755); err != nil {
		t.Fatal(err)
	}
	return dataHome
}

// contextTarget is a serve that selects no local cluster and reaches its
// cluster through the kube context kubeContext, of the data home
// dataHome.
func contextTarget(dataHome, kubeContext string) serveTarget {
	return serveTarget{dataHome: dataHome, kubeContext: func() string { return kubeContext }}
}

// onTheRouter is where serve listens for a local cluster when no flag is
// given, the router address being 10.10.10.1: the proxy on the free port
// 53124 there, which the workshop is patched with, and the Hugo server on
// the free loopback port 53123.
var onTheRouter = serveListen{LocalHost: "10.10.10.1", LocalPort: 53124, ProxyPort: 53124, HugoPort: 53123}

// routerFreePorts are the free ports of onTheRouter, by the host they are
// free on.
var routerFreePorts = map[string]int{"10.10.10.1": 53124, "127.0.0.1": 53123}

func TestServeListenServesTheLocalClusterItTargets(t *testing.T) {
	dataHome := serveDataHome(t)
	test2, err := localclusters.Named("test2", dataHome)
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name   string
		target serveTarget
		local  bool
	}{
		{
			name: "a selected cluster, whatever the kube context",
			target: serveTarget{selected: test2, isSelected: true, dataHome: dataHome,
				kubeContext: func() string { t.Error("the kube context was read"); return "kind-educates" }},
			local: true,
		},
		{name: "the default cluster's context", target: contextTarget(dataHome, "kind-educates"), local: true},
		{name: "a named cluster's context", target: contextTarget(dataHome, "kind-educates-test2"), local: true},
		{name: "a kind cluster's context that is not an Educates cluster", target: contextTarget(dataHome, "kind-educates-x")},
		{name: "another kind cluster's context", target: contextTarget(dataHome, "kind-kind")},
		{name: "a remote context", target: contextTarget(dataHome, "gke_project_zone_academy")},
		{name: "no context", target: contextTarget(dataHome, "")},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			routerAddress, freePorts, want := routerAddressNotRead(t), freePortsOn(t, nil), serveFlagDefaults
			if tc.local {
				routerAddress, freePorts, want = routerAddressOf(t, dataHome, "10.10.10.1"), freePortsOn(t, routerFreePorts), onTheRouter
			}
			got, err := chooseServeListen(serveFlagDefaults, givenFlags(), tc.target, routerAddress, freePorts)
			if err != nil {
				t.Fatalf("chooseServeListen: %v", err)
			}
			if got != want {
				t.Errorf("chooseServeListen = %+v, want %+v", got, want)
			}
		})
	}
}

func TestServeListenOnTheRouterAddress(t *testing.T) {
	cases := []struct {
		name          string
		routerAddress string
		wantLocalHost string
	}{
		{name: "an IPv4 address", routerAddress: "192.168.1.10", wantLocalHost: "192.168.1.10"},
		{name: "an IPv6 address, bracketed", routerAddress: "fd00::1", wantLocalHost: "[fd00::1]"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dataHome := t.TempDir()
			got, err := chooseServeListen(serveFlagDefaults, givenFlags(), contextTarget(dataHome, "kind-educates"),
				routerAddressOf(t, dataHome, tc.routerAddress),
				freePortsOn(t, map[string]int{tc.wantLocalHost: 53124, "127.0.0.1": 53123}))
			if err != nil {
				t.Fatalf("chooseServeListen: %v", err)
			}
			want := serveListen{LocalHost: tc.wantLocalHost, LocalPort: 53124, ProxyPort: 53124, HugoPort: 53123}
			if got != want {
				t.Errorf("chooseServeListen with router address %s = %+v, want %+v", tc.routerAddress, got, want)
			}
		})
	}
}

func TestServeListenErrorsNameTheFlagToGive(t *testing.T) {
	cause := errors.New("cause")
	failingAddress := func(string) (string, error) { return "", cause }
	failingOn := func(failing string) func(string) (int, error) {
		return func(host string) (int, error) {
			if host == failing {
				return 0, cause
			}
			return 53123, nil
		}
	}
	cases := []struct {
		name          string
		routerAddress func(string) (string, error)
		freePort      func(string) (int, error)
		wantFlag      string
	}{
		{name: "the router address", routerAddress: failingAddress, freePort: failingOn(""), wantFlag: "--local-host"},
		{name: "a free port for the proxy", routerAddress: func(string) (string, error) { return "10.10.10.1", nil },
			freePort: failingOn("10.10.10.1"), wantFlag: "--local-port"},
		{name: "a free port for the Hugo server", routerAddress: func(string) (string, error) { return "10.10.10.1", nil },
			freePort: failingOn("127.0.0.1"), wantFlag: "--hugo-port"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := chooseServeListen(serveFlagDefaults, givenFlags(), contextTarget(t.TempDir(), "kind-educates"),
				tc.routerAddress, tc.freePort)
			if !errors.Is(err, cause) {
				t.Fatalf("chooseServeListen error = %v, want it to wrap %v", err, cause)
			}
			if !strings.Contains(err.Error(), tc.wantFlag) {
				t.Errorf("chooseServeListen error %q does not name %s", err, tc.wantFlag)
			}
		})
	}
}

func TestServeListenFlagsGivenWin(t *testing.T) {
	contextNotRead := serveTarget{dataHome: t.TempDir(), kubeContext: func() string {
		t.Error("the kube context was read")
		return ""
	}}
	cases := []struct {
		name          string
		flags         serveListen
		given         []string
		target        serveTarget
		routerAddress func(*testing.T, string) func(string) (string, error)
		freePorts     map[string]int
		want          serveListen
	}{
		{
			name:          "--local-host, with the free port for the proxy chosen on it",
			flags:         serveListen{LocalHost: "192.168.1.20", LocalPort: 10081, ProxyPort: 10081, HugoPort: 1313},
			given:         []string{"local-host"},
			routerAddress: func(t *testing.T, _ string) func(string) (string, error) { return routerAddressNotRead(t) },
			freePorts:     map[string]int{"192.168.1.20": 53124, "127.0.0.1": 53123},
			want:          serveListen{LocalHost: "192.168.1.20", LocalPort: 53124, ProxyPort: 53124, HugoPort: 53123},
		},
		{
			name:      "--local-port, which the workshop is patched with",
			flags:     serveListen{LocalHost: "0.0.0.0", LocalPort: 10082, ProxyPort: 10081, HugoPort: 1313},
			given:     []string{"local-port"},
			freePorts: map[string]int{"127.0.0.1": 53123},
			want:      serveListen{LocalHost: "10.10.10.1", LocalPort: 10082, ProxyPort: 10082, HugoPort: 53123},
		},
		{
			name:      "--proxy-port",
			flags:     serveListen{LocalHost: "0.0.0.0", LocalPort: 10081, ProxyPort: 10090, HugoPort: 1313},
			given:     []string{"proxy-port"},
			freePorts: routerFreePorts,
			want:      serveListen{LocalHost: "10.10.10.1", LocalPort: 53124, ProxyPort: 10090, HugoPort: 53123},
		},
		{
			name:      "--hugo-port",
			flags:     serveListen{LocalHost: "0.0.0.0", LocalPort: 10081, ProxyPort: 10081, HugoPort: 1314},
			given:     []string{"hugo-port"},
			freePorts: map[string]int{"10.10.10.1": 53124},
			want:      serveListen{LocalHost: "10.10.10.1", LocalPort: 53124, ProxyPort: 53124, HugoPort: 1314},
		},
		{
			name:          "--local-host, --local-port and --hugo-port, with the workshop patched with the local port",
			flags:         serveListen{LocalHost: "192.168.1.20", LocalPort: 10082, ProxyPort: 10081, HugoPort: 1314},
			given:         []string{"local-host", "local-port", "hugo-port"},
			routerAddress: func(t *testing.T, _ string) func(string) (string, error) { return routerAddressNotRead(t) },
			want:          serveListen{LocalHost: "192.168.1.20", LocalPort: 10082, ProxyPort: 10082, HugoPort: 1314},
		},
		{
			name:          "every flag, which leaves the kube context unread",
			flags:         serveListen{LocalHost: "192.168.1.20", LocalPort: 10082, ProxyPort: 10083, HugoPort: 1314},
			given:         []string{"local-host", "local-port", "proxy-port", "hugo-port"},
			target:        contextNotRead,
			routerAddress: func(t *testing.T, _ string) func(string) (string, error) { return routerAddressNotRead(t) },
			want:          serveListen{LocalHost: "192.168.1.20", LocalPort: 10082, ProxyPort: 10083, HugoPort: 1314},
		},
		{
			name:          "--local-port for a cluster that is no local cluster, as before",
			flags:         serveListen{LocalHost: "0.0.0.0", LocalPort: 10082, ProxyPort: 10081, HugoPort: 1313},
			given:         []string{"local-port"},
			target:        contextTarget(t.TempDir(), "gke_project_zone_academy"),
			routerAddress: func(t *testing.T, _ string) func(string) (string, error) { return routerAddressNotRead(t) },
			want:          serveListen{LocalHost: "0.0.0.0", LocalPort: 10082, ProxyPort: 10081, HugoPort: 1313},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			target := tc.target
			if target.kubeContext == nil {
				target = contextTarget(t.TempDir(), "kind-educates")
			}
			routerAddress := routerAddressOf(t, target.dataHome, "10.10.10.1")
			if tc.routerAddress != nil {
				routerAddress = tc.routerAddress(t, target.dataHome)
			}
			got, err := chooseServeListen(tc.flags, givenFlags(tc.given...), target, routerAddress, freePortsOn(t, tc.freePorts))
			if err != nil {
				t.Fatalf("chooseServeListen: %v", err)
			}
			if got != tc.want {
				t.Errorf("chooseServeListen with %v given = %+v, want %+v", tc.given, got, tc.want)
			}
		})
	}
}

func TestFreePortOnIsOneTheProxyCanListenOn(t *testing.T) {
	port, err := freePortOn("127.0.0.1")
	if err != nil {
		t.Fatalf("freePortOn: %v", err)
	}
	l, err := net.Listen("tcp", "127.0.0.1:"+strconv.Itoa(port))
	if err != nil {
		t.Fatalf("listen on the free port %d: %v", port, err)
	}
	l.Close()
}

func TestFreePortOnAnAddressOfNoInterfaceIsAnError(t *testing.T) {
	// 192.0.2.1 is in TEST-NET-1, kept for documentation, which no
	// interface of a host running the tests has.
	if _, err := freePortOn("192.0.2.1"); err == nil {
		t.Error("freePortOn 192.0.2.1 found a free port, want an error")
	}
}

func TestHostRouterAddressChecksTheRouterFileAgainstTheHostInterfaces(t *testing.T) {
	t.Run("a missing router file", func(t *testing.T) {
		if _, err := hostRouterAddress(t.TempDir()); !errors.Is(err, fs.ErrNotExist) {
			t.Errorf("hostRouterAddress error = %v, want it to wrap fs.ErrNotExist", err)
		}
	})
	t.Run("an address of no interface", func(t *testing.T) {
		dataHome := t.TempDir()
		if _, err := config.CreateLocalRouter(dataHome, "192.0.2.1", false); err != nil {
			t.Fatal(err)
		}
		_, err := hostRouterAddress(dataHome)
		if err == nil || !strings.Contains(err.Error(), "no network interface") {
			t.Errorf("hostRouterAddress error = %v, want one saying 192.0.2.1 is on no network interface", err)
		}
	})
}
