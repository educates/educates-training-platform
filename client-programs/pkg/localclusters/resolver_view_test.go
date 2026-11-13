package localclusters

import (
	"reflect"
	"strings"
	"testing"
)

// viewInputs is what a test builds the view of the local DNS resolver
// from.
type viewInputs struct {
	DataHome           string
	HostIP             string
	Observed           []NodeContainer
	Includes           []IncludeFile
	ResolverFileExists func(suffix string) bool
}

// resolverView builds the view of the local DNS resolver from in.
func resolverView(t *testing.T, in viewInputs) ResolverView {
	t.Helper()
	inv := NewInventory(Observed{Nodes: in.Observed, HostIP: in.HostIP}, in.DataHome)
	view, err := inv.ResolverView(in.Includes, in.ResolverFileExists)
	if err != nil {
		t.Fatalf("ResolverView: %v", err)
	}
	return view
}

// nameRows returns the rows of view's names with their notes left out.
func nameRows(view ResolverView) []ResolverNameRow {
	var rows []ResolverNameRow
	for _, r := range view.Names {
		r.Note = ""
		rows = append(rows, r)
	}
	return rows
}

func TestResolverViewShowsEachClusterNameAndWhereItComesFrom(t *testing.T) {
	observed := []NodeContainer{
		controlPlane("educates-test2", "10.10.10.2", true),
		controlPlane("educates-test3", "10.10.10.3", true),
	}
	for _, tc := range []struct {
		name          string
		defaultConfig string
		want          []ResolverNameRow
	}{
		{
			name: "a default cluster that declares its domain",
			defaultConfig: localConfigHeader + "ingress:\n  domain: educates.test\n" +
				"resolver:\n  extraDomains:\n  - example.test\n  - other.test\n",
			want: []ResolverNameRow{
				{Name: "educates.test", Cluster: "(default)", From: "ingress.domain"},
				{Name: "example.test", Cluster: "(default)", From: "resolver.extraDomains"},
				{Name: "other.test", Cluster: "(default)", From: "resolver.extraDomains"},
				{Name: "10-10-10-2.nip.io", Cluster: "test2", From: "nip.io name of the listen address"},
				{Name: "test3.example.test", Cluster: "test3", From: "educatesClusterConfig.ingress.domain"},
				{Name: "extra3.example.test", Cluster: "test3", From: "target.resolver.extraDomains"},
			},
		},
		{
			name:          "a default cluster that declares no domain",
			defaultConfig: localConfigHeader,
			want: []ResolverNameRow{
				{Name: "192-168-50-151.nip.io", Cluster: "(default)", From: "nip.io name of the host IP"},
				{Name: "10-10-10-2.nip.io", Cluster: "test2", From: "nip.io name of the listen address"},
				{Name: "test3.example.test", Cluster: "test3", From: "educatesClusterConfig.ingress.domain"},
				{Name: "extra3.example.test", Cluster: "test3", From: "target.resolver.extraDomains"},
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dataHome := t.TempDir()
			writeDataHomeConfig(t, dataHome, "config.yaml", tc.defaultConfig)
			writeDataHomeConfig(t, dataHome, "clusters/test2/config.yaml", localConfigHeader+
				"cluster:\n  listenAddress: 10.10.10.2\n")
			writeDataHomeConfig(t, dataHome, "clusters/test3/config.yaml", escapeConfig(
				"  cluster:\n    listenAddress: 10.10.10.3\n  resolver:\n    extraDomains:\n    - extra3.example.test\n",
				"test3.example.test"))

			view := resolverView(t, viewInputs{DataHome: dataHome, HostIP: "192.168.50.151", Observed: observed})

			if got := nameRows(view); !reflect.DeepEqual(got, tc.want) {
				t.Errorf("name rows:\n got  %+v\n want %+v", got, tc.want)
			}
		})
	}
}

func TestResolverViewShowsTheNamesOfTheUserIncludeFiles(t *testing.T) {
	dataHome := t.TempDir()
	writeDataHomeConfig(t, dataHome, "config.yaml", localConfigHeader+"ingress:\n  domain: educates.test\n")

	view := resolverView(t, viewInputs{DataHome: dataHome, HostIP: "192.168.50.151", Includes: []IncludeFile{
		{Name: "vm.conf", Content: []byte("# My VM\n\naddress=/vm.test/192.168.64.5\n" +
			"server=/corp.example/10.1.1.1\n  address = /a.test/b.test/10.0.0.9  \nlocal=/lan/\n")},
		{Name: ".hidden.conf", Content: []byte("address=/hidden.test/10.0.0.1\n")},
		{Name: "notes.txt", Content: []byte("address=/notes.test/10.0.0.2\n")},
		{Name: "00-first.conf", Content: []byte("address=/first.test/10.0.0.3\n")},
		{Name: "zz-blocked.conf", Content: []byte("address=/blocked.test/\naddress=/null.test/#\n")},
	}})

	want := []ResolverNameRow{
		{Name: "educates.test", Cluster: "(default)", From: "ingress.domain"},
		{Name: "first.test", Cluster: "-", From: "dnsmasq.d/00-first.conf", Note: "yours, resolves to 10.0.0.3"},
		{Name: "vm.test", Cluster: "-", From: "dnsmasq.d/vm.conf", Note: "yours, resolves to 192.168.64.5"},
		{Name: "a.test", Cluster: "-", From: "dnsmasq.d/vm.conf", Note: "yours, resolves to 10.0.0.9"},
		{Name: "b.test", Cluster: "-", From: "dnsmasq.d/vm.conf", Note: "yours, resolves to 10.0.0.9"},
		{Name: "blocked.test", Cluster: "-", From: "dnsmasq.d/zz-blocked.conf", Note: "yours, answered as no such domain"},
		{Name: "null.test", Cluster: "-", From: "dnsmasq.d/zz-blocked.conf", Note: "yours, resolves to 0.0.0.0 and ::"},
	}
	if !reflect.DeepEqual(view.Names, want) {
		t.Errorf("name rows:\n got  %+v\n want %+v", view.Names, want)
	}
}

