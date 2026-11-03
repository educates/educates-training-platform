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
	applySMIngressValues(values, &platformv1alpha1.SessionManager{}, cfg, "educates-installer")

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

	entries, ok := values["imageVersions"].([]any)
	if !ok || len(entries) != 1 {
		t.Fatalf("imageVersions should contain exactly the training-portal entry, got %v", values["imageVersions"])
	}
	entry := entries[0].(map[string]any)
	if entry["name"] != "training-portal" {
		t.Errorf("imageVersions[0] = %v", entry)
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
	values := renderNodeCAInjectorValues(withOverride, cfg, testOperatorNamespace)
	image, ok := values["image"].(map[string]any)
	if !ok {
		t.Fatal("node-ca-injector override did not land on the subchart's values.image")
	}
	if image["repository"] != "localhost:5001/educates-node-ca-injector" || image["tag"] != tagLatest {
		t.Errorf("values.image = %v", image)
	}

	without := &platformv1alpha1.SessionManager{}
	values = renderNodeCAInjectorValues(without, cfg, testOperatorNamespace)
	if _, present := values["image"]; present {
		t.Errorf("values.image should be absent without an override, got %v", values["image"])
	}
}

// The session manager reads the delivery the cluster config resolved, so the
// resolved value has to reach its chart values in the form the chart accepts,
// which is an enabled flag.
func TestApplySMPackageDeliveryValues(t *testing.T) {
	cases := []struct {
		name     string
		status   *configv1alpha1.StatusPackageDelivery
		set      bool
		expected bool
	}{
		{
			name:     "enabled reaches the chart as true",
			status:   &configv1alpha1.StatusPackageDelivery{ImageMount: configv1alpha1.EffectiveDeliveryModeEnabled},
			set:      true,
			expected: true,
		},
		{
			name:     "disabled reaches the chart as false",
			status:   &configv1alpha1.StatusPackageDelivery{ImageMount: configv1alpha1.EffectiveDeliveryModeDisabled},
			set:      true,
			expected: false,
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

			if !test.set {
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

			imageMount, ok := actualMap["imageMount"].(map[string]any)

			if !ok {
				t.Fatalf("packageDelivery.imageMount is %T, want a map", actualMap["imageMount"])
			}

			enabled, ok := imageMount["enabled"].(bool)

			if !ok {
				t.Fatalf("packageDelivery.imageMount.enabled is %T, want a bool", imageMount["enabled"])
			}

			if enabled != test.expected {
				t.Errorf("packageDelivery.imageMount.enabled = %v, want %v", enabled, test.expected)
			}
		})
	}
}

// securityValues returns the policy engines a values map gives the
// session-manager chart, "" for one it leaves unset.
func securityValues(values map[string]any) (policyEngine, rulesEngine string) {
	if cs, ok := values["clusterSecurity"].(map[string]any); ok {
		policyEngine, _ = cs["policyEngine"].(string)
	}
	if ws, ok := values["workshopSecurity"].(map[string]any); ok {
		rulesEngine, _ = ws["rulesEngine"].(string)
	}
	return policyEngine, rulesEngine
}

// TestRenderSessionManagerValues_PolicyEnginesFromClusterConfig asserts
// that the engines the cluster config publishes reach the chart, so an
// engine set to None stops session-manager enforcing Kyverno policies.
func TestRenderSessionManagerValues_PolicyEnginesFromClusterConfig(t *testing.T) {
	cfg := valuesTestClusterConfig()
	cfg.Status.PolicyEnforcement = &configv1alpha1.StatusPolicyEnforcement{
		ClusterPolicyEngine:  configv1alpha1.ClusterPolicyEngineNone,
		WorkshopPolicyEngine: configv1alpha1.WorkshopPolicyEngineNone,
	}

	policy, rules := securityValues(renderSessionManagerValues(&platformv1alpha1.SessionManager{}, cfg, "educates-installer"))
	if policy != string(configv1alpha1.ClusterPolicyEngineNone) || rules != string(platformv1alpha1.WorkshopPolicyEngineNone) {
		t.Errorf("engines = (%q, %q), want (None, None)", policy, rules)
	}
}

// TestRenderSessionManagerValues_WorkshopPolicyOverrideWins asserts that
// the SessionManager's workshop engine override replaces the cluster's,
// including when the cluster config publishes no engines.
func TestRenderSessionManagerValues_WorkshopPolicyOverrideWins(t *testing.T) {
	sm := &platformv1alpha1.SessionManager{Spec: platformv1alpha1.SessionManagerSpec{
		WorkshopPolicyOverride: &platformv1alpha1.WorkshopPolicyOverride{Engine: platformv1alpha1.WorkshopPolicyEngineNone},
	}}

	published := valuesTestClusterConfig()
	published.Status.PolicyEnforcement = &configv1alpha1.StatusPolicyEnforcement{
		ClusterPolicyEngine:  configv1alpha1.ClusterPolicyEngineKyverno,
		WorkshopPolicyEngine: configv1alpha1.WorkshopPolicyEngineKyverno,
	}
	unpublished := valuesTestClusterConfig()

	for name, cfg := range map[string]*configv1alpha1.EducatesClusterConfig{
		"engines published":    published,
		"no engines published": unpublished,
	} {
		policy, rules := securityValues(renderSessionManagerValues(sm, cfg, "educates-installer"))
		if policy != string(configv1alpha1.ClusterPolicyEngineKyverno) || rules != string(platformv1alpha1.WorkshopPolicyEngineNone) {
			t.Errorf("%s: engines = (%q, %q), want (Kyverno, None)", name, policy, rules)
		}
	}
}

// TestRenderSessionManagerValues_KyvernoWhenNoEnginesPublished asserts
// that a cluster config publishing no engines gives the chart Kyverno
// for both, its own defaults, stated so the Kyverno presence gate and
// the chart agree on what session-manager enforces.
func TestRenderSessionManagerValues_KyvernoWhenNoEnginesPublished(t *testing.T) {
	policy, rules := securityValues(renderSessionManagerValues(&platformv1alpha1.SessionManager{}, valuesTestClusterConfig(), "educates-installer"))
	if policy != string(configv1alpha1.ClusterPolicyEngineKyverno) || rules != string(platformv1alpha1.WorkshopPolicyEngineKyverno) {
		t.Errorf("engines = (%q, %q), want (Kyverno, Kyverno)", policy, rules)
	}
}
