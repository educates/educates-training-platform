package wait

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

// restConfigGetter hands the waiter a fixed REST config; it only ever
// asks for that.
type restConfigGetter struct{ cfg *rest.Config }

func (g restConfigGetter) ToRESTConfig() (*rest.Config, error) { return g.cfg, nil }
func (g restConfigGetter) ToDiscoveryClient() (discovery.CachedDiscoveryInterface, error) {
	return nil, errors.New("not used")
}
func (g restConfigGetter) ToRESTMapper() (meta.RESTMapper, error)        { return nil, errors.New("not used") }
func (g restConfigGetter) ToRawKubeConfigLoader() clientcmd.ClientConfig { return nil }

const sessionManagerPath = "/apis/platform.educates.dev/v1alpha1/sessionmanagers/cluster"

// scriptedAPIServer serves discovery for SessionManager and answers the
// n-th GET of the object at path with states[n], repeating the last
// state once the script runs out. gets reports how many GETs of the
// object it has served.
func scriptedAPIServer(t *testing.T, path string, states ...map[string]interface{}) (srv *httptest.Server, gets func() int) {
	t.Helper()
	var mu sync.Mutex
	served := 0
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api":
			_, _ = io.WriteString(w, `{"kind":"APIVersions","versions":["v1"]}`)
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1":
			_, _ = io.WriteString(w, `{"kind":"APIResourceList","groupVersion":"v1","resources":[]}`)
		case r.Method == http.MethodGet && r.URL.Path == "/apis":
			_, _ = io.WriteString(w, `{"kind":"APIGroupList","apiVersion":"v1","groups":[{"name":"platform.educates.dev","versions":[{"groupVersion":"platform.educates.dev/v1alpha1","version":"v1alpha1"}],"preferredVersion":{"groupVersion":"platform.educates.dev/v1alpha1","version":"v1alpha1"}}]}`)
		case r.Method == http.MethodGet && r.URL.Path == "/apis/platform.educates.dev/v1alpha1":
			_, _ = io.WriteString(w, `{"kind":"APIResourceList","apiVersion":"v1","groupVersion":"platform.educates.dev/v1alpha1","resources":[{"name":"sessionmanagers","singularName":"sessionmanager","namespaced":false,"kind":"SessionManager","verbs":["get","list","watch","create","update","patch","delete"]}]}`)
		case r.Method == http.MethodGet && r.URL.Path == path:
			mu.Lock()
			state := states[min(served, len(states)-1)]
			served++
			mu.Unlock()
			_ = json.NewEncoder(w).Encode(state)
		default:
			w.WriteHeader(http.StatusNotFound)
			_, _ = io.WriteString(w, `{"kind":"Status","apiVersion":"v1","status":"Failure","reason":"NotFound","code":404}`)
		}
	}))
	t.Cleanup(srv.Close)
	return srv, func() int {
		mu.Lock()
		defer mu.Unlock()
		return served
	}
}

// testTimeout bounds every wait under test: long enough for any
// scripted sequence, short enough that a wait that never ends fails
// the test quickly.
const testTimeout = 5 * time.Second

// newTestClient builds a waiter against srv that polls every
// millisecond, so a scripted sequence of states plays out quickly.
func newTestClient(t *testing.T, srv *httptest.Server) *Client {
	t.Helper()
	c, err := New(restConfigGetter{cfg: &rest.Config{Host: srv.URL}})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	c.pollInterval = time.Millisecond
	return c
}

// sessionManager is the SessionManager singleton at generation, with
// status as the operator last wrote it.
func sessionManager(generation int64, status map[string]interface{}) map[string]interface{} {
	obj := map[string]interface{}{
		"apiVersion": "platform.educates.dev/v1alpha1",
		"kind":       "SessionManager",
		"metadata":   map[string]interface{}{"name": "cluster", "generation": generation},
	}
	if status != nil {
		obj["status"] = status
	}
	return obj
}

// readyStatus is a status whose Ready condition has the given status
// and observedGeneration.
func readyStatus(phase, ready string, observedGeneration int64) map[string]interface{} {
	return map[string]interface{}{
		"phase": phase,
		"conditions": []interface{}{
			map[string]interface{}{
				"type":               "Ready",
				"status":             ready,
				"observedGeneration": observedGeneration,
				"reason":             "Test",
				"message":            "scripted",
			},
		},
	}
}

// applied is the object the apply of the SessionManager returned, at
// generation.
func applied(generation int64) *unstructured.Unstructured {
	return &unstructured.Unstructured{Object: sessionManager(generation, nil)}
}

