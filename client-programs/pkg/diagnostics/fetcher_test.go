package diagnostics

import (
	"bytes"
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
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

func TestDiagnosticsRedactTrainingPortalInlineCredentials(t *testing.T) {
	var bundle bytes.Buffer

	if err := writeResources(&bundle, trainingportalResource, []unstructured.Unstructured{portalWithInlineCredentials()}); err != nil {
		t.Fatalf("writeResources: %v", err)
	}

	written := bundle.String()
	for _, value := range []string{
		"inline-admin-user", "inline-admin-password",
		"inline-robot-user", "inline-robot-password",
		"inline-robot-client-id", "inline-robot-client-secret",
	} {
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
