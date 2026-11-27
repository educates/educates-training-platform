package cmd

import (
	"bytes"
	"context"
	"errors"
	"net/netip"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/educates/educates-training-platform/client-programs/pkg/config"
	"github.com/educates/educates-training-platform/client-programs/pkg/deployer/progress"
	"github.com/educates/educates-training-platform/client-programs/pkg/localclusters"
	"github.com/educates/educates-training-platform/client-programs/pkg/router"
)

// fakeRouterContainer records what a local cluster command does to the
// router container, in order, and fails as its fields say.
type fakeRouterContainer struct {
	calls     []string
	createErr error
	reloadErr error
	adopted   bool

	// folder is what the router folder held when the router was created
	// or reloaded.
	folder []string
}

func (f *fakeRouterContainer) container(dataHome string) routerContainer {
	folder := func() []string {
		entries, _ := os.ReadDir(config.LocalRouterDir(dataHome))
		var names []string
		for _, e := range entries {
			names = append(names, e.Name())
		}
		return names
	}
	return routerContainer{
		image: router.Image,
		create: func(_ context.Context, address, dir string) (bool, error) {
			f.calls = append(f.calls, "create "+address+" "+dir)
			f.folder = folder()
			return f.adopted, f.createErr
		},
		remove: func(context.Context) error {
			f.calls = append(f.calls, "remove")
			return nil
		},
		reload: func(context.Context) error {
			f.calls = append(f.calls, "reload")
			f.folder = folder()
			return f.reloadErr
		},
		join: func(context.Context) error {
			f.calls = append(f.calls, "join")
			return nil
		},
	}
}

// routerDataHome writes a data home with the router address 10.10.10.1
// and a default local cluster on educates.test, and returns it.
func routerDataHome(t *testing.T) string {
	t.Helper()
	dataHome := t.TempDir()
	writeRouterFile(t, dataHome, handWrittenRouterFile)
	writeLocalClusterListFile(t, filepath.Join(dataHome, "config.yaml"),
		"apiVersion: cli.educates.dev/v1alpha1\nkind: EducatesLocalConfig\n"+
			"ingress:\n  domain: educates.test\n")
	return dataHome
}

// routerInventory is the inventory of dataHome with the default local
// cluster's node running, on a host with the router address, and router
// the router container observed, nil for none.
func routerInventory(dataHome string, nodes []localclusters.NodeContainer, router *localclusters.RouterContainer) localclusters.Inventory {
	if nodes == nil {
		nodes = []localclusters.NodeContainer{{Name: "educates-control-plane", KindCluster: "educates", Role: "control-plane", Running: true}}
	}
	return localclusters.NewInventory(localclusters.Observed{
		Nodes:              nodes,
		InterfaceAddresses: []netip.Addr{netip.MustParseAddr("10.10.10.1")},
		Router:             router,
	}, dataHome)
}

// routerOn is a router container from image, publishing ports 80 and 443
// on 10.10.10.1.
func routerOn(image string, running bool) *localclusters.RouterContainer {
	addr := netip.MustParseAddr("10.10.10.1")
	return &localclusters.RouterContainer{
		Running:      running,
		Image:        image,
		PortBindings: []localclusters.PortBinding{{HostIP: addr, HostPort: 80}, {HostIP: addr, HostPort: 443}},
	}
}

// routerOperationsForTest are the router operations on the fake container f
// serving dataHome, whose privileged port check returns checkErr.
func routerOperationsForTest(f *fakeRouterContainer, dataHome string, checkErr error) routerOperations {
	return routerOperations{
		container:            f.container(dataHome),
		dataHome:             dataHome,
		checkPrivilegedPorts: func(context.Context) error { return checkErr },
	}
}

func TestEnsureRouterCreatesAMissingRouterServingTheRenderedFolder(t *testing.T) {
	dataHome := routerDataHome(t)
	f := &fakeRouterContainer{}
	var out bytes.Buffer

	changed, err := routerOperationsForTest(f, dataHome, nil).ensureRouter(context.Background(), progress.New(&out, 0, false),
		routerInventory(dataHome, nil, nil))
	if err != nil {
		t.Fatalf("ensureRouter: %v", err)
	}

	if want := []string{"create 10.10.10.1 " + config.LocalRouterDir(dataHome)}; !changed || !slices.Equal(f.calls, want) {
		t.Errorf("ensureRouter() = %t with calls %q, want true with %q", changed, f.calls, want)
	}
	if want := []string{"404.txt", "503-educates.txt", "haproxy.cfg", "hosts.map"}; !slices.Equal(f.folder, want) {
		t.Errorf("router folder held %q when the router was created, want %q", f.folder, want)
	}
}