func TestWaitReady_IgnoresReadyFromAnEarlierGeneration(t *testing.T) {
	srv, gets := scriptedAPIServer(t, sessionManagerPath,
		// The operator has not seen generation 4 yet: Ready=True is
		// what it concluded for generation 3.
		sessionManager(4, readyStatus("Ready", "True", 3)),
		sessionManager(4, readyStatus("Upgrading", "False", 4)),
		sessionManager(4, readyStatus("Ready", "True", 4)),
	)
	c := newTestClient(t, srv)

	got, err := c.WaitReady(context.Background(), applied(4), testTimeout, nil)
	if err != nil {
		t.Fatalf("WaitReady: %v", err)
	}
	if n := gets(); n != 3 {
		t.Errorf("WaitReady returned after %d reads, want 3: it accepted a Ready condition before generation 4 was reconciled", n)
	}
	if phase, _, _ := unstructured.NestedString(got.Object, "status", "phase"); phase != "Ready" {
		t.Errorf("returned object has phase %q, want Ready", phase)
	}
}

func TestWaitReady_ReportsOnlyPhasesOfTheAppliedGeneration(t *testing.T) {
	srv, _ := scriptedAPIServer(t, sessionManagerPath,
		sessionManager(4, readyStatus("Ready", "True", 3)),
		sessionManager(4, readyStatus("Upgrading", "False", 4)),
		sessionManager(4, readyStatus("Ready", "True", 4)),
	)
	c := newTestClient(t, srv)

	var phases []string
	if _, err := c.WaitReady(context.Background(), applied(4), testTimeout, func(phase string) {
		phases = append(phases, phase)
	}); err != nil {
		t.Fatalf("WaitReady: %v", err)
	}
	if want := []string{"Upgrading", "Ready"}; !slices.Equal(phases, want) {
		t.Errorf("reported phases %q, want %q: the phase of generation 3's status is not progress on generation 4", phases, want)
	}
}

// statusWithoutConditionGeneration is a status whose Ready condition
// doesn't record a generation, leaving status.observedGeneration to say
// which spec it describes.
func statusWithoutConditionGeneration(ready string, observedGeneration int64) map[string]interface{} {
	return map[string]interface{}{
		"observedGeneration": observedGeneration,
		"conditions": []interface{}{
			map[string]interface{}{"type": "Ready", "status": ready},
		},
	}
}

func TestWaitReady_FallsBackToTheStatusObservedGeneration(t *testing.T) {
	srv, gets := scriptedAPIServer(t, sessionManagerPath,
		sessionManager(4, statusWithoutConditionGeneration("True", 3)),
		sessionManager(4, statusWithoutConditionGeneration("True", 4)),
	)
	c := newTestClient(t, srv)

	if _, err := c.WaitReady(context.Background(), applied(4), testTimeout, nil); err != nil {
		t.Fatalf("WaitReady: %v", err)
	}
	if n := gets(); n != 2 {
		t.Errorf("WaitReady returned after %d reads, want 2: status.observedGeneration 3 does not describe generation 4", n)
	}
}

func TestWaitReady_TimeoutNamesTheUnreconciledGeneration(t *testing.T) {
	srv, _ := scriptedAPIServer(t, sessionManagerPath,
		sessionManager(4, readyStatus("Ready", "True", 3)),
	)
	c := newTestClient(t, srv)

	_, err := c.WaitReady(context.Background(), applied(4), 20*time.Millisecond, nil)
	if err == nil {
		t.Fatal("WaitReady succeeded, want a timeout: generation 4 was never reconciled")
	}
	want := "the operator has not reconciled generation 4 yet; the status describes generation 3 (phase=Ready)"
	if !strings.Contains(err.Error(), want) {
		t.Errorf("error %q does not contain %q", err.Error(), want)
	}
}

const operatorDeploymentPath = "/apis/apps/v1/namespaces/educates-installer/deployments/educates-installer"

// rollout is the state of a one-replica Deployment's rollout at
// generation, as the deployment controller reports it.
type rollout struct {
	generation, observedGeneration       int64
	replicas, updatedReplicas, available int64
	progressDeadlineExceeded             bool
}

func (r rollout) deployment() map[string]interface{} {
	status := map[string]interface{}{
		"observedGeneration": r.observedGeneration,
		"replicas":           r.replicas,
		"updatedReplicas":    r.updatedReplicas,
		"availableReplicas":  r.available,
	}
	if r.progressDeadlineExceeded {
		status["conditions"] = []interface{}{
			map[string]interface{}{"type": "Progressing", "status": "False", "reason": "ProgressDeadlineExceeded"},
		}
	}
	return map[string]interface{}{
		"apiVersion": "apps/v1",
		"kind":       "Deployment",
		"metadata":   map[string]interface{}{"name": "educates-installer", "namespace": "educates-installer", "generation": r.generation},
		"spec":       map[string]interface{}{"replicas": int64(1)},
		"status":     status,
	}
}

