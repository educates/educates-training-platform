/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package platform

import (
	"reflect"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/yaml"

	platformv1alpha1 "github.com/educates/educates-training-platform/installer/operator/api/platform/v1alpha1"
	vendoredcharts "github.com/educates/educates-training-platform/installer/operator/vendored-charts"
)

// Plain unit tests of the session-manager chart's default-website-theme
// Secret, which sessions mount at /opt/eduk8s/theme and the training
// portal serves its theme from. Every theme file must exist, empty when
// not configured, so that the dashboard, instructions and portal pages
// that always link them don't get a 404.

// renderWebsiteThemeData installs the session-manager chart with vals
// and returns the stringData of the default-website-theme Secret.
func renderWebsiteThemeData(t *testing.T, vals map[string]any) map[string]string {
	t.Helper()

	for doc := range renderManifests(t, vendoredcharts.SessionManager, vals) {
		var s corev1.Secret
		if err := yaml.Unmarshal([]byte(doc), &s); err != nil {
			t.Fatalf("parse rendered manifest: %v", err)
		}
		if s.Kind == "Secret" && s.Name == "default-website-theme" {
			return s.StringData
		}
	}
	t.Fatalf("no default-website-theme Secret in the rendered session-manager chart")
	return nil
}

// emptyWebsiteTheme returns every file of the default website theme,
// each empty.
func emptyWebsiteTheme() map[string]string {
	return map[string]string{
		"workshop-dashboard.html":    "",
		"workshop-dashboard.js":      "",
		"workshop-dashboard.css":     "",
		"workshop-instructions.html": "",
		"workshop-instructions.js":   "",
		"workshop-instructions.css":  "",
		"workshop-started.html":      "",
		"workshop-finished.html":     "",
		"training-portal.html":       "",
		"training-portal.js":         "",
		"training-portal.css":        "",
	}
}

func TestSessionManagerChart_WebsiteThemeHasEveryFileEmptyByDefault(t *testing.T) {
	values := renderSessionManagerValues(&platformv1alpha1.SessionManager{}, valuesTestClusterConfig(), "educates-installer", testImageNamespace)

	got := renderWebsiteThemeData(t, values)

	want := emptyWebsiteTheme()
	if !reflect.DeepEqual(got, want) {
		t.Errorf("default-website-theme stringData = %v, want %v", got, want)
	}
}

func TestSessionManagerChart_WebsiteThemeKeepsInlineStyling(t *testing.T) {
	values := renderSessionManagerValues(&platformv1alpha1.SessionManager{}, valuesTestClusterConfig(), "educates-installer", testImageNamespace)
	values["websiteStyling"] = map[string]any{
		"inline": map[string]any{
			"workshopDashboard": map[string]any{"style": "body { color: red; }"},
			"workshopStarted":   map[string]any{"html": "<p>started</p>"},
			"trainingPortal":    map[string]any{"html": "<meta name=portal>", "script": "console.log(1)"},
		},
	}

	got := renderWebsiteThemeData(t, values)

	want := emptyWebsiteTheme()
	want["workshop-dashboard.css"] = "body { color: red; }"
	want["workshop-started.html"] = "<p>started</p>"
	want["training-portal.html"] = "<meta name=portal>"
	want["training-portal.js"] = "console.log(1)"
	if !reflect.DeepEqual(got, want) {
		t.Errorf("default-website-theme stringData = %v, want %v", got, want)
	}
}
