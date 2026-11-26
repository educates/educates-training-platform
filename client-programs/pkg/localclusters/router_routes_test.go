package localclusters

import (
	"fmt"
	"path/filepath"
	"reflect"
	"testing"
)

// kindControlPlane is the first control-plane node container of the kind
// cluster kindName, publishing nothing on the host, as a node of a local
// cluster served through the router is.
func kindControlPlane(kindName string, running bool) NodeContainer {
	return NodeContainer{
		Name:        kindName + "-control-plane",
		KindCluster: kindName,
		Role:        ControlPlaneRole,
		Running:     running,
	}
}

// routeSummary is what a test compares of a route: its cluster by kind
// name, its names, its node and its state.
type routeSummary struct {
	KindName string
	Names    []string
	Node     string
	State    State
}

// summarizeRoutes returns the summaries of routes, in order.
func summarizeRoutes(routes []RouterRoute) []routeSummary {
	summaries := make([]routeSummary, 0, len(routes))
	for _, r := range routes {
		summaries = append(summaries, routeSummary{KindName: r.Cluster.KindName, Names: r.Names, Node: r.Node, State: r.State})
	}
	return summaries
}

func wantRoutes(t *testing.T, got []RouterRoute, want ...routeSummary) {
	t.Helper()
	if summaries := summarizeRoutes(got); !reflect.DeepEqual(summaries, want) {
		t.Errorf("routes:\n got  %+v\n want %+v", summaries, want)
	}
}

// routerRoutes returns the routes of the inventory of the data home
// dataHome with the node containers observed, failing t when it leaves a
// config out or fails.
func routerRoutes(t *testing.T, dataHome string, observed []NodeContainer, given ...LoadedConfig) []RouterRoute {
	t.Helper()
	routes, leftOut, err := NewInventory(Observed{Nodes: observed}, dataHome).RouterRoutes(given...)
	if err != nil {
		t.Fatalf("RouterRoutes: %v", err)
	}
	if len(leftOut) > 0 {
		t.Fatalf("RouterRoutes left out %v, want none left out", leftOut)
	}
	return routes
}

// givenLoadedConfig loads body, written outside the data home, as the
// config of the local cluster id, the way a command given a config would.
func givenLoadedConfig(t *testing.T, id Identity, body string) LoadedConfig {
	t.Helper()
	return givenConfig(t, id, body).LoadedConfig
}

func TestRouterRoutesServeEveryEducatesClusterRunningOrStopped(t *testing.T) {
	dataHome := t.TempDir()
	writeDataHomeConfig(t, dataHome, "config.yaml", localConfigHeader+
		"ingress:\n  domain: educates.test\n"+
		"resolver:\n  extraDomains:\n  - example.test\n  - Educates.Test\n")
	writeDataHomeConfig(t, dataHome, "clusters/test2/config.yaml", localConfigHeader+
		"ingress:\n  domain: test2.educates.test\n")

	routes := routerRoutes(t, dataHome, []NodeContainer{
		kindControlPlane("educates-test2", false),
		{Name: "educates-test2-worker", KindCluster: "educates-test2", Role: "worker"},
		kindControlPlane("kind", true),
		kindControlPlane("educates", true),
	})

	wantRoutes(t, routes,
		routeSummary{KindName: "educates", Names: []string{"educates.test", "example.test"},
			Node: "educates-control-plane", State: Running},
		routeSummary{KindName: "educates-test2", Names: []string{"test2.educates.test"},
			Node: "educates-test2-control-plane", State: Stopped},
	)
}

// The resolver serves the default local cluster's config even with no
// default cluster, but the router routes the clusters that exist only.
func TestRouterRoutesLeaveOutAClusterThatDoesNotExist(t *testing.T) {
	dataHome := t.TempDir()
	writeDataHomeConfig(t, dataHome, "config.yaml", localConfigHeader+
		"ingress:\n  domain: educates.test\n")
	writeDataHomeConfig(t, dataHome, "clusters/test2/config.yaml", localConfigHeader+
		"ingress:\n  domain: test2.educates.test\n")

	routes := routerRoutes(t, dataHome, []NodeContainer{kindControlPlane("educates-test2", true)})

	wantRoutes(t, routes, routeSummary{KindName: "educates-test2", Names: []string{"test2.educates.test"},
		Node: "educates-test2-control-plane", State: Running})
}

func TestRouterRoutesServeTheDefaultClusterAtTheNipDomainOfTheRouterAddress(t *testing.T) {
	dataHome := t.TempDir()
	routerFile(t, dataHome, routerHeader+"address: 10.10.10.1\n")
	writeDataHomeConfig(t, dataHome, "config.yaml", localConfigHeader)

	routes := routerRoutes(t, dataHome, []NodeContainer{kindControlPlane("educates", true)})

	wantRoutes(t, routes, routeSummary{KindName: "educates", Names: []string{"10-10-10-1.nip.io"},
		Node: "educates-control-plane", State: Running})
}

