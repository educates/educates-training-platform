package localclusters

import "testing"

func TestResolverAddress(t *testing.T) {
	cases := []struct {
		name          string
		listenAddress string
		hostIP        string
		wantAddress   string
		wantSource    AddressSource
	}{
		{name: "a specific IPv4 listen address", listenAddress: "10.10.10.2", hostIP: "192.168.1.10",
			wantAddress: "10.10.10.2", wantSource: AddressFromListenAddress},
		{name: "a specific IPv6 listen address", listenAddress: "fd00::2", hostIP: "192.168.1.10",
			wantAddress: "fd00::2", wantSource: AddressFromListenAddress},
		{name: "a specific listen address with no host IP known", listenAddress: "10.10.10.2",
			wantAddress: "10.10.10.2", wantSource: AddressFromListenAddress},
		{name: "a loopback listen address", listenAddress: "127.0.0.1", hostIP: "192.168.1.10",
			wantAddress: "192.168.1.10", wantSource: AddressFromHostIP},
		{name: "another loopback listen address", listenAddress: "127.0.0.2", hostIP: "192.168.1.10",
			wantAddress: "192.168.1.10", wantSource: AddressFromHostIP},
		{name: "every host address", listenAddress: "0.0.0.0", hostIP: "192.168.1.10",
			wantAddress: "192.168.1.10", wantSource: AddressFromHostIP},
		{name: "no listen address", hostIP: "192.168.1.10",
			wantAddress: "192.168.1.10", wantSource: AddressFromHostIP},
		{name: "a listen address that is no IP", listenAddress: "localhost", hostIP: "192.168.1.10",
			wantAddress: "192.168.1.10", wantSource: AddressFromHostIP},
		{name: "a loopback listen address with no host IP known", listenAddress: "127.0.0.1",
			wantAddress: "127.0.0.1", wantSource: AddressFromLoopback},
		{name: "no listen address and no host IP known",
			wantAddress: "127.0.0.1", wantSource: AddressFromLoopback},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			address, source := resolverAddress(tc.listenAddress, tc.hostIP)

			if address != tc.wantAddress || source != tc.wantSource {
				t.Errorf("resolverAddress(%q, %q) = %q, %v; want %q, %v",
					tc.listenAddress, tc.hostIP, address, source, tc.wantAddress, tc.wantSource)
			}
		})
	}
}
