package deployer

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

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	sigsyaml "sigs.k8s.io/yaml"

	"github.com/educates/educates-training-platform/client-programs/pkg/deployer/apply"
	"github.com/educates/educates-training-platform/client-programs/pkg/deployer/chart"
	"github.com/educates/educates-training-platform/client-programs/pkg/deployer/progress"
	"github.com/educates/educates-training-platform/client-programs/pkg/deployer/wait"
)

// providerImmutableRejections are the messages the EducatesClusterConfig
// CRD's validation rules report when an apply changes how a cluster
// service of an existing install is provided, each paired with the
// setting the CLI names when it explains the rejection.
var providerImmutableRejections = []struct {
	setting string
	message string
}{
	{"certificate provider", "spec.ingress.certificates.provider is immutable; delete and recreate the resource to change the certificate provider"},
	{"ingress controller provider", "spec.ingress.controller.provider is immutable; delete and recreate the resource to change the ingress controller provider"},
	{"DNS provider", "spec.dns.provider is immutable; delete and recreate the resource to change the DNS provider"},
	{"policy enforcement", "spec.policyEnforcement engines and kyverno provider are immutable; delete and recreate the resource to change policy enforcement"},
}

// specRejection is the response a Kubernetes API server returns when
// apply of an EducatesClusterConfig fails one of the CEL rules on its
// spec, in the shape recorded from a real API server.
func specRejection(messages ...string) string {
	causes := make([]string, 0, len(messages))
	for _, m := range messages {
		causes = append(causes, `{"reason":"FieldValueInvalid","message":"Invalid value: `+m+`","field":"spec"}`)
	}
	return `{"kind":"Status","apiVersion":"v1","metadata":{},"status":"Failure","message":"EducatesClusterConfig.config.educates.dev \"cluster\" is invalid: spec: Invalid value: ` +
		strings.Join(messages, ", spec: Invalid value: ") +
		`","reason":"Invalid","details":{"name":"cluster","group":"config.educates.dev","kind":"EducatesClusterConfig","causes":[` +
		strings.Join(causes, ",") + `]},"code":422}`
}

// protocolRejection is a different validation failure of the same
// resource, which the CLI must pass through unexplained.
const protocolRejection = `{"kind":"Status","apiVersion":"v1","metadata":{},"status":"Failure","message":"EducatesClusterConfig.config.educates.dev \"cluster\" is invalid: spec.ingress: Invalid value: ingress.protocol http is only valid when ingress.certificates.provider is None","reason":"Invalid","details":{"name":"cluster","group":"config.educates.dev","kind":"EducatesClusterConfig","causes":[{"reason":"FieldValueInvalid","message":"Invalid value: ingress.protocol http is only valid when ingress.certificates.provider is None","field":"spec.ingress"}]},"code":422}`

// clusterConfigPath is where the API server serves the
// EducatesClusterConfig singleton.
const clusterConfigPath = "/apis/config.educates.dev/v1alpha1/educatesclusterconfigs/cluster"

// clusterConfigAPIServer serves just enough discovery for the CLI to map
// EducatesClusterConfig, and answers every apply of it with onApply and
// every read of it with onGet.
func clusterConfigAPIServer(t *testing.T, onApply, onGet http.HandlerFunc) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api":
			_, _ = io.WriteString(w, `{"kind":"APIVersions","versions":["v1"]}`)
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1":
			_, _ = io.WriteString(w, `{"kind":"APIResourceList","groupVersion":"v1","resources":[]}`)
		case r.Method == http.MethodGet && r.URL.Path == "/apis":
			_, _ = io.WriteString(w, `{"kind":"APIGroupList","apiVersion":"v1","groups":[{"name":"config.educates.dev","versions":[{"groupVersion":"config.educates.dev/v1alpha1","version":"v1alpha1"}],"preferredVersion":{"groupVersion":"config.educates.dev/v1alpha1","version":"v1alpha1"}}]}`)
		case r.Method == http.MethodGet && r.URL.Path == "/apis/config.educates.dev/v1alpha1":
			_, _ = io.WriteString(w, `{"kind":"APIResourceList","apiVersion":"v1","groupVersion":"config.educates.dev/v1alpha1","resources":[{"name":"educatesclusterconfigs","singularName":"educatesclusterconfig","namespaced":false,"kind":"EducatesClusterConfig","verbs":["get","list","watch","create","update","patch","delete"]}]}`)
		case r.Method == http.MethodPatch && r.URL.Path == clusterConfigPath && onApply != nil:
			onApply(w, r)
		case r.Method == http.MethodGet && r.URL.Path == clusterConfigPath && onGet != nil:
			onGet(w, r)
		default:
			w.WriteHeader(http.StatusNotFound)
			_, _ = io.WriteString(w, `{"kind":"Status","apiVersion":"v1","status":"Failure","reason":"NotFound","code":404}`)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

