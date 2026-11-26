package cmd

import (
	"bytes"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/educates/educates-training-platform/client-programs/pkg/localclusters"
	"github.com/educates/educates-training-platform/client-programs/pkg/router"
)

// routerStatusInventory is the inventory of a data home whose router
// address is 10.10.10.1, with the default local cluster on educates.test,
// running, and the named local cluster test2 on test2.educates.test,
// stopped, and router the router container observed, nil for none.
func routerStatusInventory(t *testing.T, router *localclusters.RouterContainer) localclusters.Inventory {
	t.Helper()
	dataHome := t.TempDir()
	writeRouterFile(t, dataHome, handWrittenRouterFile)
	writeLocalClusterListFile(t, filepath.Join(dataHome, "config.yaml"),
		"apiVersion: cli.educates.dev/v1alpha1\nkind: EducatesLocalConfig\n"+
			"ingress:\n  domain: educates.test\n"+
			"resolver:\n  extraDomains:\n  - example.test\n")
	writeLocalClusterListFile(t, filepath.Join(dataHome, "clusters", "test2", "config.yaml"),
		"apiVersion: cli.educates.dev/v1alpha1\nkind: EducatesLocalConfig\n"+
			"ingress:\n  domain: test2.educates.test\n")
	return updateInventory(dataHome, []localclusters.NodeContainer{
		{Name: "educates-control-plane", KindCluster: "educates", Role: "control-plane", Running: true},
		{Name: "educates-test2-control-plane", KindCluster: "educates-test2", Role: "control-plane"},
	}, router)
}

// printRouterStatusForTest prints the router status of inventory, and
// returns what it wrote and warned.
func printRouterStatusForTest(t *testing.T, inventory localclusters.Inventory) (string, string) {
	t.Helper()
	status, err := inventory.RouterStatus(router.Image)
	if err != nil {
		t.Fatalf("RouterStatus: %v", err)
	}
	var out, errOut bytes.Buffer
	printLocalRouterStatus(&out, &errOut, status)
	t.Logf("local router status:\n%s", out.String())
	return out.String(), errOut.String()
}

// fieldLines returns the fields of each line of s.
func fieldLines(s string) []string {
	var lines []string
	for _, line := range strings.Split(strings.TrimRight(s, "\n"), "\n") {
		lines = append(lines, strings.Join(strings.Fields(line), " "))
	}
	return lines
}

func TestLocalRouterStatusPrintsTheRouterAndEveryRouteOfTheDefaultAndANamedCluster(t *testing.T) {
	out, errOut := printRouterStatusForTest(t, routerStatusInventory(t, routerOn(router.Image, true)))

	want := []string{
		"Router address: 10.10.10.1",
		"Router: running (educates-router), publishing ports 80 and 443 on 10.10.10.1",
		"Image: " + router.Image,
		"",
		"NAME CLUSTER TLS STATE",
		"educates.test (default) passthrough running",
		"example.test (default) passthrough running",
		"test2.educates.test test2 passthrough stopped",
	}
	if got := fieldLines(out); !slices.Equal(got, want) {
		t.Errorf("lines:\n got  %q\n want %q", got, want)
	}
	if errOut != "" {
		t.Errorf("got warnings %q, want none", errOut)
	}
}

func TestLocalRouterStatusSaysWhenTheRouterIsNotRunning(t *testing.T) {
	for name, tc := range map[string]struct {
		router     *localclusters.RouterContainer
		wantRouter string
		wantAdvice []string
	}{
		"stopped": {
			router:     routerOn(router.Image, false),
			wantRouter: "Router: stopped (educates-router)",
			wantAdvice: []string{"not running", "educates local router update"},
		},
		"missing": {
			wantRouter: "Router: missing",
			wantAdvice: []string{"missing", "educates local router update"},
		},
	} {
		t.Run(name, func(t *testing.T) {
			out, _ := printRouterStatusForTest(t, routerStatusInventory(t, tc.router))

			lines := fieldLines(out)
			if !slices.Contains(lines, tc.wantRouter) {
				t.Errorf("lines %q do not hold %q", lines, tc.wantRouter)
			}
			for _, part := range tc.wantAdvice {
				if !strings.Contains(out, part) {
					t.Errorf("status does not mention %q", part)
				}
			}
			if !slices.Contains(lines, "test2.educates.test test2 passthrough stopped") {
				t.Errorf("lines %q do not hold test2's route", lines)
			}
		})
	}
}

func TestLocalRouterStatusWarnsAboutARouterAddressThatCannotServe(t *testing.T) {
	inventory := routerStatusInventory(t, routerOn(router.Image, true))
	dataHome := filepath.Dir(inventory.Clusters()[0].ConfigPath)
	writeRouterFile(t, dataHome, "apiVersion: cli.educates.dev/v1alpha1\nkind: EducatesLocalRouter\naddress: 10.10.10.9\n")

	out, errOut := printRouterStatusForTest(t, inventory)

	if !slices.Contains(fieldLines(out), "Router address: 10.10.10.9") {
		t.Errorf("status:\n%s\nwant the router address 10.10.10.9", out)
	}
	for _, part := range []string{"Warning:", "10.10.10.9", "no network interface"} {
		if !strings.Contains(errOut, part) {
			t.Errorf("warnings %q do not mention %q", errOut, part)
		}
	}
}