func TestResolverViewNotesHowNamesOverlap(t *testing.T) {
	dataHome := t.TempDir()
	writeDataHomeConfig(t, dataHome, "config.yaml", localConfigHeader+"ingress:\n  domain: educates.test\n"+
		"resolver:\n  extraDomains:\n  - foo.educates.test\n  - example.test\n")
	writeDataHomeConfig(t, dataHome, "clusters/test2/config.yaml", localConfigHeader+
		"cluster:\n  listenAddress: 10.10.10.2\ningress:\n  domain: test2.educates.test\n"+
		"resolver:\n  extraDomains:\n  - api.test2.educates.test\n")

	view := resolverView(t, viewInputs{
		DataHome: dataHome,
		HostIP:   "192.168.50.151",
		Observed: []NodeContainer{controlPlane("educates-test2", "10.10.10.2", true)},
		Includes: []IncludeFile{{Name: "vm.conf", Content: []byte(
			"address=/vm.educates.test/192.168.64.5\naddress=/Example.test/192.168.64.6\n")}},
	})

	want := []ResolverNameRow{
		{Name: "educates.test", Cluster: "(default)", From: "ingress.domain"},
		{Name: "foo.educates.test", Cluster: "(default)", From: "resolver.extraDomains", Note: "covered by educates.test"},
		{Name: "example.test", Cluster: "(default)", From: "resolver.extraDomains",
			Note: "warning: dnsmasq.d/vm.conf also serves this name"},
		{Name: "test2.educates.test", Cluster: "test2", From: "ingress.domain", Note: "wins over educates.test (default)"},
		{Name: "api.test2.educates.test", Cluster: "test2", From: "resolver.extraDomains", Note: "covered by test2.educates.test"},
		{Name: "vm.educates.test", Cluster: "-", From: "dnsmasq.d/vm.conf",
			Note: "yours, resolves to 192.168.64.5; wins over educates.test (default)"},
		{Name: "Example.test", Cluster: "-", From: "dnsmasq.d/vm.conf",
			Note: "yours, resolves to 192.168.64.6; warning: the default cluster also serves this name"},
	}
	if !reflect.DeepEqual(view.Names, want) {
		t.Errorf("name rows:\n got  %+v\n want %+v", view.Names, want)
	}
}

func TestResolverViewNotesNamesNoResolverFileForwards(t *testing.T) {
	dataHome := t.TempDir()
	writeDataHomeConfig(t, dataHome, "config.yaml", localConfigHeader+"ingress:\n  domain: educates.test\n"+
		"resolver:\n  extraDomains:\n  - example.test\n")
	writeDataHomeConfig(t, dataHome, "clusters/test2/config.yaml", localConfigHeader+
		"cluster:\n  listenAddress: 10.10.10.2\ningress:\n  domain: test2.educates.test\n")
	writeDataHomeConfig(t, dataHome, "clusters/test3/config.yaml", localConfigHeader+
		"cluster:\n  listenAddress: 10.10.10.3\n")
	in := viewInputs{
		DataHome: dataHome,
		HostIP:   "192.168.50.151",
		Observed: []NodeContainer{
			controlPlane("educates-test2", "10.10.10.2", true),
			controlPlane("educates-test3", "10.10.10.3", true),
		},
		Includes: []IncludeFile{{Name: "vm.conf", Content: []byte("address=/vm.lab.test/192.168.64.5\n")}},
	}
	notForwarded := "no /etc/resolver file forwards this name"

	for _, tc := range []struct {
		name               string
		resolverFileExists func(string) bool
		wantNoted          []string
	}{
		{
			name:               "with resolver files for some domains",
			resolverFileExists: func(domain string) bool { return domain == "educates.test" || domain == "lab.test" },
			wantNoted:          []string{"example.test"},
		},
		{
			name:               "with no resolver file",
			resolverFileExists: func(string) bool { return false },
			wantNoted:          []string{"educates.test", "example.test", "test2.educates.test", "vm.lab.test"},
		},
		{
			name: "where there is no /etc/resolver",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			in.ResolverFileExists = tc.resolverFileExists

			view := resolverView(t, in)

			var noted []string
			for _, r := range view.Names {
				if strings.Contains(r.Note, notForwarded) {
					noted = append(noted, r.Name)
				}
			}
			if !reflect.DeepEqual(noted, tc.wantNoted) {
				t.Errorf("names noted as not forwarded = %q, want %q", noted, tc.wantNoted)
			}
		})
	}
}

