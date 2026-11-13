package localclusters

import (
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
)

func TestDefaultIdentity(t *testing.T) {
	dataHome := t.TempDir()

	id := Default(dataHome)

	want := Identity{
		Name:           "",
		KindName:       "educates",
		Context:        "kind-educates",
		ConfigPath:     filepath.Join(dataHome, "config.yaml"),
		KindConfigPath: filepath.Join(dataHome, "educates-cluster-config.yaml"),
	}
	if id != want {
		t.Fatalf("Default(%q) = %+v, want %+v", dataHome, id, want)
	}
	if !id.IsDefault() {
		t.Errorf("IsDefault() = false, want true")
	}
	if got := id.ControlPlaneNode(); got != "educates-control-plane" {
		t.Errorf("ControlPlaneNode() = %q, want %q", got, "educates-control-plane")
	}
}

func TestNamedIdentity(t *testing.T) {
	dataHome := t.TempDir()

	id, err := Named("test2", dataHome)
	if err != nil {
		t.Fatalf("Named: %v", err)
	}

	want := Identity{
		Name:           "test2",
		KindName:       "educates-test2",
		Context:        "kind-educates-test2",
		ConfigPath:     filepath.Join(dataHome, "clusters", "test2", "config.yaml"),
		KindConfigPath: filepath.Join(dataHome, "clusters", "test2", "educates-cluster-config.yaml"),
	}
	if id != want {
		t.Fatalf("Named(test2) = %+v, want %+v", id, want)
	}
	if id.IsDefault() {
		t.Errorf("IsDefault() = true, want false")
	}
	if got := id.ControlPlaneNode(); got != "educates-test2-control-plane" {
		t.Errorf("ControlPlaneNode() = %q, want %q", got, "educates-test2-control-plane")
	}
}

func TestNodeNames(t *testing.T) {
	dataHome := t.TempDir()
	test2, err := Named("test2", dataHome)
	if err != nil {
		t.Fatalf("Named: %v", err)
	}

	cases := []struct {
		name  string
		id    Identity
		roles []string
		want  []string
	}{
		{
			name: "default single node",
			id:   Default(dataHome),
			want: []string{"educates-control-plane"},
		},
		{
			name: "named single node",
			id:   test2,
			want: []string{"educates-test2-control-plane"},
		},
		{
			name:  "default with workers",
			id:    Default(dataHome),
			roles: []string{"control-plane", "worker", "worker"},
			want:  []string{"educates-control-plane", "educates-worker", "educates-worker2"},
		},
		{
			name:  "named with workers",
			id:    test2,
			roles: []string{"control-plane", "worker", "worker", "worker"},
			want: []string{
				"educates-test2-control-plane",
				"educates-test2-worker",
				"educates-test2-worker2",
				"educates-test2-worker3",
			},
		},
		{
			name:  "several control planes add the load balancer",
			id:    test2,
			roles: []string{"control-plane", "control-plane", "worker"},
			want: []string{
				"educates-test2-control-plane",
				"educates-test2-control-plane2",
				"educates-test2-worker",
				"educates-test2-external-load-balancer",
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.id.NodeNames(tc.roles); !slices.Equal(got, tc.want) {
				t.Errorf("NodeNames(%v) = %v, want %v", tc.roles, got, tc.want)
			}
		})
	}
}

func TestLongestNameKeepsNodeNamesWithinAHostname(t *testing.T) {
	id, err := Named(strings.Repeat("a", 31), t.TempDir())
	if err != nil {
		t.Fatalf("Named: %v", err)
	}
	for _, node := range id.NodeNames([]string{"control-plane", "control-plane", "control-plane", "worker"}) {
		if len(node) > 63 {
			t.Errorf("node container name %q is %d characters, longer than 63", node, len(node))
		}
	}
}

func TestNamedRejectsInvalidNames(t *testing.T) {
	for _, name := range []string{
		"",
		"Test2",
		"test_2",
		"test.2",
		"-test",
		"test-",
		"te st",
		"tëst",
		strings.Repeat("a", 32),
	} {
		t.Run(name, func(t *testing.T) {
			_, err := Named(name, t.TempDir())
			if err == nil {
				t.Fatalf("Named(%q) succeeded, want an error", name)
			}
			msg := err.Error()
			for _, rule := range []string{strconv.Quote(name), "lowercase", "31 characters"} {
				if !strings.Contains(msg, rule) {
					t.Errorf("error %q does not mention %q", msg, rule)
				}
			}
		})
	}
}

func TestNamedAcceptsValidNames(t *testing.T) {
	for _, name := range []string{
		"a",
		"7",
		"test2",
		"2nd-cluster",
		"a-b-c",
		strings.Repeat("a", 31),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := Named(name, t.TempDir()); err != nil {
				t.Fatalf("Named(%q): %v", name, err)
			}
		})
	}
}
