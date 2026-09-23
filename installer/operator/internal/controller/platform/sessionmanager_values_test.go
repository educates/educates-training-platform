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
	"testing"

	configv1alpha1 "github.com/educates/educates-training-platform/installer/operator/api/config/v1alpha1"
	platformv1alpha1 "github.com/educates/educates-training-platform/installer/operator/api/platform/v1alpha1"
)

// Plain unit tests for the values-rendering helpers — no envtest
// needed; the integration behavior is covered by the ginkgo suite.

const tagLatest = "latest"

func TestSplitImageRef(t *testing.T) {
	cases := []struct {
		ref, repo, tag string
	}{
		{"localhost:5001/educates-session-manager:latest", "localhost:5001/educates-session-manager", tagLatest},
		{"ghcr.io/educates/educates-session-manager:4.0.0", "ghcr.io/educates/educates-session-manager", "4.0.0"},
		{"ghcr.io/educates/educates-session-manager", "ghcr.io/educates/educates-session-manager", ""},
		{"localhost:5001/no-tag", "localhost:5001/no-tag", ""},
		{"ghcr.io/x/y@sha256:abcdef", "ghcr.io/x/y@sha256:abcdef", ""},
	}
	for _, c := range cases {
		repo, tag := splitImageRef(c.ref)
		if repo != c.repo || tag != c.tag {
			t.Errorf("splitImageRef(%q) = (%q, %q), want (%q, %q)", c.ref, repo, tag, c.repo, c.tag)
		}
	}
}

func valuesTestClusterConfig() *configv1alpha1.EducatesClusterConfig {
	return &configv1alpha1.EducatesClusterConfig{
		Status: configv1alpha1.EducatesClusterConfigStatus{
			Ingress: &configv1alpha1.StatusIngress{
				Domain:           "test.example.com",
				IngressClassName: "contour",
				WildcardCertificateSecretRef: &configv1alpha1.NamespacedSecretRef{
					Name:      "wildcard-tls",
					Namespace: "educates-secrets",
				},
				CACertificateSecretRef: &configv1alpha1.NamespacedSecretRef{
					Name:      "educates-ca",
					Namespace: "educates-secrets",
				},
			},
		},
	}
}

// TestApplySMIngressValues_NoneProvider asserts that a status with no
// wildcard certificate (certificates provider None) renders an empty
// tlsCertificateRef and carries the published http protocol, so the
// chart renders plain HTTP ingresses.
func TestApplySMIngressValues_NoneProvider(t *testing.T) {
	cfg := &configv1alpha1.EducatesClusterConfig{
		Status: configv1alpha1.EducatesClusterConfigStatus{
			Ingress: &configv1alpha1.StatusIngress{
				Domain:           "test.example.com",
				IngressClassName: "contour",
				Protocol:         configv1alpha1.IngressProtocolHTTP,
			},
		},
	}
	values := map[string]any{}
	applySMIngressValues(values, &platformv1alpha1.SessionManager{}, cfg)

	ci := values["clusterIngress"].(map[string]any)
	tlsRef := ci["tlsCertificateRef"].(map[string]any)
	if tlsRef["name"] != "" || tlsRef["namespace"] != "" {
		t.Errorf("tlsCertificateRef = %v, want empty name+namespace for None provider", tlsRef)
	}
	if got, want := ci["protocol"], "http"; got != want {
		t.Errorf("clusterIngress.protocol = %v, want %v", got, want)
	}
}

func TestApplySMImageValues_RoutesSpecialOverrides(t *testing.T) {
	obj := &platformv1alpha1.SessionManager{
		Spec: platformv1alpha1.SessionManagerSpec{
			Images: &platformv1alpha1.Images{
				Overrides: []platformv1alpha1.ImageOverride{
					{Name: "session-manager", Image: "localhost:5001/educates-session-manager:latest"},
					{Name: "pause-container", Image: "localhost:5001/educates-pause-container:latest"},
					{Name: "node-ca-injector", Image: "localhost:5001/educates-node-ca-injector:latest"},
					{Name: "training-portal", Image: "localhost:5001/educates-training-portal:latest"},
				},
			},
		},
	}
	values := map[string]any{}
	applySMImageValues(values, obj, valuesTestClusterConfig())

	image, ok := values["image"].(map[string]any)
	if !ok {
		t.Fatal("session-manager override did not land on values.image")
	}
	if image["repository"] != "localhost:5001/educates-session-manager" || image["tag"] != tagLatest {
		t.Errorf("values.image = %v", image)
	}

	prePuller, ok := values["imagePrePuller"].(map[string]any)
	if !ok {
		t.Fatal("pause-container override did not land on values.imagePrePuller")
	}
	pause, ok := prePuller["pauseImage"].(map[string]any)
	if !ok || pause["repository"] != "localhost:5001/educates-pause-container" || pause["tag"] != tagLatest {
		t.Errorf("imagePrePuller.pauseImage = %v", prePuller["pauseImage"])
	}

	entries, ok := values["imageVersions"].([]any)
	if !ok || len(entries) != 1 {
		t.Fatalf("imageVersions should contain exactly the training-portal entry, got %v", values["imageVersions"])
	}
	entry := entries[0].(map[string]any)
	if entry["name"] != "training-portal" {
		t.Errorf("imageVersions[0] = %v", entry)
	}
}

