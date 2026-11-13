package cmd

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/educates/educates-training-platform/client-programs/pkg/localclusters"
)

// serveFlagDefaults is where cluster workshop serve listens when no flag
// is given and it serves no local cluster.
var serveFlagDefaults = serveListen{LocalHost: "0.0.0.0", LocalPort: 10081, HugoPort: 1313}

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

// listenAddresses returns a function giving the listen address of each
// local cluster, by name, as byName holds them, and failing the test t
// for a cluster it does not hold.
func listenAddresses(t *testing.T, byName map[string]string) func(localclusters.Identity) (string, error) {
	return func(id localclusters.Identity) (string, error) {
		addr, ok := byName[id.Name]
		if !ok {
			t.Errorf("the listen address of %s was read", id.Description())
		}
		return addr, nil
	}
}

// freePortOf returns a function giving the free port port.
func freePortOf(port int) func() (int, error) {
	return func() (int, error) { return port, nil }
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

func TestServeListenServesTheLocalClusterItTargets(t *testing.T) {
	dataHome := serveDataHome(t)
	test2, err := localclusters.Named("test2", dataHome)
	if err != nil {
		t.Fatal(err)
	}
	addresses := map[string]string{"": "10.10.10.1", "test2": "10.10.10.2"}
	targetsCluster := func(localHost string) serveListen {
		return serveListen{LocalHost: localHost, LocalPort: 10081, HugoPort: 53123}
	}
	cases := []struct {
		name   string
		target serveTarget
		want   serveListen
	}{
		{
			name: "a selected cluster, whatever the kube context",
			target: serveTarget{selected: test2, isSelected: true, dataHome: dataHome,
				kubeContext: func() string { t.Error("the kube context was read"); return "kind-educates" }},
			want: targetsCluster("10.10.10.2"),
		},
		{name: "the default cluster's context", target: contextTarget(dataHome, "kind-educates"), want: targetsCluster("10.10.10.1")},
		{name: "a named cluster's context", target: contextTarget(dataHome, "kind-educates-test2"), want: targetsCluster("10.10.10.2")},
		{name: "a kind cluster's context that is not an Educates cluster", target: contextTarget(dataHome, "kind-educates-x"), want: serveFlagDefaults},
		{name: "another kind cluster's context", target: contextTarget(dataHome, "kind-kind"), want: serveFlagDefaults},
		{name: "a remote context", target: contextTarget(dataHome, "gke_project_zone_academy"), want: serveFlagDefaults},
		{name: "no context", target: contextTarget(dataHome, ""), want: serveFlagDefaults},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := chooseServeListen(serveFlagDefaults, givenFlags(), tc.target, listenAddresses(t, addresses), freePortOf(53123))
			if err != nil {
				t.Fatalf("chooseServeListen: %v", err)
			}
			if got != tc.want {
				t.Errorf("chooseServeListen = %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestServeListenOnTheListenAddressOfTheClusterItTargets(t *testing.T) {
	cases := []struct {
		name          string
		listenAddress string
		wantLocalHost string
	}{
		{name: "a specific IPv4 address", listenAddress: "10.10.10.2", wantLocalHost: "10.10.10.2"},
		{name: "a specific IPv6 address, bracketed", listenAddress: "fd00::2", wantLocalHost: "[fd00::2]"},
		{name: "an IPv4-mapped IPv6 address as IPv4", listenAddress: "::ffff:10.10.10.2", wantLocalHost: "10.10.10.2"},
		{name: "no listen address, of a missing config, keeps every address", listenAddress: "", wantLocalHost: "0.0.0.0"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := chooseServeListen(serveFlagDefaults, givenFlags(), contextTarget(t.TempDir(), "kind-educates"),
				listenAddresses(t, map[string]string{"": tc.listenAddress}), freePortOf(53123))
			if err != nil {
				t.Fatalf("chooseServeListen: %v", err)
			}
			want := serveListen{LocalHost: tc.wantLocalHost, LocalPort: 10081, HugoPort: 53123}
			if got != want {
				t.Errorf("chooseServeListen with listen address %q = %+v, want %+v", tc.listenAddress, got, want)
			}
		})
	}
}

func TestServeListenErrorsNameTheFlagToGive(t *testing.T) {
	cause := errors.New("cause")
	failingAddress := func(localclusters.Identity) (string, error) { return "", cause }
	failingPort := func() (int, error) { return 0, cause }
	cases := []struct {
		name          string
		listenAddress func(localclusters.Identity) (string, error)
		freePort      func() (int, error)
		wantFlag      string
	}{
		{name: "the listen address", listenAddress: failingAddress, freePort: freePortOf(53123), wantFlag: "--local-host"},
		{name: "a free port", listenAddress: listenAddresses(t, map[string]string{"": "10.10.10.1"}), freePort: failingPort, wantFlag: "--hugo-port"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := chooseServeListen(serveFlagDefaults, givenFlags(), contextTarget(t.TempDir(), "kind-educates"),
				tc.listenAddress, tc.freePort)
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
	notRead := func(localclusters.Identity) (string, error) {
		t.Error("the listen address was read")
		return "", nil
	}
	notChosen := func() (int, error) {
		t.Error("a free port was chosen")
		return 0, nil
	}
	contextNotRead := serveTarget{dataHome: t.TempDir(), kubeContext: func() string {
		t.Error("the kube context was read")
		return ""
	}}
	cases := []struct {
		name          string
		flags         serveListen
		given         []string
		target        serveTarget
		listenAddress func(localclusters.Identity) (string, error)
		freePort      func() (int, error)
		want          serveListen
	}{
		{
			name:          "--local-host",
			flags:         serveListen{LocalHost: "0.0.0.0", LocalPort: 10081, HugoPort: 1313},
			given:         []string{"local-host"},
			target:        contextTarget(t.TempDir(), "kind-educates"),
			listenAddress: notRead,
			freePort:      freePortOf(53123),
			want:          serveListen{LocalHost: "0.0.0.0", LocalPort: 10081, HugoPort: 53123},
		},
		{
			name:          "--hugo-port",
			flags:         serveListen{LocalHost: "0.0.0.0", LocalPort: 10081, HugoPort: 1313},
			given:         []string{"hugo-port"},
			target:        contextTarget(t.TempDir(), "kind-educates"),
			listenAddress: listenAddresses(t, map[string]string{"": "10.10.10.1"}),
			freePort:      notChosen,
			want:          serveListen{LocalHost: "10.10.10.1", LocalPort: 10081, HugoPort: 1313},
		},
		{
			name:          "--local-port",
			flags:         serveListen{LocalHost: "0.0.0.0", LocalPort: 10082, HugoPort: 1313},
			given:         []string{"local-port"},
			target:        contextTarget(t.TempDir(), "kind-educates"),
			listenAddress: listenAddresses(t, map[string]string{"": "10.10.10.1"}),
			freePort:      freePortOf(53123),
			want:          serveListen{LocalHost: "10.10.10.1", LocalPort: 10082, HugoPort: 53123},
		},
		{
			name:          "--local-host and --hugo-port, which leave the kube context unread",
			flags:         serveListen{LocalHost: "192.168.1.10", LocalPort: 10082, HugoPort: 1314},
			given:         []string{"local-host", "local-port", "hugo-port"},
			target:        contextNotRead,
			listenAddress: notRead,
			freePort:      notChosen,
			want:          serveListen{LocalHost: "192.168.1.10", LocalPort: 10082, HugoPort: 1314},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := chooseServeListen(tc.flags, givenFlags(tc.given...), tc.target, tc.listenAddress, tc.freePort)
			if err != nil {
				t.Fatalf("chooseServeListen: %v", err)
			}
			if got != tc.want {
				t.Errorf("chooseServeListen with %v given = %+v, want %+v", tc.given, got, tc.want)
			}
		})
	}
}
