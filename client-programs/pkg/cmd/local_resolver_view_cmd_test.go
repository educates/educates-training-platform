package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/educates/educates-training-platform/client-programs/pkg/config"
	"github.com/educates/educates-training-platform/client-programs/pkg/localclusters"
)

// twoClusterDataHome writes a data home with a default cluster on
// 10.10.10.1 with two extra domains, a named cluster test2 on 10.10.10.2
// and an include file with one address= line, and returns it with the
// inventory of both clusters running.
func twoClusterDataHome(t *testing.T) (string, localclusters.Inventory) {
	t.Helper()
	dataHome := t.TempDir()
	writeLocalClusterListFile(t, filepath.Join(dataHome, "config.yaml"),
		"apiVersion: cli.educates.dev/v1alpha1\nkind: EducatesLocalConfig\n"+
			"cluster:\n  listenAddress: 10.10.10.1\ningress:\n  domain: educates.test\n"+
			"resolver:\n  extraDomains:\n  - example.test\n  - workshops.lab.test\n")
	writeLocalClusterListFile(t, filepath.Join(dataHome, "clusters", "test2", "config.yaml"),
		"apiVersion: cli.educates.dev/v1alpha1\nkind: EducatesLocalConfig\n"+
			"cluster:\n  listenAddress: 10.10.10.2\ningress:\n  domain: test2.educates.test\n")
	writeLocalClusterListFile(t, filepath.Join(config.DnsmasqIncludeDir(dataHome), "vm.conf"),
		"# The lab VM\naddress=/vm.lab.test/192.168.64.5\n")
	inventory := localclusters.NewInventory(localclusters.Observed{
		Nodes: []localclusters.NodeContainer{
			listControlPlane("educates", "10.10.10.1", true),
			listControlPlane("educates-test2", "10.10.10.2", true),
		},
	}, dataHome)
	return dataHome, inventory
}

func TestLocalResolverViewPrintsTheClustersAndTheNames(t *testing.T) {
	dataHome, inventory := twoClusterDataHome(t)
	includes, err := readResolverIncludeFiles(config.DnsmasqIncludeDir(dataHome))
	if err != nil {
		t.Fatalf("readResolverIncludeFiles: %v", err)
	}
	view, err := inventory.ResolverView(includes, func(domain string) bool { return domain == "educates.test" })
	if err != nil {
		t.Fatalf("ResolverView: %v", err)
	}
	var out, errOut bytes.Buffer

	printLocalResolverView(&out, &errOut, view)

	t.Logf("local resolver view:\n%s", out.String())
	wantTables := [][]string{
		{"CLUSTER", "ADDRESS", "CONFIG"},
		{"(default)", "10.10.10.1", "config.yaml"},
		{"test2", "10.10.10.2", "clusters/test2/config.yaml"},
		{},
		{"NAME", "CLUSTER", "FROM", "NOTE"},
		{"educates.test", "(default)", "ingress.domain", "-"},
		{"example.test", "(default)", "resolver.extraDomains", "no /etc/resolver file forwards this name"},
		{"workshops.lab.test", "(default)", "resolver.extraDomains", "no /etc/resolver file forwards this name"},
		{"test2.educates.test", "test2", "ingress.domain", "wins over educates.test (default)"},
		{"vm.lab.test", "-", "dnsmasq.d/vm.conf", "yours, resolves to 192.168.64.5; no /etc/resolver file forwards this name"},
	}
	lines := strings.Split(strings.TrimRight(out.String(), "\n"), "\n")
	if len(lines) != len(wantTables) {
		t.Fatalf("got %d lines, want %d:\n%s", len(lines), len(wantTables), out.String())
	}
	for i, want := range wantTables {
		if got := strings.Fields(lines[i]); strings.Join(got, " ") != strings.Join(strings.Fields(strings.Join(want, " ")), " ") {
			t.Errorf("line %d = %q, want fields %q", i, lines[i], want)
		}
	}
	if errOut.Len() != 0 {
		t.Errorf("got warnings %q, want none", errOut.String())
	}
}

func TestLocalResolverConfigServesTwoClustersThenTheIncludeFolder(t *testing.T) {
	_, inventory := twoClusterDataHome(t)

	content, leftOut, err := inventory.ResolverConfig()
	if err != nil || len(leftOut) != 0 {
		t.Fatalf("ResolverConfig: left out %v, error %v", leftOut, err)
	}

	t.Logf("dnsmasq.conf:\n%s", content)
	want := `#log-queries
no-resolv
server=1.0.0.1
server=1.1.1.1
strict-order

# Default local cluster (config.yaml): 10.10.10.1, its listen address
address=/educates.test/10.10.10.1
address=/example.test/10.10.10.1
address=/workshops.lab.test/10.10.10.1

# Local cluster test2 (clusters/test2/config.yaml): 10.10.10.2, its listen address
address=/test2.educates.test/10.10.10.2

# Your own entries: dnsmasq.d/*.conf
conf-dir=/etc/dnsmasq.d/,*.conf
`
	if got := string(content); got != want {
		t.Errorf("dnsmasq.conf:\n%s\nwant:\n%s", got, want)
	}
}

func TestLocalResolverViewWarnsAboutTheConfigsItLeavesOutAfterTheTables(t *testing.T) {
	dataHome, inventory := twoClusterDataHome(t)
	writeLocalClusterListFile(t, filepath.Join(dataHome, "clusters", "test2", "config.yaml"),
		"apiVersion: cli.educates.dev/v1alpha1\nkind: EducatesLocalConfig\ncluster:\n  listenAddress: 127.0.0.1\n")
	view, err := inventory.ResolverView(nil, nil)
	if err != nil {
		t.Fatalf("ResolverView: %v", err)
	}
	var out, errOut bytes.Buffer

	printLocalResolverView(&out, &errOut, view)

	if strings.Contains(out.String(), "test2") {
		t.Errorf("view shows test2, whose config the resolver leaves out:\n%s", out.String())
	}
	for _, part := range []string{"Warning:", `local cluster "test2"`, "loopback"} {
		if !strings.Contains(errOut.String(), part) {
			t.Errorf("warnings %q do not mention %q", errOut.String(), part)
		}
	}
}

func TestReadResolverIncludeFilesReadsTheConfFilesOfTheFolder(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "dnsmasq.d")
	writeLocalClusterListFile(t, filepath.Join(dir, "vm.conf"), "address=/vm.test/192.168.64.5\n")
	writeLocalClusterListFile(t, filepath.Join(dir, "notes.txt"), "address=/notes.test/10.0.0.1\n")
	if err := os.MkdirAll(filepath.Join(dir, "sub.conf"), 0o755); err != nil {
		t.Fatal(err)
	}

	files, err := readResolverIncludeFiles(dir)
	if err != nil {
		t.Fatalf("readResolverIncludeFiles: %v", err)
	}

	if len(files) != 1 || files[0].Name != "vm.conf" || string(files[0].Content) != "address=/vm.test/192.168.64.5\n" {
		t.Errorf("read %+v, want vm.conf only", files)
	}
}

func TestReadResolverIncludeFilesReadsNoneFromAMissingFolder(t *testing.T) {
	files, err := readResolverIncludeFiles(filepath.Join(t.TempDir(), "dnsmasq.d"))

	if err != nil || len(files) != 0 {
		t.Errorf("readResolverIncludeFiles = %+v, %v; want none and no error", files, err)
	}
}