func TestResolverViewListsTheConfigsTheResolverLeavesOut(t *testing.T) {
	dataHome := t.TempDir()
	writeDataHomeConfig(t, dataHome, "config.yaml", localConfigHeader+"ingress:\n  domain: educates.test\n")
	writeDataHomeConfig(t, dataHome, "clusters/test2/config.yaml", localConfigHeader+"cluster:\n  listenAddress: 127.0.0.1\n")

	view := resolverView(t, viewInputs{
		DataHome: dataHome,
		HostIP:   "192.168.50.151",
		Observed: []NodeContainer{controlPlane("educates-test2", "10.10.10.2", true)},
	})

	if len(view.Clusters) != 1 || view.Clusters[0].Cluster != "(default)" {
		t.Errorf("cluster rows = %+v, want the default cluster's only", view.Clusters)
	}
	if len(view.LeftOut) != 1 {
		t.Fatalf("left out %v, want test2's config only", view.LeftOut)
	}
	wantErrorMentioning(t, view.LeftOut[0], `local cluster "test2"`, "loopback")
}

func TestResolverViewRefusesADomainTwoClustersClaim(t *testing.T) {
	dataHome := t.TempDir()
	writeDataHomeConfig(t, dataHome, "config.yaml", localConfigHeader+"ingress:\n  domain: educates.test\n")
	writeDataHomeConfig(t, dataHome, "clusters/test2/config.yaml", localConfigHeader+
		"cluster:\n  listenAddress: 10.10.10.2\ningress:\n  domain: educates.test\n")
	inv := NewInventory(Observed{Nodes: []NodeContainer{controlPlane("educates-test2", "10.10.10.2", true)}}, dataHome)

	_, err := inv.ResolverView(nil, nil)

	wantErrorMentioning(t, err, "the default local cluster", `local cluster "test2"`, `"educates.test"`)
}

func TestResolverViewShowsEachClusterAddressAndWhereItComesFrom(t *testing.T) {
	test2 := localConfigHeader + "cluster:\n  listenAddress: 10.10.10.2\n"
	observed := []NodeContainer{controlPlane("educates-test2", "10.10.10.2", true)}
	for _, tc := range []struct {
		name          string
		defaultConfig string
		hostIP        string
		want          []ResolverClusterRow
	}{
		{
			name:          "a default cluster on a specific listen address",
			defaultConfig: localConfigHeader + "cluster:\n  listenAddress: 10.10.10.1\n",
			hostIP:        "192.168.50.151",
			want: []ResolverClusterRow{
				{Cluster: "(default)", Address: "10.10.10.1", AddressFrom: "listen address", Config: "config.yaml"},
				{Cluster: "test2", Address: "10.10.10.2", AddressFrom: "listen address", Config: "clusters/test2/config.yaml"},
			},
		},
		{
			name:          "a default cluster on the loopback address",
			defaultConfig: localConfigHeader,
			hostIP:        "192.168.50.151",
			want: []ResolverClusterRow{
				{Cluster: "(default)", Address: "192.168.50.151", AddressFrom: "host IP (listen address 127.0.0.1)", Config: "config.yaml"},
				{Cluster: "test2", Address: "10.10.10.2", AddressFrom: "listen address", Config: "clusters/test2/config.yaml"},
			},
		},
		{
			name:          "a default cluster on the loopback address, with the host IP unknown",
			defaultConfig: localConfigHeader,
			want: []ResolverClusterRow{
				{Cluster: "(default)", Address: "127.0.0.1", AddressFrom: "loopback, host IP unknown (listen address 127.0.0.1)", Config: "config.yaml"},
				{Cluster: "test2", Address: "10.10.10.2", AddressFrom: "listen address", Config: "clusters/test2/config.yaml"},
			},
		},
		{
			name: "a default EducatesConfig with no listen address",
			defaultConfig: escapeConfig("  resolver:\n    extraDomains:\n    - extra.example.test\n",
				"workshops.example.test"),
			hostIP: "192.168.50.151",
			want: []ResolverClusterRow{
				{Cluster: "(default)", Address: "192.168.50.151", AddressFrom: "host IP (no listen address)", Config: "config.yaml"},
				{Cluster: "test2", Address: "10.10.10.2", AddressFrom: "listen address", Config: "clusters/test2/config.yaml"},
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dataHome := t.TempDir()
			writeDataHomeConfig(t, dataHome, "config.yaml", tc.defaultConfig)
			writeDataHomeConfig(t, dataHome, "clusters/test2/config.yaml", test2)

			view := resolverView(t, viewInputs{DataHome: dataHome, HostIP: tc.hostIP, Observed: observed})

			if !reflect.DeepEqual(view.Clusters, tc.want) {
				t.Errorf("cluster rows:\n got  %+v\n want %+v", view.Clusters, tc.want)
			}
		})
	}
}
