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

	rbacv1 "k8s.io/api/rbac/v1"
	"sigs.k8s.io/yaml"

	platformv1alpha1 "github.com/educates/educates-training-platform/installer/operator/api/platform/v1alpha1"
	vendoredcharts "github.com/educates/educates-training-platform/installer/operator/vendored-charts"
)

// renderedObject is the slice of any rendered manifest document these
// tests look at.
type renderedObject struct {
	Kind     string `json:"kind"`
	Metadata struct {
		Name        string            `json:"name"`
		Namespace   string            `json:"namespace"`
		Annotations map[string]string `json:"annotations"`
	} `json:"metadata"`
}

// renderLookupServiceObjects renders the lookup-service chart with the
// values the operator gives it and returns each rendered document,
// parsed as its kind and metadata and kept as raw YAML.
func renderLookupServiceObjects(t *testing.T) ([]renderedObject, []string) {
	t.Helper()

	values := renderLookupServiceValues(&platformv1alpha1.LookupService{}, valuesTestClusterConfig(), testImageRegistryNamespace)

	var objects []renderedObject
	var docs []string
	for doc := range renderManifests(t, vendoredcharts.LookupService, values) {
		var o renderedObject
		if err := yaml.Unmarshal([]byte(doc), &o); err != nil {
			t.Fatalf("parse rendered manifest: %v", err)
		}
		if o.Kind == "" {
			continue
		}
		objects = append(objects, o)
		docs = append(docs, doc)
	}
	return objects, docs
}

func TestLookupServiceChart_CreatesConfigNamespaceKeptOnUninstall(t *testing.T) {
	objects, _ := renderLookupServiceObjects(t)

	for _, o := range objects {
		if o.Kind != "Namespace" || o.Metadata.Name != lookupServiceConfigNamespace {
			continue
		}
		if got := o.Metadata.Annotations["helm.sh/resource-policy"]; got != "keep" {
			t.Fatalf("Namespace educates-config has helm.sh/resource-policy %q, want %q", got, "keep")
		}
		return
	}
	t.Fatalf("the chart renders no Namespace educates-config")
}

func TestLookupServiceChart_ProbesLivenessEndpoint(t *testing.T) {
	values := renderLookupServiceValues(&platformv1alpha1.LookupService{}, valuesTestClusterConfig(), testImageRegistryNamespace)

	for _, w := range renderWorkloads(t, vendoredcharts.LookupService, values) {
		if w.Kind != "Deployment" || w.Metadata.Name != lookupServiceDeploymentName {
			continue
		}
		for _, c := range w.Spec.Template.Spec.Containers {
			if c.Name != "lookup-service" {
				continue
			}
			probe := c.LivenessProbe
			if probe == nil || probe.HTTPGet == nil {
				t.Fatalf("container lookup-service has liveness probe %+v, want an HTTP GET", probe)
			}
			if probe.HTTPGet.Path != "/healthz" || probe.HTTPGet.Port.IntValue() != 8081 {
				t.Errorf("liveness probe gets %s on port %s, want /healthz on 8081",
					probe.HTTPGet.Path, probe.HTTPGet.Port.String())
			}
			return
		}
	}
	t.Fatalf("the chart renders no lookup-service container")
}

func TestLookupServiceChart_GrantsOnlyInConfigNamespace(t *testing.T) {
	objects, docs := renderLookupServiceObjects(t)

	wantRules := []rbacv1.PolicyRule{
		{APIGroups: []string{""}, Resources: []string{"secrets"}, Verbs: []string{"list", "watch"}},
		{
			APIGroups: []string{"lookup.educates.dev"},
			Resources: []string{"clusterconfigs", "clientconfigs", "tenantconfigs"},
			Verbs:     []string{"list", "watch", "patch"},
		},
		{APIGroups: []string{""}, Resources: []string{"events"}, Verbs: []string{"create"}},
	}
	wantRoleRef := rbacv1.RoleRef{
		APIGroup: "rbac.authorization.k8s.io",
		Kind:     "Role",
		Name:     "educates-lookup-service",
	}
	wantSubjects := []rbacv1.Subject{
		{Kind: "ServiceAccount", Name: "lookup-service", Namespace: platformNamespace},
	}

	var role *rbacv1.Role
	var binding *rbacv1.RoleBinding
	for i, o := range objects {
		switch o.Kind {
		case "Role":
			role = &rbacv1.Role{}
			if err := yaml.Unmarshal([]byte(docs[i]), role); err != nil {
				t.Fatalf("parse Role: %v", err)
			}
		case "RoleBinding":
			binding = &rbacv1.RoleBinding{}
			if err := yaml.Unmarshal([]byte(docs[i]), binding); err != nil {
				t.Fatalf("parse RoleBinding: %v", err)
			}
		case "ClusterRoleBinding":
			t.Errorf("the chart renders ClusterRoleBinding %q", o.Metadata.Name)
		case "ClusterRole":
			if o.Metadata.Name == "educates:lookup-service" {
				t.Errorf("the chart renders ClusterRole %q", o.Metadata.Name)
			}
		}
	}

	if role == nil {
		t.Fatalf("the chart renders no Role")
	}
	if role.Name != "educates-lookup-service" || role.Namespace != lookupServiceConfigNamespace {
		t.Errorf("Role is %s/%s, want educates-config/educates-lookup-service", role.Namespace, role.Name)
	}
	if !reflect.DeepEqual(role.Rules, wantRules) {
		t.Errorf("Role rules are %+v, want %+v", role.Rules, wantRules)
	}

	if binding == nil {
		t.Fatalf("the chart renders no RoleBinding")
	}
	if binding.Name != "educates-lookup-service" || binding.Namespace != lookupServiceConfigNamespace {
		t.Errorf("RoleBinding is %s/%s, want educates-config/educates-lookup-service", binding.Namespace, binding.Name)
	}
	if binding.RoleRef != wantRoleRef {
		t.Errorf("RoleBinding roleRef is %+v, want %+v", binding.RoleRef, wantRoleRef)
	}
	if !reflect.DeepEqual(binding.Subjects, wantSubjects) {
		t.Errorf("RoleBinding subjects are %+v, want %+v", binding.Subjects, wantSubjects)
	}
}
