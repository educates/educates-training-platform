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
	"maps"
	"slices"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/yaml"

	configv1alpha1 "github.com/educates/educates-training-platform/installer/operator/api/config/v1alpha1"
	platformv1alpha1 "github.com/educates/educates-training-platform/installer/operator/api/platform/v1alpha1"
	vendoredcharts "github.com/educates/educates-training-platform/installer/operator/vendored-charts"
)

// Plain unit tests of how the session-manager chart fans the image pull
// Secrets out of the platform namespace, where the EducatesClusterConfig
// reconciler copies them, into the namespaces the session manager
// creates: training portal, workshop environment and workshop session
// namespaces.

// namespaceSelector is the targetNamespaces of a SecretCopier or
// SecretInjector rule.
type namespaceSelector struct {
	LabelSelector struct {
		MatchLabels map[string]string `json:"matchLabels"`
	} `json:"labelSelector"`
}

// secretPropagationResource is the part of a rendered SecretCopier or
// SecretInjector the tests read.
type secretPropagationResource struct {
	Kind     string `json:"kind"`
	Metadata struct {
		Name string `json:"name"`
	} `json:"metadata"`
	Spec struct {
		Rules []struct {
			SourceSecret struct {
				Name      string `json:"name"`
				Namespace string `json:"namespace"`
			} `json:"sourceSecret"`
			SourceSecrets struct {
				NameSelector struct {
					MatchNames []string `json:"matchNames"`
				} `json:"nameSelector"`
			} `json:"sourceSecrets"`
			TargetNamespaces namespaceSelector `json:"targetNamespaces"`
			ServiceAccounts  map[string]any    `json:"serviceAccounts"`
		} `json:"rules"`
	} `json:"spec"`
}

// renderSecretPropagation renders the session-manager chart for a
// cluster config publishing pull Secrets and returns its rendered
// resource of the given kind and name.
func renderSecretPropagation(t *testing.T, kind, name string, pullSecrets ...string) secretPropagationResource {
	t.Helper()
	cfg := valuesTestClusterConfig()
	cfg.Status.ImageRegistry = &configv1alpha1.ImageRegistry{}
	for _, s := range pullSecrets {
		cfg.Status.ImageRegistry.PullSecrets = append(cfg.Status.ImageRegistry.PullSecrets,
			configv1alpha1.LocalObjectReference{Name: s})
	}
	values := renderSessionManagerValues(&platformv1alpha1.SessionManager{}, cfg, "educates-installer", testImageRegistryNamespace)

	for doc := range renderManifests(t, vendoredcharts.SessionManager, values) {
		var r secretPropagationResource
		if err := yaml.Unmarshal([]byte(doc), &r); err != nil {
			t.Fatalf("parse rendered manifest: %v", err)
		}
		if r.Kind == kind && r.Metadata.Name == name {
			return r
		}
	}
	t.Fatalf("no %s %q in the rendered session-manager chart", kind, name)
	return secretPropagationResource{}
}

var sessionNamespaceLabels = map[string]string{"training.educates.dev/component": "session"}

func TestSessionManagerChart_CopiesPullSecretsIntoSessionNamespaces(t *testing.T) {
	copier := renderSecretPropagation(t, "SecretCopier", "educates-downstream-image-pull-secrets", "mirror-pull-secret")

	for _, rule := range copier.Spec.Rules {
		if rule.SourceSecret.Name == "mirror-pull-secret" &&
			rule.SourceSecret.Namespace == platformNamespace &&
			maps.Equal(rule.TargetNamespaces.LabelSelector.MatchLabels, sessionNamespaceLabels) {
			return
		}
	}
	t.Errorf("no rule copies mirror-pull-secret from %s into namespaces labelled %v: %+v",
		platformNamespace, sessionNamespaceLabels, copier.Spec.Rules)
}

// The session manager names the pull Secrets on every ServiceAccount it
// creates, and on the default ServiceAccount of every namespace it
// creates, before any pod can use them: a pod takes its ServiceAccount's
// pull secrets only when it is created, which may be before secrets-manager
// injects them. It reads the names from its runtime config.
func TestSessionManagerChart_GivesTheRuntimeThePullSecretNames(t *testing.T) {
	cfg := valuesTestClusterConfig()
	cfg.Status.ImageRegistry = &configv1alpha1.ImageRegistry{
		PullSecrets: []configv1alpha1.LocalObjectReference{{Name: "mirror-pull-secret"}},
	}
	values := renderSessionManagerValues(&platformv1alpha1.SessionManager{}, cfg, "educates-installer", testImageRegistryNamespace)

	for doc := range renderManifests(t, vendoredcharts.SessionManager, values) {
		var s corev1.Secret
		if err := yaml.Unmarshal([]byte(doc), &s); err != nil {
			t.Fatalf("parse rendered manifest: %v", err)
		}
		runtimeConfig, ok := s.StringData["educates-operator-config.yaml"]
		if s.Name != "educates-config" || !ok {
			continue
		}
		var config struct {
			ImagePullSecrets []string `json:"imagePullSecrets"`
		}
		if err := yaml.Unmarshal([]byte(runtimeConfig), &config); err != nil {
			t.Fatalf("parse runtime config: %v", err)
		}
		if !slices.Equal(config.ImagePullSecrets, []string{"mirror-pull-secret"}) {
			t.Errorf("runtime config imagePullSecrets = %v, want [mirror-pull-secret]", config.ImagePullSecrets)
		}
		return
	}
	t.Fatal("no educates-config Secret in the rendered session-manager chart")
}

// The injector rule for session namespaces names no ServiceAccounts, so
// it covers every one there: the default ServiceAccount, and those the
// workshop's session objects create, such as vcluster's.
func TestSessionManagerChart_InjectsPullSecretsIntoEverySessionServiceAccount(t *testing.T) {
	injector := renderSecretPropagation(t, "SecretInjector", "educates-image-pull-secrets", "mirror-pull-secret")

	for _, rule := range injector.Spec.Rules {
		if maps.Equal(rule.TargetNamespaces.LabelSelector.MatchLabels, sessionNamespaceLabels) &&
			slices.Contains(rule.SourceSecrets.NameSelector.MatchNames, "mirror-pull-secret") &&
			rule.ServiceAccounts == nil {
			return
		}
	}
	t.Errorf("no rule injects mirror-pull-secret into every ServiceAccount of namespaces labelled %v: %+v",
		sessionNamespaceLabels, injector.Spec.Rules)
}
