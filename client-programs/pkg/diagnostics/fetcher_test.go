package diagnostics

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/educates/educates-training-platform/client-programs/pkg/cluster"
)

// testPortals are the training portals the fake cluster runs, each with
// its namespace, as session-manager names it.
var testPortals = []string{"portal-a", "portal-b"}

func portalNamespace(portal string) string { return portal + "-ui" }

// fakeEducatesCluster serves the Kubernetes API requests and training
// portal REST API requests the diagnostics fetcher makes, for a cluster
// running testPortals. Each portal's REST API is served under
// /portals/<name>, the URL its TrainingPortal status gives.
func fakeEducatesCluster(t *testing.T) *httptest.Server {
	t.Helper()

	var srv *httptest.Server

	writeJSON := func(w http.ResponseWriter, value interface{}) {
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(value); err != nil {
			t.Errorf("encode response: %v", err)
		}
	}

	trainingPortal := func(name string) map[string]interface{} {
		return map[string]interface{}{
			"apiVersion": "training.educates.dev/v1beta1",
			"kind":       "TrainingPortal",
			"metadata":   map[string]interface{}{"name": name},
			"status": map[string]interface{}{
				"educates": map[string]interface{}{
					"url": srv.URL + "/portals/" + name,
					"clients": map[string]interface{}{
						"robot": map[string]interface{}{"id": "robot-id", "secret": "robot-secret"},
					},
					"credentials": map[string]interface{}{
						"robot": map[string]interface{}{"username": "robot", "password": "robot-password"},
					},
				},
			},
		}
	}

	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")

		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/namespaces":
			var items []interface{}
			for _, portal := range testPortals {
				items = append(items, map[string]interface{}{"metadata": map[string]interface{}{"name": portalNamespace(portal)}})
			}
			writeJSON(w, map[string]interface{}{"kind": "NamespaceList", "apiVersion": "v1", "items": items})

		case r.Method == http.MethodGet && len(parts) == 5 && parts[0] == "api" && parts[2] == "namespaces" && parts[4] == "pods":
			namespace := parts[3]
			writeJSON(w, map[string]interface{}{"kind": "PodList", "apiVersion": "v1", "items": []interface{}{
				map[string]interface{}{"metadata": map[string]interface{}{"name": "training-portal", "namespace": namespace}},
			}})

		case r.Method == http.MethodGet && len(parts) == 7 && parts[0] == "api" && parts[4] == "pods" && parts[6] == "log":
			fmt.Fprintf(w, "log of %s\n", parts[3])

		case r.Method == http.MethodGet && r.URL.Path == "/apis/training.educates.dev/v1beta1/trainingportals":
			var items []interface{}
			for _, portal := range testPortals {
				items = append(items, trainingPortal(portal))
			}
			writeJSON(w, map[string]interface{}{
				"apiVersion": "training.educates.dev/v1beta1",
				"kind":       "TrainingPortalList",
				"metadata":   map[string]interface{}{},
				"items":      items,
			})

		case r.Method == http.MethodGet && len(parts) == 5 && parts[0] == "apis" && parts[3] == "trainingportals":
			writeJSON(w, trainingPortal(parts[4]))

		case r.Method == http.MethodPost && len(parts) == 4 && parts[0] == "portals" && parts[2] == "oauth2" && parts[3] == "token":
			writeJSON(w, map[string]interface{}{"access_token": "token-" + parts[1], "token_type": "Bearer"})

		case r.Method == http.MethodPost && len(parts) == 4 && parts[0] == "portals" && parts[2] == "oauth2" && parts[3] == "revoke-token":
			w.WriteHeader(http.StatusOK)

		case r.Method == http.MethodGet && len(parts) == 5 && parts[0] == "portals" && parts[2] == "workshops" && parts[4] == "environments":
			if got, want := r.Header.Get("Authorization"), "Bearer token-"+parts[1]; got != want {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			writeJSON(w, map[string]interface{}{"portal": map[string]interface{}{"name": parts[1]}, "environments": []interface{}{}})

		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.String())
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)

	return srv
}

// newTestFetcher returns a fetcher whose cluster is srv, reached through
// a kubeconfig file, and the directory it writes its files to.
func newTestFetcher(t *testing.T, srv *httptest.Server) (*ClusterDiagnosticsFetcher, string) {
	t.Helper()

	kubeconfig := filepath.Join(t.TempDir(), "kubeconfig")
	contents := fmt.Sprintf(`apiVersion: v1
kind: Config
clusters:
- name: fake
  cluster:
    server: %s
contexts:
- name: fake
  context:
    cluster: fake
    user: fake
current-context: fake
users:
- name: fake
  user: {}
`, srv.URL)
	if err := os.WriteFile(kubeconfig, []byte(contents), 0o600); err != nil {
		t.Fatalf("write kubeconfig: %v", err)
	}

	tempDir := t.TempDir()

	return &ClusterDiagnosticsFetcher{cluster.NewClusterConfig(kubeconfig, ""), tempDir, false}, tempDir
}

func readFile(t *testing.T, path string) string {
	t.Helper()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", filepath.Base(path), err)
	}

	return string(data)
}

func TestFetchLogsForDeployment_WritesAFilePerPortalNamespace(t *testing.T) {
	fetcher, dir := newTestFetcher(t, fakeEducatesCluster(t))

	if err := fetcher.fetchLogsForDeployment("deployment=training-portal", "training.educates.dev/component=portal", "training-portal-%v.log"); err != nil {
		t.Fatalf("fetch logs: %v", err)
	}

	for _, portal := range testPortals {
		namespace := portalNamespace(portal)
		path := filepath.Join(dir, "training-portal-"+namespace+".log")

		if got, want := readFile(t, path), "log of "+namespace+"\n"; got != want {
			t.Errorf("%s holds %q, want %q", filepath.Base(path), got, want)
		}
	}
}

func TestFetchTrainingPortalDetailsAtRest_WritesAFilePerPortal(t *testing.T) {
	fetcher, dir := newTestFetcher(t, fakeEducatesCluster(t))

	if err := fetcher.fetchTrainingPortalDetailsAtRest("training-portal-catalog-%v.json"); err != nil {
		t.Fatalf("fetch catalogs: %v", err)
	}

	for _, portal := range testPortals {
		path := filepath.Join(dir, "training-portal-catalog-"+portal+".json")

		var catalog struct {
			Portal struct {
				Name string `json:"name"`
			} `json:"portal"`
		}
		if err := json.Unmarshal([]byte(readFile(t, path)), &catalog); err != nil {
			t.Fatalf("decode %s: %v", filepath.Base(path), err)
		}

		if catalog.Portal.Name != portal {
			t.Errorf("%s holds the catalog of %q, want %q", filepath.Base(path), catalog.Portal.Name, portal)
		}
	}
}

func TestFetchLogsForDeployment_WritesOneFileForANamedNamespace(t *testing.T) {
	fetcher, dir := newTestFetcher(t, fakeEducatesCluster(t))

	if err := fetcher.fetchLogsForDeployment("deployment=session-manager", "educates", "session-manager.log"); err != nil {
		t.Fatalf("fetch logs: %v", err)
	}

	if got, want := readFile(t, filepath.Join(dir, "session-manager.log")), "log of educates\n"; got != want {
		t.Errorf("session-manager.log holds %q, want %q", got, want)
	}
}
