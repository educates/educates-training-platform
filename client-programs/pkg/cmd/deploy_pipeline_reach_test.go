package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeKubeconfig writes a kubeconfig whose contexts a and b reach the
// API server at server, with currentContext as its current context, and
// returns its path. It also keeps the test from running as a pod would,
// through the in-cluster service account.
func writeKubeconfig(t *testing.T, currentContext, server string) string {
	t.Helper()
	t.Setenv("KUBERNETES_SERVICE_HOST", "")
	t.Setenv("KUBERNETES_SERVICE_PORT", "")

	body := `apiVersion: v1
kind: Config
clusters:
- name: c
  cluster:
    server: ` + server + `
users:
- name: u
  user: {}
contexts:
- name: a
  context: {cluster: c, user: u}
- name: b
  context: {cluster: c, user: u}
current-context: "` + currentContext + `"
`
	path := filepath.Join(t.TempDir(), "kubeconfig")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// reachForTest runs reachDeployCluster with the kubeconfig at path and
// the --context value context, and returns its error.
func reachForTest(path, context string) error {
	flags := deployPipelineFlags{Kubeconfig: path, Context: context}
	_, err := reachDeployCluster(deployConfigFlags(flags), flags)
	return err
}

func wantErrorParts(t *testing.T, err error, parts ...string) {
	t.Helper()
	if err == nil {
		t.Fatalf("got no error, want one mentioning %q", parts)
	}
	for _, part := range parts {
		if !strings.Contains(err.Error(), part) {
			t.Errorf("error %q does not mention %q", err, part)
		}
	}
}

// TestReachDeployClusterNeedsAKubeContext checks that a kubeconfig with
// no current context, and no --context, is reported as such, with the
// contexts to choose from, rather than as a failed request to client-go's
// default server.
func TestReachDeployClusterNeedsAKubeContext(t *testing.T) {
	err := reachForTest(writeKubeconfig(t, "", "http://127.0.0.1:1"), "")

	wantErrorParts(t, err, "no kube context is selected", "the contexts a and b", "--context", "--local-cluster")
	if strings.Contains(err.Error(), "localhost:8080") {
		t.Errorf("error %q names client-go's default server", err)
	}
}

func TestReachDeployClusterRefusesAContextTheKubeconfigLacks(t *testing.T) {
	err := reachForTest(writeKubeconfig(t, "a", "http://127.0.0.1:1"), "nope")

	wantErrorParts(t, err, `kube context "nope" is not in the kubeconfig`, "which holds the contexts a and b")
}

// TestReachDeployClusterNamesTheContextAndServerItCannotReach checks that
// a cluster which does not answer is reported with its context, its
// server and the cause, without the request's own URL.
func TestReachDeployClusterNamesTheContextAndServerItCannotReach(t *testing.T) {
	err := reachForTest(writeKubeconfig(t, "b", "http://127.0.0.1:1"), "")

	wantErrorParts(t, err, `unable to reach the cluster of kube context "b" at http://127.0.0.1:1`)
	if strings.Contains(err.Error(), `Get "`) {
		t.Errorf("error %q repeats the request URL", err)
	}
}
