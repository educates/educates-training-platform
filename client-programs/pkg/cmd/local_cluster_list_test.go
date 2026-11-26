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

func TestLocalClusterListShowsTheRouterAddressAndEveryEducatesClusterWithTheDefaultMarked(t *testing.T) {
	dataHome := t.TempDir()
	writeRouterFile(t, dataHome, handWrittenRouterFile)
	writeLocalClusterListFile(t, filepath.Join(dataHome, "config.yaml"),
		"apiVersion: cli.educates.dev/v1alpha1\nkind: EducatesLocalConfig\ningress:\n  domain: educates.test\n")
	writeLocalClusterListFile(t, filepath.Join(dataHome, "clusters", "test2", "config.yaml"),
		"apiVersion: cli.educates.dev/v1alpha1\nkind: EducatesLocalConfig\ningress:\n  domain: test2.educates.test\n")
	defaultNode := listControlPlane("educates", "10.10.10.1", true)
	defaultNode.KubernetesVersion = "v1.36.4"
	inventory := localclusters.NewInventory(localclusters.Observed{Nodes: []localclusters.NodeContainer{
		listControlPlane("educates-test2", "10.10.10.2", false),
		{Name: "educates-test2-worker", KindCluster: "educates-test2", Role: "worker"},
		{Name: "educates-test2-worker2", KindCluster: "educates-test2", Role: "worker"},
		listControlPlane("educates-x", "10.10.10.9", true),
		defaultNode,
	}}, dataHome)
	var out, errOut bytes.Buffer

	printLocalClusterList(&out, &errOut, inventory)

	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 5 || lines[0] != "Router address: 10.10.10.1" || lines[1] != "" {
		t.Fatalf("got\n%s\nwant the router address, an empty line, a header and 2 rows", out.String())
	}
	lines = lines[2:]
	wantRows := [][]string{
		{"NAME", "STATE", "NODES", "KUBERNETES", "DOMAIN", "CONTEXT"},
		{"(default)", "running", "1", "v1.36.4", "educates.test", "kind-educates"},
		{"test2", "stopped", "3", "-", "test2.educates.test", "kind-educates-test2"},
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
	writeRouterFile(t, dataHome, handWrittenRouterFile)
	writeLocalClusterListFile(t, filepath.Join(dataHome, "clusters", "test2", "config.yaml"),
		"apiVersion: cli.educates.dev/v1alpha9\nkind: EducatesLocalConfig\n")
	inventory := localclusters.NewInventory(localclusters.Observed{Nodes: []localclusters.NodeContainer{
		listControlPlane("educates-test2", "10.10.10.2", true),
	}}, dataHome)
	var out, errOut bytes.Buffer

	printLocalClusterList(&out, &errOut, inventory)

	if got := strings.Fields(strings.Split(strings.TrimSpace(out.String()), "\n")[3]); strings.Join(got, " ") !=
		"test2 running 1 - - kind-educates-test2" {
		t.Errorf("row = %q, want the unread version and the domain shown as -", got)
	}
	for _, part := range []string{`local cluster "test2"`, "unsupported apiVersion"} {
		if !strings.Contains(errOut.String(), part) {
			t.Errorf("warnings %q do not mention %q", errOut.String(), part)
		}
	}
}

func TestLocalClusterListWarnsWithoutARouterAddress(t *testing.T) {
	dataHome := t.TempDir()
	writeLocalClusterListFile(t, filepath.Join(dataHome, "config.yaml"),
		"apiVersion: cli.educates.dev/v1alpha1\nkind: EducatesLocalConfig\ningress:\n  domain: educates.test\n")
	inventory := localclusters.NewInventory(localclusters.Observed{Nodes: []localclusters.NodeContainer{
		listControlPlane("educates", "10.10.10.1", true),
	}}, dataHome)
	var out, errOut bytes.Buffer

	printLocalClusterList(&out, &errOut, inventory)

	if first := strings.Split(out.String(), "\n")[0]; first != "Router address: -" {
		t.Errorf("first line = %q, want the router address shown as -", first)
	}
	for _, part := range []string{"router.yaml", "educates local router update --address <ip>"} {
		if !strings.Contains(errOut.String(), part) {
			t.Errorf("warnings %q do not mention %q", errOut.String(), part)
		}
	}
}

func TestLocalClusterListWithNoCluster(t *testing.T) {
	var out, errOut bytes.Buffer

	printLocalClusterList(&out, &errOut, localclusters.NewInventory(localclusters.Observed{}, t.TempDir()))

	if !strings.Contains(out.String(), "No local clusters") {
		t.Errorf("output %q does not say there are no local clusters", out.String())
	}
	if errOut.Len() != 0 {
		t.Errorf("got warnings %q, want none with no local cluster to reach", errOut.String())
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