// rejectingAPIServer serves EducatesClusterConfig and answers every
// apply of it with rejection.
func rejectingAPIServer(t *testing.T, rejection string) *httptest.Server {
	t.Helper()
	return clusterConfigAPIServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnprocessableEntity)
		_, _ = io.WriteString(w, rejection)
	}, nil)
}

// clusterConfig is the EducatesClusterConfig singleton at generation,
// with a Ready=True condition the operator concluded for
// readyGeneration, or no status when readyGeneration is zero.
func clusterConfig(generation, readyGeneration int64) map[string]interface{} {
	obj := map[string]interface{}{
		"apiVersion": "config.educates.dev/v1alpha1",
		"kind":       "EducatesClusterConfig",
		"metadata":   map[string]interface{}{"name": "cluster", "generation": generation},
	}
	if readyGeneration > 0 {
		obj["status"] = map[string]interface{}{
			"phase": "Ready",
			"conditions": []interface{}{
				map[string]interface{}{"type": "Ready", "status": "True", "observedGeneration": readyGeneration},
			},
		}
	}
	return obj
}

// reconcilingAPIServer serves EducatesClusterConfig, answers its apply
// with the singleton at generation, and the n-th read of it with
// states[n], repeating the last state once the script runs out. gets
// reports how many reads it has served.
func reconcilingAPIServer(t *testing.T, generation int64, states ...map[string]interface{}) (srv *httptest.Server, gets func() int) {
	t.Helper()
	var mu sync.Mutex
	served := 0
	srv = clusterConfigAPIServer(t,
		func(w http.ResponseWriter, _ *http.Request) {
			_ = json.NewEncoder(w).Encode(clusterConfig(generation, 0))
		},
		func(w http.ResponseWriter, _ *http.Request) {
			mu.Lock()
			state := states[min(served, len(states)-1)]
			served++
			mu.Unlock()
			_ = json.NewEncoder(w).Encode(state)
		})
	return srv, func() int {
		mu.Lock()
		defer mu.Unlock()
		return served
	}
}

// restConfigGetter hands the deployer a fixed REST config; apply and
// wait only ever ask for that.
type restConfigGetter struct{ cfg *rest.Config }

func (g restConfigGetter) ToRESTConfig() (*rest.Config, error) { return g.cfg, nil }
func (g restConfigGetter) ToDiscoveryClient() (discovery.CachedDiscoveryInterface, error) {
	return nil, errors.New("not used")
}
func (g restConfigGetter) ToRESTMapper() (meta.RESTMapper, error)        { return nil, errors.New("not used") }
func (g restConfigGetter) ToRawKubeConfigLoader() clientcmd.ClientConfig { return nil }

// applyClusterConfig runs the deploy step that applies the
// EducatesClusterConfig against srv, and returns the object it saw Ready
// and its error.
func applyClusterConfig(t *testing.T, srv *httptest.Server) (*unstructured.Unstructured, error) {
	t.Helper()
	opts := Options{
		Getter:   restConfigGetter{cfg: &rest.Config{Host: srv.URL}},
		Progress: progress.New(io.Discard, 0, false),
		Timeout:  DefaultTimeout,
	}
	applier, err := apply.New(opts.Getter)
	if err != nil {
		t.Fatalf("apply.New: %v", err)
	}
	waiter, err := wait.New(opts.Getter)
	if err != nil {
		t.Fatalf("wait.New: %v", err)
	}
	ecc := map[string]interface{}{
		"apiVersion": "config.educates.dev/v1alpha1",
		"kind":       "EducatesClusterConfig",
		"metadata":   map[string]interface{}{"name": "cluster"},
		"spec": map[string]interface{}{
			"mode": "Managed",
			"ingress": map[string]interface{}{
				"domain":           "educates.test",
				"ingressClassName": "contour",
				"controller":       map[string]interface{}{"provider": "BundledContour"},
				"certificates":     map[string]interface{}{"provider": "None"},
			},
		},
	}
	return installStep(context.Background(), opts, applier, waiter, ecc, "EducatesClusterConfig")
}

func TestInstallStep_WaitsForTheGenerationItApplied(t *testing.T) {
	srv, gets := reconcilingAPIServer(t, 4,
		// The apply moved the resource to generation 4; the Ready
		// condition is still the one the operator concluded for 3.
		clusterConfig(4, 3),
		clusterConfig(4, 4),
	)
	if _, err := applyClusterConfig(t, srv); err != nil {
		t.Fatalf("installStep: %v", err)
	}
	if n := gets(); n != 2 {
		t.Errorf("installStep returned after %d reads, want 2: it took generation 3's Ready for the generation it applied", n)
	}
}

