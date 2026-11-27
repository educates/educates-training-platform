package localclusters

import (
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/educates/educates-training-platform/client-programs/pkg/config"
)

// statusRowSummary is what a test compares of a router status row: its
// name, its cluster by kind name, how its TLS is served and the cluster's
// state.
type statusRowSummary struct {
	Domain       string
	KindName     string
	TLS          RouterTLS
	ClusterState State
}

func wantStatusRows(t *testing.T, got []RouterStatusRow, want ...statusRowSummary) {
	t.Helper()
	summaries := make([]statusRowSummary, 0, len(got))
	for _, r := range got {
		summaries = append(summaries, statusRowSummary{Domain: r.Domain, KindName: r.Cluster.KindName, TLS: r.TLS, ClusterState: r.ClusterState})
	}
	if !reflect.DeepEqual(summaries, want) {
		t.Errorf("status rows:\n got  %+v\n want %+v", summaries, want)
	}
}

// routerStatusDataHome is a data home whose router address is 10.10.10.1,
// with a default local cluster on educates.test, which also serves
// extra.example.test, and a named local cluster test2 on
// test2.educates.test.
func routerStatusDataHome(t *testing.T) string {
	t.Helper()
	dataHome := t.TempDir()
	routerFile(t, dataHome, routerHeader+"address: 10.10.10.1\n")
	writeClusterConfig(t, dataHome, "", localConfigHeader+"ingress:\n  domain: educates.test\nresolver:\n  extraDomains:\n  - extra.example.test\n")
	writeClusterConfig(t, dataHome, "test2", localConfigHeader+"ingress:\n  domain: test2.educates.test\n")
	return dataHome
}

// routerStatus returns the router status of the data home dataHome, on a
// host with the interfaces of hostInterfaces, the default local cluster
// running and the named local cluster test2 stopped, and router the
// router container observed, nil for none.
func routerStatus(t *testing.T, dataHome string, router *RouterContainer) RouterStatus {
	t.Helper()
	inv := NewInventory(Observed{
		Nodes:              []NodeContainer{kindControlPlane("educates", true), kindControlPlane("educates-test2", false)},
		InterfaceAddresses: hostInterfaces,
		Router:             router,
	}, dataHome)
	status, err := inv.RouterStatus(pinnedRouterImage)
	if err != nil {
		t.Fatalf("RouterStatus: %v", err)
	}
	return status
}

// The status shows the router ports the router publishes. A router
// created before the router served the local clusters' API servers
// publishes ports 80 and 443 only, and has to be re-created.
func TestRouterStatusShowsThePublishedPortsAndAdvisesReCreatingARouterWithoutPort6443(t *testing.T) {
	dataHome := routerStatusDataHome(t)

	current := routerStatus(t, dataHome, routerContainer(pinnedRouterImage, "10.10.10.1", true))
	old := routerStatus(t, dataHome, ingressOnlyRouter("10.10.10.1"))

	if want := []uint16{80, 443, 6443}; !reflect.DeepEqual(current.PublishedPorts, want) || current.Advice != "" {
		t.Errorf("router publishing every port: ports %v, advice %q; want %v and no advice",
			current.PublishedPorts, current.Advice, want)
	}
	if want := []uint16{80, 443}; !reflect.DeepEqual(old.PublishedPorts, want) {
		t.Errorf("router without port 6443: ports %v, want %v", old.PublishedPorts, want)
	}
	for _, part := range []string{"re-created", "port 6443", "educates local router update"} {
		if !strings.Contains(old.Advice, part) {
			t.Errorf("advice %q does not mention %q", old.Advice, part)
		}
	}
}

func TestRouterStatusListsEveryNameOfEveryClusterWithTheClusterState(t *testing.T) {
	dataHome := routerStatusDataHome(t)

	status := routerStatus(t, dataHome, routerContainer(pinnedRouterImage, "10.10.10.1", true))

	wantStatusRows(t, status.Routes,
		statusRowSummary{"educates.test", "educates", RouterTLSPassthrough, Running},
		statusRowSummary{"extra.example.test", "educates", RouterTLSPassthrough, Running},
		statusRowSummary{"test2.educates.test", "educates-test2", RouterTLSPassthrough, Stopped},
	)
	if status.Address != "10.10.10.1" || status.AddressError != nil {
		t.Errorf("router address = %q, %v; want 10.10.10.1 and no error", status.Address, status.AddressError)
	}
	if status.State != RouterRunning || status.Image != pinnedRouterImage || status.PublishedAddress != "10.10.10.1" {
		t.Errorf("router = %s, image %q, published on %q; want running, the pinned image, on 10.10.10.1",
			status.State, status.Image, status.PublishedAddress)
	}
	if status.Advice != "" {
		t.Errorf("advice = %q, want none for a router serving as it should", status.Advice)
	}
}

