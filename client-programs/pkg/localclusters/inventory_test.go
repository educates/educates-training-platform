package localclusters

import (
	"net/netip"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// addClusterFolder creates the data-home folder of the named local
// cluster name, which makes a kind cluster educates-<name> an Educates
// cluster.
func addClusterFolder(t *testing.T, dataHome, name string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(dataHome, "clusters", name), 0o755); err != nil {
		t.Fatal(err)
	}
}

// controlPlane is the first control-plane node container of the kind
// cluster kindName, configured to publish ports 80 and 443 on hostIP.
func controlPlane(kindName, hostIP string, running bool) NodeContainer {
	return NodeContainer{
		Name:         kindName + "-control-plane",
		KindCluster:  kindName,
		Role:         "control-plane",
		Running:      running,
		PortBindings: ingressBindings(hostIP),
	}
}

// ingressBindings are the bindings of ports 80 and 443 on hostIP, beside
// the API server's on loopback, as kind configures a control-plane node.
func ingressBindings(hostIP string) []PortBinding {
	addr := netip.MustParseAddr(hostIP)
	return []PortBinding{
		{HostIP: netip.MustParseAddr("127.0.0.1"), HostPort: 52345},
		{HostIP: addr, HostPort: 80},
		{HostIP: addr, HostPort: 443},
	}
}

// kindNames returns the kind cluster names of ids.
func kindNames(ids []Identity) []string {
	names := make([]string, 0, len(ids))
	for _, id := range ids {
		names = append(names, id.KindName)
	}
	return names
}

func TestClustersAreTheDefaultAndTheNamedClustersWithAFolder(t *testing.T) {
	dataHome := t.TempDir()
	addClusterFolder(t, dataHome, "test2")
	addClusterFolder(t, dataHome, "alpha")
	addClusterFolder(t, dataHome, "configured-but-not-created")
	observed := Observed{Nodes: []NodeContainer{
		controlPlane("educates-test2", "10.10.10.2", true),
		{Name: "educates-test2-worker", KindCluster: "educates-test2", Role: "worker", Running: true},
		controlPlane("educates-x", "10.10.10.3", true),
		controlPlane("educates-installer-smoke", "0.0.0.0", true),
		controlPlane("kind", "0.0.0.0", false),
		controlPlane("educates-alpha", "10.10.10.4", false),
		controlPlane("educates", "10.10.10.1", true),
		controlPlane("educates-Bad_Name", "10.10.10.5", true),
	}}

	got := kindNames(NewInventory(observed, dataHome).Clusters())

	want := []string{"educates", "educates-alpha", "educates-test2"}
	if !slices.Equal(got, want) {
		t.Errorf("Clusters() = %v, want %v", got, want)
	}
}