func TestDeploy_ProviderChangeIsExplained(t *testing.T) {
	for _, rejection := range providerImmutableRejections {
		t.Run(rejection.setting, func(t *testing.T) {
			_, err := applyClusterConfig(t, rejectingAPIServer(t, specRejection(rejection.message)))
			if err == nil {
				t.Fatal("apply succeeded, want the provider change rejected")
			}
			msg := err.Error()
			for _, want := range []string{
				"the " + rejection.setting + " of an existing Educates install cannot change",
				"'educates admin platform delete'",
				"recreate the cluster",
				// The API server's own reason stays visible.
				rejection.message,
			} {
				if !strings.Contains(msg, want) {
					t.Errorf("error %q does not contain %q", msg, want)
				}
			}
			if !apierrors.IsInvalid(err) {
				t.Errorf("error no longer wraps the API server's Invalid status: %v", err)
			}
		})
	}
}

func TestDeploy_SeveralProviderChangesAreExplainedTogether(t *testing.T) {
	_, err := applyClusterConfig(t, rejectingAPIServer(t, specRejection(
		providerImmutableRejections[0].message,
		providerImmutableRejections[2].message,
	)))
	if err == nil {
		t.Fatal("apply succeeded, want the provider changes rejected")
	}
	want := "the certificate provider and DNS provider of an existing Educates install cannot change"
	if !strings.Contains(err.Error(), want) {
		t.Errorf("error %q does not contain %q", err.Error(), want)
	}
}

func TestDeploy_OtherApplyRejectionsAreNotExplained(t *testing.T) {
	_, err := applyClusterConfig(t, rejectingAPIServer(t, protocolRejection))
	if err == nil {
		t.Fatal("apply succeeded, want the protocol rejection")
	}
	msg := err.Error()
	if !strings.Contains(msg, "ingress.protocol http is only valid") {
		t.Errorf("error %q lost the API server's reason", msg)
	}
	if strings.Contains(msg, "admin platform delete") {
		t.Errorf("error %q explains a provider change for an unrelated rejection", msg)
	}
}

// TestProviderImmutableRejections_MatchEmbeddedCRD keeps the rejections
// the tests above record in step with the CEL rules of the
// EducatesClusterConfig CRD the CLI installs, both ways: rewording a
// rule cannot silently drop the remedy the CLI adds, and a new
// immutability rule cannot go without it. The spec.mode rule is left
// out, since its own message already names the remedy.
func TestProviderImmutableRejections_MatchEmbeddedCRD(t *testing.T) {
	chrt, err := chart.Load()
	if err != nil {
		t.Fatalf("chart.Load: %v", err)
	}
	var messages []string
	for _, c := range chrt.CRDObjects() {
		crd := map[string]interface{}{}
		if err := sigsyaml.Unmarshal(c.File.Data, &crd); err != nil {
			t.Fatalf("parse %s: %v", c.Filename, err)
		}
		if kind, _, _ := unstructured.NestedString(crd, "spec", "names", "kind"); kind != "EducatesClusterConfig" {
			continue
		}
		versions, _, _ := unstructured.NestedSlice(crd, "spec", "versions")
		for _, v := range versions {
			version, _ := v.(map[string]interface{})
			rules, _, _ := unstructured.NestedSlice(version,
				"schema", "openAPIV3Schema", "properties", "spec", "x-kubernetes-validations")
			for _, r := range rules {
				rule, _ := r.(map[string]interface{})
				if msg, ok := rule["message"].(string); ok {
					messages = append(messages, msg)
				}
			}
		}
	}
	if len(messages) == 0 {
		t.Fatal("the embedded EducatesClusterConfig CRD has no validation rules on spec")
	}
	recorded := make([]string, 0, len(providerImmutableRejections))
	for _, rejection := range providerImmutableRejections {
		recorded = append(recorded, rejection.message)
		if !slices.Contains(messages, rejection.message) {
			t.Errorf("no validation rule on spec of the embedded EducatesClusterConfig CRD reports %q; its messages are %q",
				rejection.message, messages)
		}
	}
	for _, msg := range messages {
		if strings.Contains(msg, " immutable; ") && !strings.HasPrefix(msg, "spec.mode ") && !slices.Contains(recorded, msg) {
			t.Errorf("the embedded EducatesClusterConfig CRD has an immutability rule the CLI does not explain: %q", msg)
		}
	}
}