func TestEnsureRouterRecreatesARouterOnAnotherImage(t *testing.T) {
	dataHome := routerDataHome(t)
	f := &fakeRouterContainer{}
	var out bytes.Buffer

	changed, err := routerOperationsForTest(f, dataHome, nil).ensureRouter(context.Background(), progress.New(&out, 0, false),
		routerInventory(dataHome, nil, routerOn("haproxy:3.4.7-alpine", true)))
	if err != nil {
		t.Fatalf("ensureRouter: %v", err)
	}

	if want := []string{"remove", "create 10.10.10.1 " + config.LocalRouterDir(dataHome)}; !changed || !slices.Equal(f.calls, want) {
		t.Errorf("ensureRouter() = %t with calls %q, want true with %q", changed, f.calls, want)
	}
	if !strings.Contains(out.String(), "haproxy:3.4.7-alpine") {
		t.Errorf("ensureRouter reported:\n%s\nwant why it re-creates the router", out.String())
	}
}

// A router on the pinned image and the router address needs no creating,
// and no Docker Desktop check: it is refreshed instead.
func TestEnsureRouterLeavesARunningRouterToTheRefresh(t *testing.T) {
	dataHome := routerDataHome(t)
	f := &fakeRouterContainer{}
	var out bytes.Buffer

	changed, err := routerOperationsForTest(f, dataHome, errors.New("setting off")).ensureRouter(context.Background(),
		progress.New(&out, 0, false), routerInventory(dataHome, nil, routerOn(router.Image, true)))
	if err != nil {
		t.Fatalf("ensureRouter: %v", err)
	}

	if changed || len(f.calls) != 0 {
		t.Errorf("ensureRouter() = %t with calls %q, want false with none", changed, f.calls)
	}
}

func TestEnsureRouterRefusesWhenDockerDesktopCannotServeThePorts(t *testing.T) {
	dataHome := routerDataHome(t)
	f := &fakeRouterContainer{}
	settingOff := errors.New(`"Allow privileged port forwarding to external machines" is off`)
	var out bytes.Buffer

	_, err := routerOperationsForTest(f, dataHome, settingOff).ensureRouter(context.Background(), progress.New(&out, 0, false),
		routerInventory(dataHome, nil, nil))

	if !errors.Is(err, settingOff) || len(f.calls) != 0 {
		t.Errorf("ensureRouter() = %v with calls %q, want the setting's error and no call", err, f.calls)
	}
}

func TestEnsureRouterNamesAClusterMadeByAnEarlierCLIWhenTheRouterCannotStart(t *testing.T) {
	dataHome := routerDataHome(t)
	dockerErr := errors.New("listen tcp4 0.0.0.0:80: bind: address already in use")
	f := &fakeRouterContainer{createErr: &router.StartError{Err: dockerErr}}
	addr := netip.MustParseAddr("10.10.10.1")
	earlier := []localclusters.NodeContainer{{
		Name: "educates-control-plane", KindCluster: "educates", Role: "control-plane", Running: true,
		PortBindings: []localclusters.PortBinding{{HostIP: addr, HostPort: 80}, {HostIP: addr, HostPort: 443}},
	}}
	var out bytes.Buffer

	_, err := routerOperationsForTest(f, dataHome, nil).ensureRouter(context.Background(), progress.New(&out, 0, false),
		routerInventory(dataHome, earlier, nil))

	if err == nil || !errors.Is(err, dockerErr) {
		t.Fatalf("ensureRouter() = %v, want Docker's error", err)
	}
	for _, part := range []string{"10.10.10.1", "the default local cluster, which is running", "earlier release",
		"educates local cluster delete"} {
		if !strings.Contains(err.Error(), part) {
			t.Errorf("error %q does not mention %q", err, part)
		}
	}
}

func TestEnsureRouterReloadsARouterAnotherCommandCreated(t *testing.T) {
	dataHome := routerDataHome(t)
	f := &fakeRouterContainer{adopted: true}
	var out bytes.Buffer

	if _, err := routerOperationsForTest(f, dataHome, nil).ensureRouter(context.Background(), progress.New(&out, 0, false),
		routerInventory(dataHome, nil, nil)); err != nil {
		t.Fatalf("ensureRouter: %v", err)
	}

	if want := []string{"create 10.10.10.1 " + config.LocalRouterDir(dataHome), "reload"}; !slices.Equal(f.calls, want) {
		t.Errorf("calls = %q, want %q", f.calls, want)
	}
}

// refreshRouterForTest refreshes the fake router container f of dataHome
// with the inventory inv, and returns what the refresh reported.
func refreshRouterForTest(f *fakeRouterContainer, dataHome string, inv localclusters.Inventory, given ...localclusters.LoadedConfig) string {
	var out bytes.Buffer
	routerOperationsForTest(f, dataHome, nil).refreshRouter(context.Background(), progress.New(&out, 0, false),
		func(context.Context) (localclusters.Inventory, error) { return inv, nil }, given...)
	return out.String()
}

