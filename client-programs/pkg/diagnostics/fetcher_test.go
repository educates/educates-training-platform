package diagnostics

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/yaml"
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