func TestRouterStatusSaysWhenTheRouterIsNotRunningAndHowToFixIt(t *testing.T) {
	for _, tc := range []struct {
		name       string
		router     *RouterContainer
		wantState  RouterState
		wantAdvice []string
	}{
		{
			name:       "no router",
			wantState:  RouterMissing,
			wantAdvice: []string{"educates local router update", "creates it"},
		},
		{
			name:       "a stopped router",
			router:     routerContainer(pinnedRouterImage, "10.10.10.1", false),
			wantState:  RouterStopped,
			wantAdvice: []string{"educates local router update", "re-creates it", "not running"},
		},
		{
			name:       "a router on another image",
			router:     routerContainer("haproxy:3.4.7-alpine", "10.10.10.1", true),
			wantState:  RouterRunning,
			wantAdvice: []string{"educates local router update", "re-creates it", "haproxy:3.4.7-alpine", pinnedRouterImage},
		},
		{
			name:       "a router on another address",
			router:     routerContainer(pinnedRouterImage, "10.10.10.2", true),
			wantState:  RouterRunning,
			wantAdvice: []string{"educates local router update", "re-creates it", "10.10.10.2", "10.10.10.1"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			status := routerStatus(t, routerStatusDataHome(t), tc.router)

			if status.State != tc.wantState {
				t.Errorf("state = %q, want %q", status.State, tc.wantState)
			}
			for _, part := range tc.wantAdvice {
				if !strings.Contains(status.Advice, part) {
					t.Errorf("advice %q does not mention %q", status.Advice, part)
				}
			}
		})
	}
}

func TestRouterStatusWithNoClusterSaysTheFirstClusterCreatesTheRouter(t *testing.T) {
	dataHome := t.TempDir()
	routerFile(t, dataHome, routerHeader+"address: 10.10.10.1\n")

	status, err := NewInventory(Observed{InterfaceAddresses: hostInterfaces}, dataHome).RouterStatus(pinnedRouterImage)
	if err != nil {
		t.Fatalf("RouterStatus: %v", err)
	}

	if status.State != RouterMissing || status.Image != pinnedRouterImage || len(status.Routes) != 0 {
		t.Errorf("status = %+v, want a missing router on the pinned image and no route", status)
	}
	for _, part := range []string{"no local cluster", "educates local cluster create"} {
		if !strings.Contains(status.Advice, part) {
			t.Errorf("advice %q does not mention %q", status.Advice, part)
		}
	}
}

func TestRouterAddressIsTheAddressOfTheRouterFile(t *testing.T) {
	dataHome := t.TempDir()
	routerFile(t, dataHome, routerHeader+"address: ::ffff:10.10.10.1\n")
	inv := NewInventory(Observed{}, dataHome)

	if address, err := inv.RouterAddress(); address != "10.10.10.1" || err != nil {
		t.Errorf("RouterAddress() = %q, %v; want 10.10.10.1, nil", address, err)
	}

	if err := os.Remove(config.LocalRouterPath(dataHome)); err != nil {
		t.Fatal(err)
	}
	address, err := inv.RouterAddress()
	if address != "" {
		t.Errorf("RouterAddress() = %q without a router file, want none", address)
	}
	wantErrorMentioning(t, err, "router.yaml", "educates local router update --address <ip>")
}

func TestRouterStatusSaysWhyTheRouterAddressCannotServe(t *testing.T) {
	t.Run("no router file", func(t *testing.T) {
		dataHome := routerStatusDataHome(t)
		if err := os.Remove(config.LocalRouterPath(dataHome)); err != nil {
			t.Fatal(err)
		}

		status := routerStatus(t, dataHome, routerContainer(pinnedRouterImage, "10.10.10.1", false))

		if status.Address != "" || status.Advice != "" {
			t.Errorf("address %q, advice %q; want neither without a router address", status.Address, status.Advice)
		}
		wantErrorMentioning(t, status.AddressError, "router.yaml", "educates local router update --address <ip>")
		if len(status.Routes) != 3 {
			t.Errorf("routes = %+v, want the 3 names of the clusters", status.Routes)
		}
	})

	t.Run("an address on no interface", func(t *testing.T) {
		dataHome := routerStatusDataHome(t)
		routerFile(t, dataHome, routerHeader+"address: 10.10.10.9\n")

		status := routerStatus(t, dataHome, routerContainer(pinnedRouterImage, "10.10.10.1", true))

		if status.Address != "10.10.10.9" || status.Advice != "" {
			t.Errorf("address %q, advice %q; want 10.10.10.9, and no advice to re-create the router on it",
				status.Address, status.Advice)
		}
		wantErrorMentioning(t, status.AddressError, "10.10.10.9", "no network interface", "educates local router update --address <ip>")
	})
}
