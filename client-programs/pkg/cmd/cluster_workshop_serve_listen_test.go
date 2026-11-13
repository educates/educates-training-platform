package cmd

import (
	"errors"
	"strings"
	"testing"
)

// serveFlagDefaults is where cluster workshop serve listens when no flag
// is given and no local cluster is selected.
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

// listenAddressOf returns a function giving the selected cluster's listen
// address addr.
func listenAddressOf(addr string) func() (string, error) {
	return func() (string, error) { return addr, nil }
}

// freePortOf returns a function giving the free port port.
func freePortOf(port int) func() (int, error) {
	return func() (int, error) { return port, nil }
}

// notConsulted returns functions for the selected cluster's listen address
// and a free port that fail the test t when called.
func notConsulted(t *testing.T) (func() (string, error), func() (int, error)) {
	return func() (string, error) {
			t.Error("the selected cluster's listen address was read")
			return "", nil
		}, func() (int, error) {
			t.Error("a free port was chosen")
			return 0, nil
		}
}

func TestServeListenWithNoClusterSelected(t *testing.T) {
	cases := []struct {
		name  string
		flags serveListen
		given []string
	}{
		{name: "keeps the defaults", flags: serveFlagDefaults},
		{
			name:  "keeps the flags given",
			flags: serveListen{LocalHost: "127.0.0.1", LocalPort: 10082, HugoPort: 1314},
			given: []string{"local-host", "local-port", "hugo-port"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			listenAddress, freePort := notConsulted(t)
			got, err := chooseServeListen(tc.flags, givenFlags(tc.given...), false, listenAddress, freePort)
			if err != nil {
				t.Fatalf("chooseServeListen: %v", err)
			}
			if got != tc.flags {
				t.Errorf("chooseServeListen = %+v, want the flags %+v", got, tc.flags)
			}
		})
	}
}

func TestServeListenForSelectedCluster(t *testing.T) {
	cases := []struct {
		name          string
		listenAddress string
		wantLocalHost string
	}{
		{name: "a specific IPv4 address", listenAddress: "10.10.10.2", wantLocalHost: "10.10.10.2"},
		{name: "a specific IPv6 address, bracketed", listenAddress: "fd00::2", wantLocalHost: "[fd00::2]"},
		{name: "an IPv4-mapped IPv6 address as IPv4", listenAddress: "::ffff:10.10.10.2", wantLocalHost: "10.10.10.2"},
		{name: "a loopback address keeps every address", listenAddress: "127.0.0.1", wantLocalHost: "0.0.0.0"},
		{name: "another loopback address keeps every address", listenAddress: "127.0.0.2", wantLocalHost: "0.0.0.0"},
		{name: "the IPv6 loopback address keeps every address", listenAddress: "::1", wantLocalHost: "0.0.0.0"},
		{name: "every IPv4 address keeps every address", listenAddress: "0.0.0.0", wantLocalHost: "0.0.0.0"},
		{name: "every IPv6 address keeps every address", listenAddress: "::", wantLocalHost: "0.0.0.0"},
		{name: "no listen address keeps every address", listenAddress: "", wantLocalHost: "0.0.0.0"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := chooseServeListen(serveFlagDefaults, givenFlags(), true, listenAddressOf(tc.listenAddress), freePortOf(53123))
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
	failingAddress := func() (string, error) { return "", cause }
	failingPort := func() (int, error) { return 0, cause }
	cases := []struct {
		name          string
		listenAddress func() (string, error)
		freePort      func() (int, error)
		wantFlag      string
	}{
		{name: "the listen address", listenAddress: failingAddress, freePort: freePortOf(53123), wantFlag: "--local-host"},
		{name: "a free port", listenAddress: listenAddressOf("10.10.10.2"), freePort: failingPort, wantFlag: "--hugo-port"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := chooseServeListen(serveFlagDefaults, givenFlags(), true, tc.listenAddress, tc.freePort)
			if !errors.Is(err, cause) {
				t.Fatalf("chooseServeListen error = %v, want it to wrap %v", err, cause)
			}
			if !strings.Contains(err.Error(), tc.wantFlag) {
				t.Errorf("chooseServeListen error %q does not name %s", err, tc.wantFlag)
			}
		})
	}
}

func TestServeListenFlagsGivenWinForSelectedCluster(t *testing.T) {
	notRead, notChosen := notConsulted(t)
	cases := []struct {
		name          string
		flags         serveListen
		given         []string
		listenAddress func() (string, error)
		freePort      func() (int, error)
		want          serveListen
	}{
		{
			name:          "--local-host",
			flags:         serveListen{LocalHost: "0.0.0.0", LocalPort: 10081, HugoPort: 1313},
			given:         []string{"local-host"},
			listenAddress: notRead,
			freePort:      freePortOf(53123),
			want:          serveListen{LocalHost: "0.0.0.0", LocalPort: 10081, HugoPort: 53123},
		},
		{
			name:          "--hugo-port",
			flags:         serveListen{LocalHost: "0.0.0.0", LocalPort: 10081, HugoPort: 1313},
			given:         []string{"hugo-port"},
			listenAddress: listenAddressOf("10.10.10.2"),
			freePort:      notChosen,
			want:          serveListen{LocalHost: "10.10.10.2", LocalPort: 10081, HugoPort: 1313},
		},
		{
			name:          "--local-port",
			flags:         serveListen{LocalHost: "0.0.0.0", LocalPort: 10082, HugoPort: 1313},
			given:         []string{"local-port"},
			listenAddress: listenAddressOf("10.10.10.2"),
			freePort:      freePortOf(53123),
			want:          serveListen{LocalHost: "10.10.10.2", LocalPort: 10082, HugoPort: 53123},
		},
		{
			name:          "all three",
			flags:         serveListen{LocalHost: "192.168.1.10", LocalPort: 10082, HugoPort: 1314},
			given:         []string{"local-host", "local-port", "hugo-port"},
			listenAddress: notRead,
			freePort:      notChosen,
			want:          serveListen{LocalHost: "192.168.1.10", LocalPort: 10082, HugoPort: 1314},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := chooseServeListen(tc.flags, givenFlags(tc.given...), true, tc.listenAddress, tc.freePort)
			if err != nil {
				t.Fatalf("chooseServeListen: %v", err)
			}
			if got != tc.want {
				t.Errorf("chooseServeListen with %v given = %+v, want %+v", tc.given, got, tc.want)
			}
		})
	}
}
