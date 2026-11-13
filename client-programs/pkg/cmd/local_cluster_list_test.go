package cmd

import (
	"bytes"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/educates/educates-training-platform/client-programs/pkg/localclusters"
)

// listControlPlane is the control-plane node container of the kind
// cluster kindName, configured to publish ports 80 and 443 on hostIP.
func listControlPlane(kindName, hostIP string, running bool) localclusters.NodeContainer {
	addr := netip.MustParseAddr(hostIP)
	return localclusters.NodeContainer{
		Name:        kindName + "-control-plane",
		KindCluster: kindName,
		Role:        "control-plane",
		Running:     running,
		PortBindings: []localclusters.PortBinding{
			{HostIP: addr, HostPort: 80},
			{HostIP: addr, HostPort: 443},
		},
	}
}

func TestLocalClusterListShowsEveryEducatesClusterWithTheDefaultMarked(t *testing.T) {
	dataHome := t.TempDir()
	writeLocalClusterListFile(t, filepath.Join(dataHome, "config.yaml"),
		"apiVersion: cli.educates.dev/v1alpha1\nkind: EducatesLocalConfig\ncluster:\n  listenAddress: 10.10.10.1\ningress:\n  domain: educates.test\n")
	writeLocalClusterListFile(t, filepath.Join(dataHome, "clusters", "test2", "config.yaml"),
		"apiVersion: cli.educates.dev/v1alpha1\nkind: EducatesLocalConfig\ncluster:\n  listenAddress: 10.10.10.2\n")
	inventory := localclusters.NewInventory(localclusters.Observed{Nodes: []localclusters.NodeContainer{
		listControlPlane("educates-test2", "10.10.10.2", false),
		listControlPlane("educates-x", "10.10.10.9", true),
		listControlPlane("educates", "10.10.10.1", true),
	}}, dataHome)
	var out, errOut bytes.Buffer

	printLocalClusterList(&out, &errOut, inventory.List())

	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 3 {
		t.Fatalf("got %d lines, want a header and 2 rows:\n%s", len(lines), out.String())
	}
	wantRows := [][]string{
		{"NAME", "STATE", "LISTEN", "IP", "DOMAIN", "CONTEXT"},
		{"(default)", "running", "10.10.10.1", "educates.test", "kind-educates"},
		{"test2", "stopped", "10.10.10.2", "10-10-10-2.nip.io", "kind-educates-test2"},
	}
	for i, want := range wantRows {
		if got := strings.Fields(lines[i]); strings.Join(got, " ") != strings.Join(want, " ") {
			t.Errorf("line %d = %q, want fields %q", i, lines[i], want)
		}
	}
	if errOut.Len() != 0 {
		t.Errorf("got warnings %q, want none", errOut.String())
	}
}

func TestLocalClusterListWarnsAboutAConfigThatDoesNotLoad(t *testing.T) {
	dataHome := t.TempDir()
	writeLocalClusterListFile(t, filepath.Join(dataHome, "clusters", "test2", "config.yaml"),
		"apiVersion: cli.educates.dev/v1alpha1\nkind: EducatesLocalConfig\ncluster:\n  listenAddress: 127.0.0.1\n")
	inventory := localclusters.NewInventory(localclusters.Observed{Nodes: []localclusters.NodeContainer{
		listControlPlane("educates-test2", "10.10.10.2", true),
	}}, dataHome)
	var out, errOut bytes.Buffer

	printLocalClusterList(&out, &errOut, inventory.List())

	if got := strings.Fields(strings.Split(strings.TrimSpace(out.String()), "\n")[1]); strings.Join(got, " ") !=
		"test2 running 10.10.10.2 - kind-educates-test2" {
		t.Errorf("row = %q, want the domain shown as -", got)
	}
	for _, part := range []string{`local cluster "test2"`, "loopback"} {
		if !strings.Contains(errOut.String(), part) {
			t.Errorf("warnings %q do not mention %q", errOut.String(), part)
		}
	}
}

func TestLocalClusterListWithNoCluster(t *testing.T) {
	var out, errOut bytes.Buffer

	printLocalClusterList(&out, &errOut, localclusters.NewInventory(localclusters.Observed{}, t.TempDir()).List())

	if !strings.Contains(out.String(), "No local clusters") {
		t.Errorf("output %q does not say there are no local clusters", out.String())
	}
}

// writeLocalClusterListFile writes body to path, creating its folder.
func writeLocalClusterListFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}
