package diagnostics

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/yaml"

	"github.com/educates/educates-training-platform/client-programs/pkg/cluster"
)

// portalWithInlineCredentials returns a TrainingPortal whose spec sets every
// inline credential, each value distinct so a leak is easy to spot.
func portalWithInlineCredentials() unstructured.Unstructured {
	return unstructured.Unstructured{Object: map[string]interface{}{
		"apiVersion": "training.educates.dev/v1beta1",
		"kind":       "TrainingPortal",
		"metadata":   map[string]interface{}{"name": "lab-portal"},
		"spec": map[string]interface{}{
			"portal": map[string]interface{}{
				"title": "Lab Portal",
				"credentials": map[string]interface{}{
					"admin": map[string]interface{}{"username": "inline-admin-user", "password": "inline-admin-password"},
					"robot": map[string]interface{}{"username": "inline-robot-user", "password": "inline-robot-password"},
				},
				"clients": map[string]interface{}{
					"robot": map[string]interface{}{"id": "inline-robot-client-id", "secret": "inline-robot-client-secret"},
				},
			},
		},
	}}
}

var inlineCredentialValues = []string{
	"inline-admin-user", "inline-admin-password",
	"inline-robot-user", "inline-robot-password",
	"inline-robot-client-id", "inline-robot-client-secret",
}

func TestDiagnosticsRedactTrainingPortalInlineCredentialsInCopiesOfItsSpec(t *testing.T) {
	// kopf records the spec it last handled, and kubectl apply the
	// manifest it last applied, as JSON in annotations of the object.
	portal := portalWithInlineCredentials()
	portal.SetAnnotations(map[string]string{
		"kopf.zalando.org/last-handled-configuration": `{"spec":{"portal":{"title":"Lab Portal",` +
			`"credentials":{"admin":{"username":"inline-admin-user","password":"inline-admin-password"},"robot":{"username":"inline-robot-user","password":"inline-robot-password"}},` +
			`"clients":{"robot":{"id":"inline-robot-client-id","secret":"inline-robot-client-secret"}}}}}`,
		"kubectl.kubernetes.io/last-applied-configuration": `{"apiVersion":"training.educates.dev/v1beta1","kind":"TrainingPortal","metadata":{"name":"lab-portal"},` +
			`"spec":{"portal":{"title":"Lab Portal",` +
			`"credentials":{"admin":{"username":"inline-admin-user","password":"inline-admin-password"},"robot":{"username":"inline-robot-user","password":"inline-robot-password"}},` +
			`"clients":{"robot":{"id":"inline-robot-client-id","secret":"inline-robot-client-secret"}}}}}`,
		"example.com/unrelated": "inline-admin-user is kept here",
	})
	var bundle bytes.Buffer

	if err := writeResources(&bundle, trainingportalResource, []unstructured.Unstructured{portal}); err != nil {
		t.Fatalf("writeResources: %v", err)
	}

	written := bundle.String()
	for _, value := range inlineCredentialValues {
		if strings.Contains(strings.ReplaceAll(written, "inline-admin-user is kept here", ""), value) {
			t.Errorf("the bundle holds the credential %q:\n%s", value, written)
		}
	}
	var collected unstructured.Unstructured
	if err := yaml.Unmarshal(bundle.Bytes(), &collected.Object); err != nil {
		t.Fatalf("reading back the bundle: %v", err)
	}
	annotations := collected.GetAnnotations()
	for _, annotation := range []string{"kopf.zalando.org/last-handled-configuration", "kubectl.kubernetes.io/last-applied-configuration"} {
		var copied map[string]interface{}
		if err := json.Unmarshal([]byte(annotations[annotation]), &copied); err != nil {
			t.Errorf("the annotation %s is no longer JSON: %v", annotation, err)
			continue
		}
		title, _, _ := unstructured.NestedString(copied, "spec", "portal", "title")
		password, _, _ := unstructured.NestedString(copied, "spec", "portal", "credentials", "admin", "password")
		if title != "Lab Portal" || password != "REDACTED" {
			t.Errorf("the annotation %s holds title %q and admin password %q, want %q and %q", annotation, title, password, "Lab Portal", "REDACTED")
		}
	}
	if annotations["example.com/unrelated"] != "inline-admin-user is kept here" {
		t.Errorf("the bundle changed an annotation holding no copy of the spec: %q", annotations["example.com/unrelated"])
	}
}

