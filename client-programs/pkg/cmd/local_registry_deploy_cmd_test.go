package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/educates/educates-training-platform/client-programs/pkg/localclusters"
)

func TestLocalRegistryDeployStillAcceptsItsDeprecatedKubeOptions(t *testing.T) {
	c := (&ProjectInfo{}).NewLocalRegistryDeployCmd()

	if err := c.ParseFlags([]string{"--kubeconfig", "/tmp/kubeconfig", "--context", "kind-educates"}); err != nil {
		t.Fatalf("ParseFlags() = %v, want the deprecated options accepted", err)
	}
	for _, name := range []string{"kubeconfig", "context"} {
		f := c.Flags().Lookup(name)
		if f == nil {
			t.Fatalf("--%s is missing", name)
		}
		for _, part := range []string{"still", "every local cluster", "removed"} {
			if !strings.Contains(f.Deprecated, part) {
				t.Errorf("--%s deprecation message = %q, want it to mention %q", name, f.Deprecated, part)
			}
		}
	}
}

// registryKubeconfig is a kubeconfig whose contexts reach the API servers
// servers names by context, with currentContext current.
func registryKubeconfig(t *testing.T, path, currentContext string, servers map[string]string) {
	t.Helper()
	var b strings.Builder
	b.WriteString("apiVersion: v1\nkind: Config\ncurrent-context: " + currentContext + "\nclusters:\n")
	for name, server := range servers {
		b.WriteString("- name: " + name + "\n  cluster:\n    server: " + server + "\n")
	}
	b.WriteString("contexts:\n")
	for name := range servers {
		b.WriteString("- name: " + name + "\n  context:\n    cluster: " + name + "\n    user: developer\n")
	}
	b.WriteString("users:\n- name: developer\n  user:\n    token: not-a-secret\n")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(b.String()), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestLocalRegistryDeploySkipsADeprecatedContextThatIsALocalCluster(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	kindKubeconfig := filepath.Join(home, ".kube", "config")
	t.Setenv("KUBECONFIG", kindKubeconfig)
	registryKubeconfig(t, kindKubeconfig, "kind-educates", map[string]string{
		"kind-educates":       "https://127.0.0.1:50001",
		"kind-educates-test2": "https://127.0.0.1:50002",
	})
	other := filepath.Join(t.TempDir(), "config")
	registryKubeconfig(t, other, "remote", map[string]string{
		"remote":           "https://10.0.0.1:6443",
		"copy-of-educates": "https://127.0.0.1:50001",
	})
	dataHome := t.TempDir()
	test2, err := localclusters.Named("test2", dataHome)
	if err != nil {
		t.Fatal(err)
	}
	targets := []localclusters.RegistryTarget{
		{Identity: localclusters.Default(dataHome)},
		{Identity: test2},
	}

	cases := []struct {
		name string
		opts KubeconfigOptions
		want bool
	}{
		{name: "a local cluster's context", opts: KubeconfigOptions{Context: "kind-educates-test2"}, want: true},
		{name: "the current context, a local cluster's", opts: KubeconfigOptions{Kubeconfig: kindKubeconfig}, want: true},
		{name: "another file's context on a local cluster's API server",
			opts: KubeconfigOptions{Kubeconfig: other, Context: "copy-of-educates"}, want: true},
		{name: "another cluster's context", opts: KubeconfigOptions{Kubeconfig: other, Context: "remote"}},
		{name: "another file's current context", opts: KubeconfigOptions{Kubeconfig: other}},
		{name: "a context that does not exist", opts: KubeconfigOptions{Context: "missing"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := reachesALocalCluster(tc.opts, targets); got != tc.want {
				t.Errorf("reachesALocalCluster(%+v) = %t, want %t", tc.opts, got, tc.want)
			}
		})
	}
}
