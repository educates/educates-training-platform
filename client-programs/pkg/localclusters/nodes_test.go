package localclusters

import (
	"slices"
	"testing"
)

func TestRegistryNodes(t *testing.T) {
	dataHome := t.TempDir()
	test2, err := Named("test2", dataHome)
	if err != nil {
		t.Fatalf("Named: %v", err)
	}

	cases := []struct {
		name     string
		id       Identity
		observed []NodeContainer
		want     []string
	}{
		{
			name: "default single node",
			id:   Default(dataHome),
			observed: []NodeContainer{
				{Name: "educates-control-plane", KindCluster: "educates", Role: "control-plane", Running: true},
			},
			want: []string{"educates-control-plane"},
		},
		{
			name: "default with workers, listed newest first",
			id:   Default(dataHome),
			observed: []NodeContainer{
				{Name: "educates-worker10", KindCluster: "educates", Role: "worker", Running: true},
				{Name: "educates-worker2", KindCluster: "educates", Role: "worker", Running: true},
				{Name: "educates-worker", KindCluster: "educates", Role: "worker", Running: true},
				{Name: "educates-control-plane", KindCluster: "educates", Role: "control-plane", Running: true},
			},
			want: []string{"educates-control-plane", "educates-worker", "educates-worker2", "educates-worker10"},
		},
		{
			name: "other clusters' nodes left out",
			id:   test2,
			observed: []NodeContainer{
				{Name: "educates-control-plane", KindCluster: "educates", Role: "control-plane", Running: true},
				{Name: "educates-worker", KindCluster: "educates", Role: "worker", Running: true},
				{Name: "educates-test2-worker", KindCluster: "educates-test2", Role: "worker", Running: true},
				{Name: "educates-test2-control-plane", KindCluster: "educates-test2", Role: "control-plane", Running: true},
				{Name: "educates-test2-x-control-plane", KindCluster: "educates-test2-x", Role: "control-plane", Running: true},
			},
			want: []string{"educates-test2-control-plane", "educates-test2-worker"},
		},
		{
			name: "default cluster beside a named cluster called worker",
			id:   Default(dataHome),
			observed: []NodeContainer{
				{Name: "educates-worker-control-plane", KindCluster: "educates-worker", Role: "control-plane", Running: true},
				{Name: "educates-control-plane", KindCluster: "educates", Role: "control-plane", Running: true},
			},
			want: []string{"educates-control-plane"},
		},
		{
			name: "several control planes leave out the load balancer",
			id:   test2,
			observed: []NodeContainer{
				{Name: "educates-test2-external-load-balancer", KindCluster: "educates-test2", Role: "external-load-balancer", Running: true},
				{Name: "educates-test2-worker", KindCluster: "educates-test2", Role: "worker", Running: true},
				{Name: "educates-test2-control-plane2", KindCluster: "educates-test2", Role: "control-plane", Running: true},
				{Name: "educates-test2-control-plane", KindCluster: "educates-test2", Role: "control-plane", Running: true},
			},
			want: []string{"educates-test2-control-plane", "educates-test2-control-plane2", "educates-test2-worker"},
		},
		{
			name: "stopped nodes still listed",
			id:   Default(dataHome),
			observed: []NodeContainer{
				{Name: "educates-worker", KindCluster: "educates", Role: "worker", Running: false},
				{Name: "educates-control-plane", KindCluster: "educates", Role: "control-plane", Running: true},
			},
			want: []string{"educates-control-plane", "educates-worker"},
		},
		{
			name: "no nodes observed",
			id:   Default(dataHome),
			want: nil,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.id.RegistryNodes(tc.observed); !slices.Equal(got, tc.want) {
				t.Errorf("RegistryNodes() = %v, want %v", got, tc.want)
			}
		})
	}
}

// haCluster is the observed state of the named cluster test2 with two
// control planes, two workers and the load balancer, listed newest first,
// beside the default cluster. running names the containers that run.
func haCluster(running ...string) []NodeContainer {
	nodes := []NodeContainer{
		{Name: "educates-test2-external-load-balancer", KindCluster: "educates-test2", Role: "external-load-balancer"},
		{Name: "educates-test2-worker2", KindCluster: "educates-test2", Role: "worker"},
		{Name: "educates-test2-worker", KindCluster: "educates-test2", Role: "worker"},
		{Name: "educates-test2-control-plane2", KindCluster: "educates-test2", Role: "control-plane"},
		{Name: "educates-test2-control-plane", KindCluster: "educates-test2", Role: "control-plane"},
		{Name: "educates-control-plane", KindCluster: "educates", Role: "control-plane", Running: true},
	}
	for i := range nodes {
		if slices.Contains(running, nodes[i].Name) {
			nodes[i].Running = true
		}
	}
	return nodes
}

