package cmd

import (
	"strings"
	"testing"
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
		if !strings.Contains(f.Deprecated, "every local cluster") {
			t.Errorf("--%s deprecation message = %q, want it to say the command configures every local cluster", name, f.Deprecated)
		}
	}
}
