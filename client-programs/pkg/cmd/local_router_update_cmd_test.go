package cmd

import (
	"bytes"
	"context"
	"errors"
	"net/netip"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/educates/educates-training-platform/client-programs/pkg/config"
	"github.com/educates/educates-training-platform/client-programs/pkg/deployer/progress"
	"github.com/educates/educates-training-platform/client-programs/pkg/localclusters"
	"github.com/educates/educates-training-platform/client-programs/pkg/router"
)

// updateHostInterfaces are the interface addresses of a host with the
// aliases 10.10.10.1 and 10.10.10.2 added.
var updateHostInterfaces = []netip.Addr{
	netip.MustParseAddr("127.0.0.1"),
	netip.MustParseAddr("192.168.1.20"),
	netip.MustParseAddr("10.10.10.1"),
	netip.MustParseAddr("10.10.10.2"),
}

// updateInventory is the inventory of dataHome on a host with the
// interfaces of updateHostInterfaces, with the node containers nodes,
// and router the router container observed, nil for none.
func updateInventory(dataHome string, nodes []localclusters.NodeContainer, router *localclusters.RouterContainer) localclusters.Inventory {
	return localclusters.NewInventory(localclusters.Observed{
		Nodes:              nodes,
		InterfaceAddresses: updateHostInterfaces,
		Router:             router,
	}, dataHome)
}

// runningDefaultCluster is the node container of the default local
// cluster, running.
var runningDefaultCluster = []localclusters.NodeContainer{
	{Name: "educates-control-plane", KindCluster: "educates", Role: "control-plane", Running: true},
}

// routerUpdateResult is what local router update did.
type routerUpdateResult struct {
	err               error
	out               string
	resolverRefreshed bool
}

// updateRouterForTest runs local router update against the fake router
// container f of dataHome, with inv the inventory observed and address
// the value of --address.
func updateRouterForTest(f *fakeRouterContainer, dataHome string, inv localclusters.Inventory, address string) routerUpdateResult {
	var out bytes.Buffer
	var result routerUpdateResult
	result.err = routerChangeForTest(f, dataHome, nil).update(context.Background(), &out, progress.New(&out, 0, false), inv, address,
		func(progress.Reporter) { result.resolverRefreshed = true })
	result.out = out.String()
	return result
}

func TestLocalRouterUpdateReloadsARouterThatNeedsNoChange(t *testing.T) {
	dataHome := routerDataHome(t)
	f := &fakeRouterContainer{}

	result := updateRouterForTest(f, dataHome, updateInventory(dataHome, runningDefaultCluster, routerOn(router.Image, true)), "")

	if result.err != nil {
		t.Fatalf("update: %v", result.err)
	}
	if want := []string{"join", "reload"}; !slices.Equal(f.calls, want) {
		t.Errorf("calls = %q, want %q: a reload, which keeps open connections", f.calls, want)
	}
	if !slices.Contains(f.folder, "503-educates.txt") {
		t.Errorf("router folder held %q when reloaded, want the default cluster's route", f.folder)
	}
	if result.resolverRefreshed {
		t.Errorf("the resolver was refreshed with the router address unchanged")
	}
	wantRouterFile(t, dataHome, handWrittenRouterFile)
}

func TestLocalRouterUpdateFailsWhenTheRouterRefusesItsConfiguration(t *testing.T) {
	dataHome := routerDataHome(t)
	refused := errors.New("the router refuses its configuration")
	f := &fakeRouterContainer{reloadErr: refused}

	result := updateRouterForTest(f, dataHome, updateInventory(dataHome, runningDefaultCluster, routerOn(router.Image, true)), "")

	if !errors.Is(result.err, refused) {
		t.Errorf("update() = %v, want the reload's error", result.err)
	}
}

func TestLocalRouterUpdateRecreatesARouterOnAnotherImage(t *testing.T) {
	dataHome := routerDataHome(t)
	f := &fakeRouterContainer{}

	result := updateRouterForTest(f, dataHome, updateInventory(dataHome, runningDefaultCluster, routerOn("haproxy:3.4.7-alpine", true)), "")

	if result.err != nil {
		t.Fatalf("update: %v", result.err)
	}
	if want := []string{"remove", "create 10.10.10.1 " + config.LocalRouterDir(dataHome)}; !slices.Equal(f.calls, want) {
		t.Errorf("calls = %q, want %q", f.calls, want)
	}
}

func TestLocalRouterUpdateCreatesAMissingRouterWhileALocalClusterExists(t *testing.T) {
	dataHome := routerDataHome(t)
	stopped := []localclusters.NodeContainer{{Name: "educates-control-plane", KindCluster: "educates", Role: "control-plane"}}
	f := &fakeRouterContainer{}

	result := updateRouterForTest(f, dataHome, updateInventory(dataHome, stopped, nil), "")

	if result.err != nil {
		t.Fatalf("update: %v", result.err)
	}
	if want := []string{"create 10.10.10.1 " + config.LocalRouterDir(dataHome)}; !slices.Equal(f.calls, want) {
		t.Errorf("calls = %q, want %q", f.calls, want)
	}
}