func TestNodesToStart(t *testing.T) {
	test2, err := Named("test2", t.TempDir())
	if err != nil {
		t.Fatalf("Named: %v", err)
	}

	cases := []struct {
		name     string
		observed []NodeContainer
		want     []string
	}{
		{
			name:     "stopped cluster starts every node, load balancer included",
			observed: haCluster(),
			want: []string{
				"educates-test2-control-plane",
				"educates-test2-control-plane2",
				"educates-test2-worker",
				"educates-test2-worker2",
				"educates-test2-external-load-balancer",
			},
		},
		{
			name:     "partly running cluster starts the nodes that are not running",
			observed: haCluster("educates-test2-control-plane", "educates-test2-worker", "educates-test2-external-load-balancer"),
			want:     []string{"educates-test2-control-plane2", "educates-test2-worker2"},
		},
		{
			name: "running cluster starts nothing",
			observed: haCluster(
				"educates-test2-control-plane",
				"educates-test2-control-plane2",
				"educates-test2-worker",
				"educates-test2-worker2",
				"educates-test2-external-load-balancer",
			),
			want: nil,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := test2.NodesToStart(tc.observed); !slices.Equal(got, tc.want) {
				t.Errorf("NodesToStart() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestNodesToStop(t *testing.T) {
	test2, err := Named("test2", t.TempDir())
	if err != nil {
		t.Fatalf("Named: %v", err)
	}

	cases := []struct {
		name     string
		observed []NodeContainer
		want     []string
	}{
		{
			name: "running cluster stops every node, control planes last",
			observed: haCluster(
				"educates-test2-control-plane",
				"educates-test2-control-plane2",
				"educates-test2-worker",
				"educates-test2-worker2",
				"educates-test2-external-load-balancer",
			),
			want: []string{
				"educates-test2-external-load-balancer",
				"educates-test2-worker2",
				"educates-test2-worker",
				"educates-test2-control-plane2",
				"educates-test2-control-plane",
			},
		},
		{
			name:     "partly running cluster stops the nodes that run",
			observed: haCluster("educates-test2-control-plane", "educates-test2-worker2"),
			want:     []string{"educates-test2-worker2", "educates-test2-control-plane"},
		},
		{
			name:     "stopped cluster stops nothing",
			observed: haCluster(),
			want:     nil,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := test2.NodesToStop(tc.observed); !slices.Equal(got, tc.want) {
				t.Errorf("NodesToStop() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestState(t *testing.T) {
	test2, err := Named("test2", t.TempDir())
	if err != nil {
		t.Fatalf("Named: %v", err)
	}

	cases := []struct {
		name     string
		observed []NodeContainer
		want     State
	}{
		{
			name: "every node running",
			observed: haCluster(
				"educates-test2-control-plane",
				"educates-test2-control-plane2",
				"educates-test2-worker",
				"educates-test2-worker2",
				"educates-test2-external-load-balancer",
			),
			want: Running,
		},
		{
			name:     "no node running, while another cluster runs",
			observed: haCluster(),
			want:     Stopped,
		},
		{
			name:     "a stopped worker",
			observed: haCluster("educates-test2-control-plane", "educates-test2-control-plane2", "educates-test2-worker", "educates-test2-external-load-balancer"),
			want:     PartlyRunning,
		},
		{
			name: "only the load balancer stopped",
			observed: haCluster(
				"educates-test2-control-plane",
				"educates-test2-control-plane2",
				"educates-test2-worker",
				"educates-test2-worker2",
			),
			want: PartlyRunning,
		},
		{
			name: "single node running",
			observed: []NodeContainer{
				{Name: "educates-test2-control-plane", KindCluster: "educates-test2", Role: "control-plane", Running: true},
			},
			want: Running,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := test2.State(tc.observed); got != tc.want {
				t.Errorf("State() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestStateNames(t *testing.T) {
	for state, want := range map[State]string{
		Running:       "running",
		Stopped:       "stopped",
		PartlyRunning: "partly running",
	} {
		if string(state) != want {
			t.Errorf("state %q, want %q", state, want)
		}
	}
}