func TestWaitRolledOut_WaitsForTheNewReplicasAndNoOldOnes(t *testing.T) {
	srv, gets := scriptedAPIServer(t, operatorDeploymentPath,
		// The deployment controller hasn't seen the new spec: the
		// status still describes the running operator.
		rollout{generation: 2, observedGeneration: 1, replicas: 1, updatedReplicas: 1, available: 1}.deployment(),
		rollout{generation: 2, observedGeneration: 2, replicas: 1, updatedReplicas: 0, available: 1}.deployment(),
		rollout{generation: 2, observedGeneration: 2, replicas: 2, updatedReplicas: 1, available: 1}.deployment(),
		rollout{generation: 2, observedGeneration: 2, replicas: 1, updatedReplicas: 1, available: 0}.deployment(),
		rollout{generation: 2, observedGeneration: 2, replicas: 1, updatedReplicas: 1, available: 1}.deployment(),
	)
	c := newTestClient(t, srv)

	if err := c.WaitRolledOut(context.Background(), "educates-installer", "educates-installer", testTimeout, nil); err != nil {
		t.Fatalf("WaitRolledOut: %v", err)
	}
	if n := gets(); n != 5 {
		t.Errorf("WaitRolledOut returned after %d reads, want 5: the rollout was not complete before the last one", n)
	}
}

func TestWaitRolledOut_ReportsEachChangeOfProgress(t *testing.T) {
	srv, _ := scriptedAPIServer(t, operatorDeploymentPath,
		rollout{generation: 2, observedGeneration: 1, replicas: 1, updatedReplicas: 1, available: 1}.deployment(),
		rollout{generation: 2, observedGeneration: 2, replicas: 1, updatedReplicas: 0, available: 1}.deployment(),
		rollout{generation: 2, observedGeneration: 2, replicas: 1, updatedReplicas: 0, available: 0}.deployment(),
		rollout{generation: 2, observedGeneration: 2, replicas: 1, updatedReplicas: 1, available: 0}.deployment(),
		rollout{generation: 2, observedGeneration: 2, replicas: 1, updatedReplicas: 1, available: 1}.deployment(),
	)
	c := newTestClient(t, srv)

	var progress []string
	if err := c.WaitRolledOut(context.Background(), "educates-installer", "educates-installer", testTimeout, func(p string) {
		progress = append(progress, p)
	}); err != nil {
		t.Fatalf("WaitRolledOut: %v", err)
	}
	want := []string{
		"waiting for the rollout to start",
		"rolling out, 0 of 1 replicas updated",
		"rolling out, 0 of 1 updated replicas available",
	}
	if !slices.Equal(progress, want) {
		t.Errorf("reported progress %q, want %q", progress, want)
	}
}

func TestWaitRolledOut_FailsOnceTheProgressDeadlineIsExceeded(t *testing.T) {
	srv, gets := scriptedAPIServer(t, operatorDeploymentPath,
		rollout{generation: 2, observedGeneration: 2, replicas: 1, updatedReplicas: 1, available: 0, progressDeadlineExceeded: true}.deployment(),
	)
	c := newTestClient(t, srv)

	err := c.WaitRolledOut(context.Background(), "educates-installer", "educates-installer", testTimeout, nil)
	if err == nil || !strings.Contains(err.Error(), "exceeded its progress deadline") {
		t.Fatalf("WaitRolledOut error = %v, want the progress deadline reported", err)
	}
	if n := gets(); n != 1 {
		t.Errorf("WaitRolledOut read the Deployment %d times, want 1: a rollout past its deadline won't finish", n)
	}
}

func TestWaitRolledOut_TimeoutSaysWhatTheRolloutWaitsOn(t *testing.T) {
	srv, _ := scriptedAPIServer(t, operatorDeploymentPath,
		rollout{generation: 2, observedGeneration: 2, replicas: 1, updatedReplicas: 1, available: 0}.deployment(),
	)
	c := newTestClient(t, srv)

	err := c.WaitRolledOut(context.Background(), "educates-installer", "educates-installer", 20*time.Millisecond, nil)
	want := "timeout waiting for Deployment educates-installer/educates-installer to roll out (last status: rolling out, 0 of 1 updated replicas available)"
	if err == nil || err.Error() != want {
		t.Fatalf("WaitRolledOut error = %v, want %q", err, want)
	}
}