// writeClusterConfig writes body as the config of the local cluster
// named name in the data home dataHome, the default local cluster's when
// name is empty.
func writeClusterConfig(t *testing.T, dataHome, name, body string) {
	t.Helper()
	id := Default(dataHome)
	if name != "" {
		var err error
		if id, err = Named(name, dataHome); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.MkdirAll(filepath.Dir(id.ConfigPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(id.ConfigPath, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestListRows(t *testing.T) {
	dataHome := t.TempDir()
	writeClusterConfig(t, dataHome, "", localConfigHeader+"ingress:\n  domain: educates.test\n")
	writeClusterConfig(t, dataHome, "test2", localConfigHeader+"cluster:\n  listenAddress: 10.10.10.2\n")
	writeClusterConfig(t, dataHome, "test3", localConfigHeader+
		"cluster:\n  listenAddress: 10.10.10.3\ningress:\n  domain: test3.educates.test\n")
	observed := Observed{Nodes: []NodeContainer{
		controlPlane("educates", "10.10.10.1", true),
		controlPlane("educates-test2", "10.10.10.2", false),
		{Name: "educates-test2-worker", KindCluster: "educates-test2", Role: "worker"},
		controlPlane("educates-test3", "10.10.10.3", true),
		{Name: "educates-test3-worker", KindCluster: "educates-test3", Role: "worker", Running: false},
		controlPlane("educates-x", "10.10.10.9", true),
	}}

	rows := NewInventory(observed, dataHome).List()

	want := []struct {
		name, state, listen, domain, context string
		isDefault                            bool
	}{
		{"", "running", "10.10.10.1", "educates.test", "kind-educates", true},
		{"test2", "stopped", "10.10.10.2", "10-10-10-2.nip.io", "kind-educates-test2", false},
		{"test3", "partly running", "10.10.10.3", "test3.educates.test", "kind-educates-test3", false},
	}
	if len(rows) != len(want) {
		t.Fatalf("List() returned %d rows, want %d: %+v", len(rows), len(want), rows)
	}
	for i, w := range want {
		r := rows[i]
		if r.Identity.Name != w.name || r.Identity.IsDefault() != w.isDefault || string(r.State) != w.state ||
			r.ListenAddress != w.listen || r.Domain != w.domain || r.Identity.Context != w.context {
			t.Errorf("row %d = {name %q, default %t, state %q, listen %q, domain %q, context %q}, want %+v",
				i, r.Identity.Name, r.Identity.IsDefault(), r.State, r.ListenAddress, r.Domain, r.Identity.Context, w)
		}
		if r.ConfigError != nil {
			t.Errorf("row %d: ConfigError = %v, want none", i, r.ConfigError)
		}
	}
}

func TestListRowsShowTheDomainTheResolverServes(t *testing.T) {
	dataHome := t.TempDir()
	writeClusterConfig(t, dataHome, "", localConfigHeader)
	writeClusterConfig(t, dataHome, "test2", localConfigHeader+"cluster:\n  listenAddress: 10.10.10.2\n")
	nodes := []NodeContainer{
		controlPlane("educates", "10.10.10.1", true),
		controlPlane("educates-test2", "10.10.10.2", true),
	}

	for _, tc := range []struct {
		name, hostIP, wantDefault string
	}{
		{name: "the default cluster's host IP nip.io domain", hostIP: "192.168.1.10", wantDefault: "192-168-1-10.nip.io"},
		{name: "no domain for the default cluster without a host IP"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			inv := NewInventory(Observed{Nodes: nodes, HostIP: tc.hostIP}, dataHome)

			rows := inv.List()

			if got := rows[0].Domain; got != tc.wantDefault {
				t.Errorf("default cluster's domain = %q, want %q", got, tc.wantDefault)
			}
			if got, want := rows[1].Domain, "10-10-10-2.nip.io"; got != want {
				t.Errorf("test2's domain = %q, want %q", got, want)
			}
			content, _, err := inv.ResolverConfig()
			if err != nil {
				t.Fatalf("ResolverConfig: %v", err)
			}
			for _, row := range rows {
				if row.Domain != "" && !strings.Contains(string(content), "address=/"+row.Domain+"/") {
					t.Errorf("the resolver does not serve %s's listed domain %q:\n%s", row.Identity.Description(), row.Domain, content)
				}
			}
		})
	}
}

func TestListRowsOfClustersWithoutAConfig(t *testing.T) {
	dataHome := t.TempDir()
	addClusterFolder(t, dataHome, "fromfile")
	observed := Observed{Nodes: []NodeContainer{
		controlPlane("educates", "0.0.0.0", true),
		controlPlane("educates-fromfile", "10.10.10.2", true),
	}}

	rows := NewInventory(observed, dataHome).List()

	if len(rows) != 2 {
		t.Fatalf("List() returned %d rows, want 2: %+v", len(rows), rows)
	}
	for _, r := range rows {
		if r.Domain != "" || r.ConfigError != nil {
			t.Errorf("row of %s: domain %q, config error %v; want no domain and no error", r.Identity.KindName, r.Domain, r.ConfigError)
		}
	}
	if rows[0].ListenAddress != "0.0.0.0" {
		t.Errorf("listen address of the default cluster = %q, want 0.0.0.0", rows[0].ListenAddress)
	}
}

func TestListRowsReportAConfigThatDoesNotLoad(t *testing.T) {
	dataHome := t.TempDir()
	writeClusterConfig(t, dataHome, "test2", localConfigHeader+"cluster:\n  listenAddress: 127.0.0.1\n")
	observed := Observed{Nodes: []NodeContainer{controlPlane("educates-test2", "10.10.10.2", true)}}

	rows := NewInventory(observed, dataHome).List()

	if len(rows) != 1 {
		t.Fatalf("List() returned %d rows, want 1: %+v", len(rows), rows)
	}
	if rows[0].Domain != "" {
		t.Errorf("domain = %q, want none", rows[0].Domain)
	}
	wantErrorMentioning(t, rows[0].ConfigError, "loopback")
}

func TestListRowsLeaveTheListenAddressEmptyWithNoIngressBinding(t *testing.T) {
	dataHome := t.TempDir()
	observed := Observed{Nodes: []NodeContainer{
		{Name: "educates-control-plane", KindCluster: "educates", Role: "control-plane"},
	}}

	rows := NewInventory(observed, dataHome).List()

	if len(rows) != 1 || rows[0].ListenAddress != "" {
		t.Errorf("List() = %+v, want one row with no listen address", rows)
	}
}

// hostInterfaces are the interface addresses of a host with the default
// cluster's and two more host IPs added.
var hostInterfaces = []netip.Addr{
	netip.MustParseAddr("127.0.0.1"),
	netip.MustParseAddr("::1"),
	netip.MustParseAddr("192.168.1.20"),
	netip.MustParseAddr("10.10.10.1"),
	netip.MustParseAddr("10.10.10.2"),
	netip.MustParseAddr("10.10.10.3"),
}

// namedForTest returns the identity of the named local cluster name of
// the data home dataHome, with its folder created.
func namedForTest(t *testing.T, dataHome, name string) Identity {
	t.Helper()
	addClusterFolder(t, dataHome, name)
	id, err := Named(name, dataHome)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func TestCheckCreateRefusesAnIPAnotherClusterBinds(t *testing.T) {
	cases := []struct {
		name  string
		other NodeContainer
		state string
	}{
		{name: "running", other: controlPlane("educates-test2", "10.10.10.2", true), state: "running"},
		{name: "stopped", other: controlPlane("educates-test2", "10.10.10.2", false), state: "stopped"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dataHome := t.TempDir()
			namedForTest(t, dataHome, "test2")
			test3 := namedForTest(t, dataHome, "test3")
			inv := NewInventory(Observed{Nodes: []NodeContainer{tc.other}, InterfaceAddresses: hostInterfaces}, dataHome)

			err := inv.CheckCreate(test3, "10.10.10.2")

			wantErrorMentioning(t, err, `local cluster "test3"`, `local cluster "test2"`, tc.state, "10.10.10.2", "host IP of its own")
		})
	}
}

func TestCheckCreateRefusesTheDefaultClustersIPForANamedCluster(t *testing.T) {
	dataHome := t.TempDir()
	test2 := namedForTest(t, dataHome, "test2")
	inv := NewInventory(Observed{
		Nodes:              []NodeContainer{controlPlane("educates", "10.10.10.1", true)},
		InterfaceAddresses: hostInterfaces,
	}, dataHome)

	err := inv.CheckCreate(test2, "10.10.10.1")

	wantErrorMentioning(t, err, `local cluster "test2"`, "the default local cluster", "10.10.10.1")
}

func TestCheckCreateRefusesANamedClusterBesideADefaultClusterOnEveryAddress(t *testing.T) {
	for _, bound := range []string{"0.0.0.0", "::"} {
		t.Run(bound, func(t *testing.T) {
			dataHome := t.TempDir()
			test2 := namedForTest(t, dataHome, "test2")
			inv := NewInventory(Observed{
				Nodes:              []NodeContainer{controlPlane("educates", bound, true)},
				InterfaceAddresses: hostInterfaces,
			}, dataHome)

			err := inv.CheckCreate(test2, "10.10.10.2")

			wantErrorMentioning(t, err, `local cluster "test2"`, "the default local cluster", bound,
				"every host address", "Re-create the default local cluster on a specific IP")
		})
	}
}

func TestCheckCreateRefusesTheDefaultClusterOnEveryAddressBesideANamedCluster(t *testing.T) {
	dataHome := t.TempDir()
	namedForTest(t, dataHome, "test2")
	inv := NewInventory(Observed{
		Nodes:              []NodeContainer{controlPlane("educates-test2", "10.10.10.2", false)},
		InterfaceAddresses: hostInterfaces,
	}, dataHome)

	err := inv.CheckCreate(Default(dataHome), "0.0.0.0")

	wantErrorMentioning(t, err, "the default local cluster", `local cluster "test2"`, "stopped", "10.10.10.2",
		"every host address", "cluster.listenAddress", "specific IP")
}

func TestCheckCreateAllowsClustersOnIPsOfTheirOwn(t *testing.T) {
	dataHome := t.TempDir()
	namedForTest(t, dataHome, "test2")
	test3 := namedForTest(t, dataHome, "test3")
	observed := Observed{
		Nodes: []NodeContainer{
			controlPlane("educates", "127.0.0.1", true),
			controlPlane("educates-test2", "10.10.10.2", false),
		},
		InterfaceAddresses: hostInterfaces,
	}
	inv := NewInventory(observed, dataHome)

	if err := inv.CheckCreate(test3, "10.10.10.3"); err != nil {
		t.Errorf("CheckCreate(test3, 10.10.10.3) = %v, want no error", err)
	}
	if err := inv.CheckCreate(Default(dataHome), "10.10.10.1"); err != nil {
		t.Errorf("CheckCreate(default, 10.10.10.1) = %v, want no error", err)
	}
}

func TestCheckCreateIgnoresKindClustersThatAreNotEducatesClusters(t *testing.T) {
	dataHome := t.TempDir()
	test2 := namedForTest(t, dataHome, "test2")
	observed := Observed{
		Nodes: []NodeContainer{
			controlPlane("educates-x", "10.10.10.2", true),
			controlPlane("kind", "0.0.0.0", true),
		},
		InterfaceAddresses: hostInterfaces,
	}

	if err := NewInventory(observed, dataHome).CheckCreate(test2, "10.10.10.2"); err != nil {
		t.Errorf("CheckCreate = %v, want no error", err)
	}
}

func TestCheckCreateRefusesAnAddressOnNoHostInterface(t *testing.T) {
	dataHome := t.TempDir()
	test4 := namedForTest(t, dataHome, "test4")
	inv := NewInventory(Observed{InterfaceAddresses: hostInterfaces}, dataHome)

	err := inv.CheckCreate(test4, "10.10.10.4")

	wantErrorMentioning(t, err, `local cluster "test4"`, "10.10.10.4", "no network interface")
}

func TestCheckCreateAllowsEveryAddressWithNoOtherCluster(t *testing.T) {
	inv := NewInventory(Observed{InterfaceAddresses: hostInterfaces}, t.TempDir())

	if err := inv.CheckCreate(Default(t.TempDir()), "0.0.0.0"); err != nil {
		t.Errorf("CheckCreate(default, 0.0.0.0) = %v, want no error", err)
	}
}

func TestCheckCreateReportsAClashBeforeAMissingInterface(t *testing.T) {
	dataHome := t.TempDir()
	namedForTest(t, dataHome, "test2")
	test3 := namedForTest(t, dataHome, "test3")
	inv := NewInventory(Observed{
		Nodes:              []NodeContainer{controlPlane("educates-test2", "10.10.10.9", false)},
		InterfaceAddresses: hostInterfaces,
	}, dataHome)

	err := inv.CheckCreate(test3, "10.10.10.9")

	wantErrorMentioning(t, err, `local cluster "test2"`, "host IP of its own")
}

func TestCheckCreateRefusesAnAddressThatIsNotAnIP(t *testing.T) {
	dataHome := t.TempDir()
	inv := NewInventory(Observed{InterfaceAddresses: hostInterfaces}, dataHome)

	err := inv.CheckCreate(Default(dataHome), "localhost")

	wantErrorMentioning(t, err, "localhost", "not an IP address")
}

func TestCheckStartRefusesAnIPAnotherClusterBinds(t *testing.T) {
	dataHome := t.TempDir()
	test2 := namedForTest(t, dataHome, "test2")
	namedForTest(t, dataHome, "test3")
	inv := NewInventory(Observed{
		Nodes: []NodeContainer{
			controlPlane("educates-test2", "10.10.10.2", false),
			controlPlane("educates-test3", "10.10.10.2", true),
		},
		InterfaceAddresses: hostInterfaces,
	}, dataHome)

	err := inv.CheckStart(test2)

	wantErrorMentioning(t, err, `local cluster "test2"`, `local cluster "test3"`, "running", "10.10.10.2")
}

func TestCheckStartTellsADefaultClusterOnEveryAddressToBeRecreated(t *testing.T) {
	dataHome := t.TempDir()
	namedForTest(t, dataHome, "test2")
	inv := NewInventory(Observed{
		Nodes: []NodeContainer{
			controlPlane("educates", "0.0.0.0", false),
			controlPlane("educates-test2", "10.10.10.2", true),
		},
		InterfaceAddresses: hostInterfaces,
	}, dataHome)

	err := inv.CheckStart(Default(dataHome))

	wantErrorMentioning(t, err, "the default local cluster", `local cluster "test2"`, "0.0.0.0",
		"Re-create the default local cluster on a specific IP")
}

func TestCheckStartAllowsAClusterOnAnIPOfItsOwn(t *testing.T) {
	dataHome := t.TempDir()
	test2 := namedForTest(t, dataHome, "test2")
	inv := NewInventory(Observed{
		Nodes: []NodeContainer{
			controlPlane("educates", "10.10.10.1", true),
			controlPlane("educates-test2", "10.10.10.2", false),
			{Name: "educates-test2-worker", KindCluster: "educates-test2", Role: "worker"},
		},
		InterfaceAddresses: hostInterfaces,
	}, dataHome)

	if err := inv.CheckStart(test2); err != nil {
		t.Errorf("CheckStart(test2) = %v, want no error", err)
	}
}

func TestCheckStartRefusesAnAddressOnNoHostInterface(t *testing.T) {
	dataHome := t.TempDir()
	test2 := namedForTest(t, dataHome, "test2")
	inv := NewInventory(Observed{
		Nodes:              []NodeContainer{controlPlane("educates-test2", "10.10.10.7", false)},
		InterfaceAddresses: hostInterfaces,
	}, dataHome)

	err := inv.CheckStart(test2)

	wantErrorMentioning(t, err, `local cluster "test2"`, "10.10.10.7", "no network interface")
}

func TestCheckStartLeavesAClusterWithNoNodesToTheStart(t *testing.T) {
	dataHome := t.TempDir()
	test2 := namedForTest(t, dataHome, "test2")
	inv := NewInventory(Observed{InterfaceAddresses: hostInterfaces}, dataHome)

	if err := inv.CheckStart(test2); err != nil {
		t.Errorf("CheckStart(test2) = %v, want no error", err)
	}
}

func TestCheckEducatesClusterRefusesANamedClusterWithoutAFolder(t *testing.T) {
	dataHome := t.TempDir()
	id := namedID(t, "foo", dataHome)

	err := CheckEducatesCluster(id)

	wantErrorMentioning(t, err, `local cluster "foo"`, filepath.Join(dataHome, "clusters", "foo"),
		"educates-foo", "is not a local cluster", "left alone",
		"educates local cluster create --local-cluster foo")
}

func TestCheckEducatesClusterAcceptsTheClustersTheInventoryCounts(t *testing.T) {
	dataHome := t.TempDir()
	addClusterFolder(t, dataHome, "test2")

	for _, id := range []Identity{Default(dataHome), namedID(t, "test2", dataHome)} {
		if err := CheckEducatesCluster(id); err != nil {
			t.Errorf("CheckEducatesCluster(%s) = %v, want nil", id.Description(), err)
		}
	}
}

func TestClustersIsEmptyWithNoEducatesCluster(t *testing.T) {
	observed := Observed{Nodes: []NodeContainer{controlPlane("kind", "0.0.0.0", true)}}

	if got := NewInventory(observed, t.TempDir()).Clusters(); len(got) != 0 {
		t.Errorf("Clusters() = %v, want none", kindNames(got))
	}
}

func TestSharedInfrastructureUsers(t *testing.T) {
	dataHome := t.TempDir()
	test2 := namedForTest(t, dataHome, "test2")
	namedForTest(t, dataHome, "test3")

	cases := []struct {
		name     string
		deleted  Identity
		observed []NodeContainer
		want     []string
	}{
		{
			name:    "last cluster deleted",
			deleted: test2,
			want:    nil,
		},
		{
			name:     "deleted cluster still observed",
			deleted:  test2,
			observed: []NodeContainer{controlPlane("educates-test2", "10.10.10.2", true)},
			want:     nil,
		},
		{
			name:    "only kind clusters that are not Educates clusters remain",
			deleted: Default(dataHome),
			observed: []NodeContainer{
				controlPlane("educates-x", "10.10.10.9", true),
				controlPlane("kind", "0.0.0.0", true),
			},
			want: nil,
		},
		{
			name:    "other clusters remain, running or stopped",
			deleted: test2,
			observed: []NodeContainer{
				controlPlane("educates-test3", "10.10.10.3", false),
				controlPlane("educates", "10.10.10.1", true),
			},
			want: []string{"educates", "educates-test3"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			inv := NewInventory(Observed{Nodes: tc.observed}, dataHome)

			got := kindNames(inv.SharedInfrastructureUsers(tc.deleted))

			if !slices.Equal(got, tc.want) {
				t.Errorf("SharedInfrastructureUsers = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestDescribeClusters(t *testing.T) {
	dataHome := t.TempDir()
	test2 := namedForTest(t, dataHome, "test2")
	test3 := namedForTest(t, dataHome, "test3")

	cases := []struct {
		ids  []Identity
		want string
	}{
		{ids: []Identity{test2}, want: `local cluster "test2"`},
		{ids: []Identity{Default(dataHome), test2}, want: `the default local cluster and local cluster "test2"`},
		{ids: []Identity{Default(dataHome), test2, test3},
			want: `the default local cluster, local cluster "test2" and local cluster "test3"`},
	}
	for _, tc := range cases {
		if got := DescribeClusters(tc.ids); got != tc.want {
			t.Errorf("DescribeClusters(%v) = %q, want %q", kindNames(tc.ids), got, tc.want)
		}
	}
}