func TestLocalRouterUpdateCreatesNoRouterWithoutALocalCluster(t *testing.T) {
	dataHome := routerDataHome(t)
	f := &fakeRouterContainer{}

	result := updateRouterForTest(f, dataHome, updateInventory(dataHome, nil, nil), "10.10.10.2")

	if result.err != nil {
		t.Fatalf("update: %v", result.err)
	}
	if len(f.calls) != 0 {
		t.Errorf("calls = %q, want none without a local cluster", f.calls)
	}
	for _, part := range []string{"no local cluster", "educates local cluster create"} {
		if !strings.Contains(result.out, part) {
			t.Errorf("update reported:\n%s\nwant it to mention %q", result.out, part)
		}
	}
	if got, err := localclusters.LoadRouterAddress(dataHome, updateHostInterfaces); err != nil || got != "10.10.10.2" {
		t.Errorf("router address = %q, %v; want 10.10.10.2 written", got, err)
	}
}

func TestLocalRouterUpdateWithAnAddressWritesItAndRecreatesTheRouterOnIt(t *testing.T) {
	dataHome := routerDataHome(t)
	f := &fakeRouterContainer{}

	result := updateRouterForTest(f, dataHome, updateInventory(dataHome, runningDefaultCluster, routerOn(router.Image, true)), "10.10.10.2")

	if result.err != nil {
		t.Fatalf("update: %v", result.err)
	}
	wantRouterFile(t, dataHome, strings.Replace(handWrittenRouterFile, "10.10.10.1", "10.10.10.2", 1))
	if want := []string{"remove", "create 10.10.10.2 " + config.LocalRouterDir(dataHome)}; !slices.Equal(f.calls, want) {
		t.Errorf("calls = %q, want %q: the router re-created on the new address", f.calls, want)
	}
	if !result.resolverRefreshed {
		t.Errorf("the resolver was not refreshed, so the clusters' names still resolve to the previous address")
	}
	for _, part := range []string{"10.10.10.2", "10.10.10.1"} {
		if !strings.Contains(result.out, part) {
			t.Errorf("update reported:\n%s\nwant it to mention %q", result.out, part)
		}
	}
}

func TestLocalRouterUpdateRefusesAnAddressTheRouterAddressRulesRefuse(t *testing.T) {
	for address, why := range map[string]string{
		"127.0.0.1":  "loopback address",
		"10.10.10.9": "on no network interface of this host",
	} {
		t.Run(address, func(t *testing.T) {
			dataHome := routerDataHome(t)
			f := &fakeRouterContainer{}

			result := updateRouterForTest(f, dataHome, updateInventory(dataHome, runningDefaultCluster, routerOn(router.Image, true)), address)

			wantError(t, result.err, address, why)
			wantRouterFile(t, dataHome, handWrittenRouterFile)
			if len(f.calls) != 0 || result.resolverRefreshed {
				t.Errorf("calls %q, resolver refreshed %t; want nothing done", f.calls, result.resolverRefreshed)
			}
		})
	}
}

// A cluster whose domain is the nip.io name of the router address takes
// the new address's name, which its portals answer on only once it is
// deployed again.
func TestLocalRouterUpdateWithAnAddressSaysWhichClustersToDeployAgain(t *testing.T) {
	dataHome := t.TempDir()
	writeRouterFile(t, dataHome, handWrittenRouterFile)
	writeLocalClusterListFile(t, filepath.Join(dataHome, "config.yaml"),
		"apiVersion: cli.educates.dev/v1alpha1\nkind: EducatesLocalConfig\n")
	writeLocalClusterListFile(t, filepath.Join(dataHome, "clusters", "test2", "config.yaml"),
		"apiVersion: cli.educates.dev/v1alpha1\nkind: EducatesLocalConfig\n"+
			"ingress:\n  domain: test2.educates.test\n")
	nodes := append(slices.Clone(runningDefaultCluster),
		localclusters.NodeContainer{Name: "educates-test2-control-plane", KindCluster: "educates-test2", Role: "control-plane", Running: true})
	f := &fakeRouterContainer{}

	result := updateRouterForTest(f, dataHome, updateInventory(dataHome, nodes, routerOn(router.Image, true)), "10.10.10.2")

	if result.err != nil {
		t.Fatalf("update: %v", result.err)
	}
	for _, part := range []string{"the default local cluster", "10-10-10-2.nip.io", "educates admin platform deploy --local-config"} {
		if !strings.Contains(result.out, part) {
			t.Errorf("update reported:\n%s\nwant it to mention %q", result.out, part)
		}
	}
	if strings.Contains(result.out, "test2") {
		t.Errorf("update reported:\n%s\nwant nothing about test2, whose domain is its own", result.out)
	}
}