func TestDiagnosticsRedactTrainingPortalInlineCredentials(t *testing.T) {
	var bundle bytes.Buffer

	if err := writeResources(&bundle, trainingportalResource, []unstructured.Unstructured{portalWithInlineCredentials()}); err != nil {
		t.Fatalf("writeResources: %v", err)
	}

	written := bundle.String()
	for _, value := range inlineCredentialValues {
		if strings.Contains(written, value) {
			t.Errorf("the bundle holds the credential %q:\n%s", value, written)
		}
	}
	if !strings.Contains(written, "title: Lab Portal") {
		t.Errorf("the bundle lost the rest of the TrainingPortal's spec:\n%s", written)
	}
	if !strings.Contains(written, "REDACTED") {
		t.Errorf("the bundle does not show that credentials were redacted:\n%s", written)
	}
}

// testPortals are the training portals the fake cluster runs, each with
// its namespace, as session-manager names it.
var testPortals = []string{"portal-a", "portal-b"}

func portalNamespace(portal string) string { return portal + "-ui" }

// robotCredentialsSecret is the Secret in its namespace that a portal's
// status names as holding its robot credentials.
const robotCredentialsSecret = "training-portal-robot-credentials"

// robotCredentials are the values of the robot credentials Secret of
// portal, by key, each distinct to the portal.
func robotCredentials(portal string) map[string]string {
	return map[string]string{
		"username":      "robot-" + portal,
		"password":      "robot-password-" + portal,
		"client-id":     "robot-client-id-" + portal,
		"client-secret": "robot-client-secret-" + portal,
	}
}

// fakeEducatesCluster serves the Kubernetes API requests and training
// portal REST API requests the diagnostics fetcher makes, for a cluster
// running testPortals. Each portal's REST API is served under
// /portals/<name>, the URL its TrainingPortal status gives, and accepts
// only a login with the robot credentials of the Secret its status names.
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
					"url":       srv.URL + "/portals/" + name,
					"namespace": portalNamespace(name),
					"credentialSecrets": map[string]interface{}{
						"robot": robotCredentialsSecret,
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

		case r.Method == http.MethodGet && len(parts) == 6 && parts[0] == "api" && parts[4] == "secrets" && parts[5] == robotCredentialsSecret:
			portal, ok := strings.CutSuffix(parts[3], "-ui")
			if !ok {
				t.Errorf("robot credentials Secret requested in namespace %q, which is no portal's", parts[3])
				w.WriteHeader(http.StatusNotFound)
				return
			}
			data := map[string]string{}
			for key, value := range robotCredentials(portal) {
				data[key] = base64.StdEncoding.EncodeToString([]byte(value))
			}
			writeJSON(w, map[string]interface{}{
				"kind":       "Secret",
				"apiVersion": "v1",
				"metadata":   map[string]interface{}{"name": robotCredentialsSecret, "namespace": parts[3]},
				"type":       "Opaque",
				"data":       data,
			})

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
			want := robotCredentials(parts[1])
			clientID, clientSecret, _ := r.BasicAuth()
			if err := r.ParseForm(); err != nil {
				t.Errorf("parse login form for %s: %v", parts[1], err)
			}
			if clientID != want["client-id"] || clientSecret != want["client-secret"] ||
				r.PostForm.Get("username") != want["username"] || r.PostForm.Get("password") != want["password"] {
				t.Errorf("login to %s did not use the robot credentials of its Secret", parts[1])
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
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