func TestRefreshRouterWritesTheFolderJoinsTheClusterNetworkAndReloads(t *testing.T) {
	dataHome := routerDataHome(t)
	f := &fakeRouterContainer{}

	out := refreshRouterForTest(f, dataHome, routerInventory(dataHome, nil, routerOn(router.Image, true)))

	if want := []string{"join", "reload"}; !slices.Equal(f.calls, want) {
		t.Errorf("calls = %q, want %q", f.calls, want)
	}
	if !slices.Contains(f.folder, "503-educates.txt") {
		t.Errorf("router folder held %q when reloaded, want the default cluster's route", f.folder)
	}
	if strings.Contains(out, "⚠") {
		t.Errorf("refresh warned:\n%s", out)
	}
}

func TestRefreshRouterDoesNothingWithoutARouter(t *testing.T) {
	dataHome := routerDataHome(t)
	f := &fakeRouterContainer{}

	out := refreshRouterForTest(f, dataHome, routerInventory(dataHome, nil, nil))

	if len(f.calls) != 0 || out != "" {
		t.Errorf("calls %q, reported %q; want nothing done", f.calls, out)
	}
	if _, err := os.Stat(config.LocalRouterDir(dataHome)); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("router folder written with no router: %v", err)
	}
}

// A cluster being created is routed from the config given, which may not
// be in the data home.
func TestRefreshRouterRoutesTheConfigGiven(t *testing.T) {
	dataHome := routerDataHome(t)
	f := &fakeRouterContainer{}
	test2, err := localclusters.Named("test2", dataHome)
	if err != nil {
		t.Fatal(err)
	}
	path := writeFixture(t, "apiVersion: cli.educates.dev/v1alpha1\nkind: EducatesLocalConfig\n"+
		"ingress:\n  domain: test2.educates.test\n")
	cfg, err := localclusters.LoadConfigFile(test2, path)
	if err != nil {
		t.Fatal(err)
	}

	refreshRouterForTest(f, dataHome, routerInventory(dataHome, nil, routerOn(router.Image, true)),
		localclusters.LoadedConfig{Identity: test2, Config: cfg, Source: path})

	if !slices.Contains(f.folder, "503-educates-test2.txt") {
		t.Errorf("router folder held %q when reloaded, want test2's route", f.folder)
	}
}

func TestRefreshRouterOnlyWritesTheFolderOfAStoppedRouter(t *testing.T) {
	dataHome := routerDataHome(t)
	f := &fakeRouterContainer{}

	out := refreshRouterForTest(f, dataHome, routerInventory(dataHome, nil, routerOn(router.Image, false)))

	if want := []string{"join"}; !slices.Equal(f.calls, want) {
		t.Errorf("calls = %q, want %q", f.calls, want)
	}
	if _, err := os.Stat(filepath.Join(config.LocalRouterDir(dataHome), "haproxy.cfg")); err != nil {
		t.Errorf("router folder not written: %v", err)
	}
	if !strings.Contains(out, "not running") {
		t.Errorf("refresh reported:\n%s\nwant a warning that the router is not running", out)
	}
}

func TestRefreshRouterWarnsWhenItCannotReload(t *testing.T) {
	dataHome := routerDataHome(t)
	f := &fakeRouterContainer{reloadErr: errors.New("the router refuses its configuration")}

	out := refreshRouterForTest(f, dataHome, routerInventory(dataHome, nil, routerOn(router.Image, true)))

	for _, part := range []string{"⚠", "the router refuses its configuration", "educates local router update", "skipped"} {
		if !strings.Contains(out, part) {
			t.Errorf("refresh reported:\n%s\nwant it to mention %q", out, part)
		}
	}
}

func TestRemoveRouterRemovesTheContainerAndTheFolderButKeepsTheRouterFile(t *testing.T) {
	dataHome := routerDataHome(t)
	if err := localclusters.WriteRouterFolder(dataHome, localclusters.RenderRouter(nil)); err != nil {
		t.Fatal(err)
	}
	f := &fakeRouterContainer{}

	if err := routerOperationsForTest(f, dataHome, nil).removeRouter(context.Background()); err != nil {
		t.Fatalf("removeRouter: %v", err)
	}

	if want := []string{"remove"}; !slices.Equal(f.calls, want) {
		t.Errorf("calls = %q, want %q", f.calls, want)
	}
	if _, err := os.Stat(config.LocalRouterDir(dataHome)); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("router folder still there: %v", err)
	}
	if _, err := os.Stat(config.LocalRouterPath(dataHome)); err != nil {
		t.Errorf("router.yaml is gone: %v", err)
	}
}