// A named cluster declaring no domain is served at its name under the
// nip.io name of the router address, nested under the default cluster's,
// and the router's host map takes the most specific domain first, of
// every cluster's domain and extra domains alike, so that each name
// reaches the cluster the resolver sends it for.
func TestRouterRoutesServeNamedClustersByNameMostSpecificDomainFirst(t *testing.T) {
	dataHome := t.TempDir()
	routerFile(t, dataHome, routerHeader+"address: 10.10.10.1\n")
	writeDataHomeConfig(t, dataHome, "config.yaml", localConfigHeader+
		"resolver:\n  extraDomains:\n  - educates.test\n")
	writeDataHomeConfig(t, dataHome, "clusters/test2/config.yaml", localConfigHeader)
	writeDataHomeConfig(t, dataHome, "clusters/pr1341/config.yaml", localConfigHeader+
		"ingress:\n  domain: pr1341.educates.test\nresolver:\n  extraDomains:\n  - apps.example.test\n")

	routes := routerRoutes(t, dataHome, []NodeContainer{
		kindControlPlane("educates", true),
		kindControlPlane("educates-test2", true),
		kindControlPlane("educates-pr1341", false),
	})

	wantRoutes(t, routes,
		routeSummary{KindName: "educates", Names: []string{"10-10-10-1.nip.io", "educates.test"},
			Node: "educates-control-plane", State: Running},
		routeSummary{KindName: "educates-pr1341", Names: []string{"pr1341.educates.test", "apps.example.test"},
			Node: "educates-pr1341-control-plane", State: Stopped},
		routeSummary{KindName: "educates-test2", Names: []string{"test2.10-10-10-1.nip.io"},
			Node: "educates-test2-control-plane", State: Running},
	)
	wantEntries(t, mapEntries(t, RenderRouter(routes)),
		".test2.10-10-10-1.nip.io educates-test2",
		".10-10-10-1.nip.io educates",
		".apps.example.test educates-pr1341",
		".pr1341.educates.test educates-pr1341",
		".educates.test educates",
	)
}

// A config given is that of a cluster being created, which replaces the
// one in the data home, and is routed whether or not its nodes exist yet.
func TestRouterRoutesServeAClusterBeingCreatedFromItsGivenConfig(t *testing.T) {
	dataHome := t.TempDir()
	writeDataHomeConfig(t, dataHome, "clusters/test2/config.yaml", localConfigHeader+
		"ingress:\n  domain: old.educates.test\n")
	test2 := namedID(t, "test2", dataHome)

	routes := routerRoutes(t, dataHome, nil,
		givenLoadedConfig(t, test2, localConfigHeader+"ingress:\n  domain: test2.educates.test\n"))

	wantRoutes(t, routes, routeSummary{KindName: "educates-test2", Names: []string{"test2.educates.test"},
		Node: "educates-test2-control-plane", State: Stopped})
}

// A cluster with no config in the data home, such as one created with
// --config, or whose config does not load, is another cluster's problem:
// the router leaves it out, and says why for the caller to warn about.
func TestRouterRoutesLeaveOutAClusterWhoseConfigTheyCannotUse(t *testing.T) {
	dataHome := t.TempDir()
	writeDataHomeConfig(t, dataHome, "config.yaml", localConfigHeader+
		"ingress:\n  domain: educates.test\n")
	writeDataHomeConfig(t, dataHome, "clusters/test2/config.yaml", twoControlPlanesConfig)
	addClusterFolder(t, dataHome, "test3")

	routes, leftOut, err := NewInventory(Observed{Nodes: []NodeContainer{
		kindControlPlane("educates", true),
		kindControlPlane("educates-test2", true),
		kindControlPlane("educates-test3", true),
	}}, dataHome).RouterRoutes()
	if err != nil {
		t.Fatalf("RouterRoutes: %v", err)
	}

	wantRoutes(t, routes, routeSummary{KindName: "educates", Names: []string{"educates.test"},
		Node: "educates-control-plane", State: Running})
	if len(leftOut) != 2 {
		t.Fatalf("left out %v, want test2's and test3's configs", leftOut)
	}
	wantErrorMentioning(t, leftOut[0], "router", `local cluster "test2"`,
		filepath.Join(dataHome, "clusters", "test2", "config.yaml"), twoControlPlanes)
	wantErrorMentioning(t, leftOut[1], "router", `local cluster "test3"`, "no config")
}

func TestRouterRoutesRefuseAGivenConfigTheyCannotServe(t *testing.T) {
	for _, tc := range unservableDefaultConfigs {
		t.Run(tc.name, func(t *testing.T) {
			dataHome := t.TempDir()
			given := givenLoadedConfig(t, Default(dataHome), tc.body)

			_, _, err := NewInventory(Observed{}, dataHome).RouterRoutes(given)

			wantErrorMentioning(t, err, given.Source, tc.reason)
		})
	}
}

func TestRouterRoutesRefuseADomainTwoClustersClaim(t *testing.T) {
	dataHome := t.TempDir()
	writeDataHomeConfig(t, dataHome, "config.yaml", localConfigHeader+
		"ingress:\n  domain: educates.test\n")
	writeDataHomeConfig(t, dataHome, "clusters/test2/config.yaml", localConfigHeader+
		"ingress:\n  domain: test2.educates.test\n"+
		"resolver:\n  extraDomains:\n  - Educates.Test\n")

	_, _, err := NewInventory(Observed{Nodes: []NodeContainer{
		kindControlPlane("educates", true),
		kindControlPlane("educates-test2", false),
	}}, dataHome).RouterRoutes()

	wantErrorMentioning(t, err, "the default local cluster", `local cluster "test2"`, fmt.Sprintf("%q", "educates.test"))
}