func TestRenderSessionManagerValues_PrePullerComposition(t *testing.T) {
	enabled := true
	obj := &platformv1alpha1.SessionManager{
		Spec: platformv1alpha1.SessionManagerSpec{
			ImagePrePuller: &platformv1alpha1.ImagePrePuller{Enabled: enabled},
			Images: &platformv1alpha1.Images{
				Overrides: []platformv1alpha1.ImageOverride{
					{Name: "pause-container", Image: "localhost:5001/educates-pause-container:latest"},
				},
			},
		},
	}
	values := renderSessionManagerValues(obj, valuesTestClusterConfig())

	prePuller, ok := values["imagePrePuller"].(map[string]any)
	if !ok {
		t.Fatal("imagePrePuller missing")
	}
	if prePuller["enabled"] != true {
		t.Errorf("imagePrePuller.enabled = %v, want true (clobbered by pauseImage writer?)", prePuller["enabled"])
	}
	if _, ok := prePuller["pauseImage"].(map[string]any); !ok {
		t.Errorf("imagePrePuller.pauseImage = %v, want map (clobbered by enabled writer?)", prePuller["pauseImage"])
	}
}

func TestRenderNodeCAInjectorValues_ImageOverride(t *testing.T) {
	cfg := valuesTestClusterConfig()

	withOverride := &platformv1alpha1.SessionManager{
		Spec: platformv1alpha1.SessionManagerSpec{
			Images: &platformv1alpha1.Images{
				Overrides: []platformv1alpha1.ImageOverride{
					{Name: "node-ca-injector", Image: "localhost:5001/educates-node-ca-injector:latest"},
				},
			},
		},
	}
	values := renderNodeCAInjectorValues(withOverride, cfg)
	image, ok := values["image"].(map[string]any)
	if !ok {
		t.Fatal("node-ca-injector override did not land on the subchart's values.image")
	}
	if image["repository"] != "localhost:5001/educates-node-ca-injector" || image["tag"] != tagLatest {
		t.Errorf("values.image = %v", image)
	}

	without := &platformv1alpha1.SessionManager{}
	values = renderNodeCAInjectorValues(without, cfg)
	if _, present := values["image"]; present {
		t.Errorf("values.image should be absent without an override, got %v", values["image"])
	}
}

// The session manager reads the delivery the cluster config resolved, so the
// resolved value has to reach its chart values in the form the chart accepts,
// which is lowercased.
func TestApplySMPackageDeliveryValues(t *testing.T) {
	cases := []struct {
		name     string
		status   *configv1alpha1.StatusPackageDelivery
		expected any
	}{
		{
			name:     "enabled reaches the chart lowercased",
			status:   &configv1alpha1.StatusPackageDelivery{ImageMount: configv1alpha1.EffectiveDeliveryModeEnabled},
			expected: map[string]any{"imageMount": "enabled"},
		},
		{
			name:     "disabled reaches the chart lowercased",
			status:   &configv1alpha1.StatusPackageDelivery{ImageMount: configv1alpha1.EffectiveDeliveryModeDisabled},
			expected: map[string]any{"imageMount": "disabled"},
		},
		{
			// A cluster config which has not resolved yet leaves the chart
			// default in place rather than forcing a value.
			name:   "an unresolved status sets nothing",
			status: nil,
		},
		{
			name:   "an empty value sets nothing",
			status: &configv1alpha1.StatusPackageDelivery{},
		},
	}

	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			cfg := &configv1alpha1.EducatesClusterConfig{}
			cfg.Status.PackageDelivery = test.status

			values := map[string]any{}

			applySMPackageDeliveryValues(values, cfg)

			actual, found := values["packageDelivery"]

			if test.expected == nil {
				if found {
					t.Errorf("expected no value, got %v", actual)
				}

				return
			}

			if !found {
				t.Fatalf("expected packageDelivery to be set")
			}

			actualMap, ok := actual.(map[string]any)

			if !ok {
				t.Fatalf("packageDelivery is %T, want a map", actual)
			}

			expectedMap := test.expected.(map[string]any)

			if actualMap["imageMount"] != expectedMap["imageMount"] {
				t.Errorf("imageMount = %v, want %v", actualMap["imageMount"], expectedMap["imageMount"])
			}
		})
	}
}
