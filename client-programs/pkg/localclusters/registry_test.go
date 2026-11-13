package localclusters

import (
	"slices"
	"testing"
)

// sharedInfrastructureHost is a host running the default local cluster
// with a worker, the stopped named cluster test2 with two control planes
// behind kind's load balancer and a worker, and two kind clusters that are
// not Educates clusters.
func sharedInfrastructureHost(t *testing.T) (Observed, string) {
	t.Helper()
	dataHome := t.TempDir()
	addClusterFolder(t, dataHome, "test2")
	return Observed{Nodes: []NodeContainer{
		{Name: "educates-test2-worker", KindCluster: "educates-test2", Role: "worker"},
		{Name: "educates-test2-external-load-balancer", KindCluster: "educates-test2", Role: "external-load-balancer"},
		{Name: "educates-test2-control-plane2", KindCluster: "educates-test2", Role: "control-plane"},
		controlPlane("educates-test2", "10.10.10.2", false),
		controlPlane("educates-x", "10.10.10.3", true),
		controlPlane("kind", "0.0.0.0", true),
		{Name: "educates-worker", KindCluster: "educates", Role: "worker", Running: true},
		controlPlane("educates", "10.10.10.1", true),
	}}, dataHome
}

func TestRegistryNodesAreEveryNodeOfEveryLocalCluster(t *testing.T) {
	observed, dataHome := sharedInfrastructureHost(t)

	got := NewInventory(observed, dataHome).RegistryNodes()

	want := []string{
		"educates-control-plane", "educates-worker",
		"educates-test2-control-plane", "educates-test2-control-plane2", "educates-test2-worker",
	}
	if !slices.Equal(got, want) {
		t.Errorf("RegistryNodes() = %v, want %v", got, want)
	}
}

func TestRegistryNodesOfAHostWithoutLocalClusters(t *testing.T) {
	observed := Observed{Nodes: []NodeContainer{controlPlane("kind", "0.0.0.0", true)}}

	if got := NewInventory(observed, t.TempDir()).RegistryNodes(); len(got) != 0 {
		t.Errorf("RegistryNodes() = %v, want none", got)
	}
}

func TestRegistryTargetsAreEveryLocalClusterWithItsNodes(t *testing.T) {
	observed, dataHome := sharedInfrastructureHost(t)

	got := NewInventory(observed, dataHome).RegistryTargets()

	want := []struct {
		context string
		state   State
		nodes   []string
	}{
		{"kind-educates", Running, []string{"educates-control-plane", "educates-worker"}},
		{"kind-educates-test2", Stopped,
			[]string{"educates-test2-control-plane", "educates-test2-control-plane2", "educates-test2-worker"}},
	}
	if len(got) != len(want) {
		t.Fatalf("RegistryTargets() returned %d targets, want %d: %+v", len(got), len(want), got)
	}
	for i, w := range want {
		g := got[i]
		if g.Identity.Context != w.context || g.State != w.state || !slices.Equal(g.Nodes, w.nodes) {
			t.Errorf("target %d = {context %q, state %q, nodes %v}, want %+v",
				i, g.Identity.Context, g.State, g.Nodes, w)
		}
	}
}
