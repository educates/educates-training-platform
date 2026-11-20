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
	"slices"
	"testing"

	corev1 "k8s.io/api/core/v1"
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

// renderLookupServicePodSpec renders the lookup-service chart with the
// values the operator gives it and returns the pod spec of its Deployment.
func renderLookupServicePodSpec(t *testing.T) corev1.PodSpec {
	t.Helper()
	return renderLookupServicePodSpecFor(t, &platformv1alpha1.LookupService{})
}

// renderLookupServicePodSpecFor renders the lookup-service chart with the
// values the operator gives it for obj and returns the pod spec of its
// Deployment.
func renderLookupServicePodSpecFor(t *testing.T, obj *platformv1alpha1.LookupService) corev1.PodSpec {
	t.Helper()

	values := renderLookupServiceValues(obj, valuesTestClusterConfig(), testImageRegistryNamespace)

	for _, w := range renderWorkloads(t, vendoredcharts.LookupService, values) {
		if w.Kind == "Deployment" && w.Metadata.Name == lookupServiceDeploymentName {
			return w.Spec.Template.Spec
		}
	}
	t.Fatalf("the chart renders no Deployment lookup-service")
	return corev1.PodSpec{}
}

// lookupServiceContainer returns the lookup-service container of a pod spec.
func lookupServiceContainer(t *testing.T, spec corev1.PodSpec) corev1.Container {
	t.Helper()
	for _, c := range spec.Containers {
		if c.Name == "lookup-service" {
			return c
		}
	}
	t.Fatalf("the chart renders no lookup-service container")
	return corev1.Container{}
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
	for _, c := range renderLookupServicePodSpec(t).Containers {
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
	t.Fatalf("the chart renders no lookup-service container")
}

// The service reports ready once it has loaded its own configuration, so
// the Deployment becomes Available, and the LookupService Ready, only then.
func TestLookupServiceChart_ProbesReadinessEndpoint(t *testing.T) {
	probe := lookupServiceContainer(t, renderLookupServicePodSpec(t)).ReadinessProbe
	if probe == nil || probe.HTTPGet == nil {
		t.Fatalf("container lookup-service has readiness probe %+v, want an HTTP GET", probe)
	}
	if probe.HTTPGet.Path != "/readyz" || probe.HTTPGet.Port.IntValue() != 8081 {
		t.Errorf("readiness probe gets %s on port %s, want /readyz on 8081",
			probe.HTTPGet.Path, probe.HTTPGet.Port.String())
	}
}

func TestLookupServiceChart_RunsNonRoot(t *testing.T) {
	spec := renderLookupServicePodSpec(t)
	if spec.SecurityContext == nil || spec.SecurityContext.RunAsNonRoot == nil || !*spec.SecurityContext.RunAsNonRoot {
		t.Errorf("Deployment lookup-service has pod security context %+v, want runAsNonRoot true", spec.SecurityContext)
	}
	sc := lookupServiceContainer(t, spec).SecurityContext
	if sc == nil || sc.AllowPrivilegeEscalation == nil || *sc.AllowPrivilegeEscalation {
		t.Errorf("container lookup-service has security context %+v, want allowPrivilegeEscalation false", sc)
	}
}

// The service loads the CA trust Secret's ca.crt itself, from the file its
// --ingress-ca-file flag names, so the flag must name the file the chart
// mounts.
func TestLookupServiceChart_PointsServiceAtMountedCA(t *testing.T) {
	spec := renderLookupServicePodSpec(t)
	caSecret := valuesTestClusterConfig().Status.Ingress.CACertificateSecretRef.Name

	caVolume := ""
	for _, v := range spec.Volumes {
		if v.Secret != nil && v.Secret.SecretName == caSecret {
			caVolume = v.Name
		}
	}
	if caVolume == "" {
		t.Fatalf("Deployment lookup-service mounts no CA trust Secret %q", caSecret)
	}

	c := lookupServiceContainer(t, spec)
	for _, m := range c.VolumeMounts {
		if m.Name != caVolume {
			continue
		}
		want := "--ingress-ca-file=" + m.MountPath + "/ca.crt"
		if !slices.Contains(c.Args, want) {
			t.Errorf("container lookup-service has args %q, want them to hold %q", c.Args, want)
		}
		return
	}
	t.Fatalf("container lookup-service does not mount volume %q", caVolume)
}

func TestLookupServiceChart_PassesLogLevelToService(t *testing.T) {
	for _, level := range []platformv1alpha1.LogLevel{platformv1alpha1.LogLevelDebug, platformv1alpha1.LogLevelError} {
		obj := &platformv1alpha1.LookupService{Spec: platformv1alpha1.LookupServiceSpec{LogLevel: level}}
		c := lookupServiceContainer(t, renderLookupServicePodSpecFor(t, obj))

		want := "--log-level=" + string(level)
		if !slices.Contains(c.Args, want) {
			t.Errorf("with logLevel %s, container lookup-service has args %q, want them to hold %q", level, c.Args, want)
		}
	}
}

// The remote-access-token Secret exists only while SessionManager installs
// remote access, so lookup-service's pod has to start without it.
func TestLookupServiceChart_MountsRemoteAccessTokenOptionally(t *testing.T) {
	for _, v := range renderLookupServicePodSpec(t).Volumes {
		if v.Secret == nil || v.Secret.SecretName != "remote-access-token" {
			continue
		}
		if v.Secret.Optional == nil || !*v.Secret.Optional {
			t.Errorf("volume %q mounts Secret remote-access-token with optional %v, want true",
				v.Name, v.Secret.Optional)
		}
		return
	}
	t.Fatalf("Deployment lookup-service mounts no remote-access-token Secret")
}

// lookupServiceRBAC renders the lookup-service chart with the values the
// operator gives it and returns its Roles and RoleBindings, keyed by
// namespace/name. It fails the test when the chart binds anything cluster
// wide to lookup-service.
func lookupServiceRBAC(t *testing.T) (map[string]*rbacv1.Role, map[string]*rbacv1.RoleBinding) {
	t.Helper()
	objects, docs := renderLookupServiceObjects(t)

	roles := map[string]*rbacv1.Role{}
	bindings := map[string]*rbacv1.RoleBinding{}
	for i, o := range objects {
		key := o.Metadata.Namespace + "/" + o.Metadata.Name
		switch o.Kind {
		case "Role":
			role := &rbacv1.Role{}
			if err := yaml.Unmarshal([]byte(docs[i]), role); err != nil {
				t.Fatalf("parse Role: %v", err)
			}
			roles[key] = role
		case "RoleBinding":
			binding := &rbacv1.RoleBinding{}
			if err := yaml.Unmarshal([]byte(docs[i]), binding); err != nil {
				t.Fatalf("parse RoleBinding: %v", err)
			}
			bindings[key] = binding
		case "ClusterRoleBinding":
			t.Errorf("the chart renders ClusterRoleBinding %q", o.Metadata.Name)
		case "ClusterRole":
			if o.Metadata.Name == "educates:lookup-service" {
				t.Errorf("the chart renders ClusterRole %q", o.Metadata.Name)
			}
		}
	}
	return roles, bindings
}

// assertRoleGrantedToLookupService fails unless the chart renders the Role
// namespace/name with exactly the given rules, and a RoleBinding of the same
// name binding it to lookup-service's service account.
func assertRoleGrantedToLookupService(t *testing.T, namespace, name string, wantRules []rbacv1.PolicyRule) {
	t.Helper()
	roles, bindings := lookupServiceRBAC(t)
	key := namespace + "/" + name

	role, ok := roles[key]
	if !ok {
		t.Fatalf("the chart renders no Role %s", key)
	}
	if !reflect.DeepEqual(role.Rules, wantRules) {
		t.Errorf("Role %s rules are %+v, want %+v", key, role.Rules, wantRules)
	}

	binding, ok := bindings[key]
	if !ok {
		t.Fatalf("the chart renders no RoleBinding %s", key)
	}
	wantRoleRef := rbacv1.RoleRef{APIGroup: "rbac.authorization.k8s.io", Kind: "Role", Name: name}
	if binding.RoleRef != wantRoleRef {
		t.Errorf("RoleBinding %s roleRef is %+v, want %+v", key, binding.RoleRef, wantRoleRef)
	}
	wantSubjects := []rbacv1.Subject{
		{Kind: "ServiceAccount", Name: "lookup-service", Namespace: platformNamespace},
	}
	if !reflect.DeepEqual(binding.Subjects, wantSubjects) {
		t.Errorf("RoleBinding %s subjects are %+v, want %+v", key, binding.Subjects, wantSubjects)
	}
}

// The service only reads its configuration namespace: it watches its
// configuration resources and the kubeconfig Secrets they reference, and
// writes nothing there.
func TestLookupServiceChart_GrantsConfigNamespaceAccess(t *testing.T) {
	assertRoleGrantedToLookupService(t, lookupServiceConfigNamespace, "educates-lookup-service", []rbacv1.PolicyRule{
		{APIGroups: []string{""}, Resources: []string{"secrets"}, Verbs: []string{"list", "watch"}},
		{
			APIGroups: []string{"lookup.educates.dev"},
			Resources: []string{"clusterconfigs", "clientconfigs", "tenantconfigs"},
			Verbs:     []string{"list", "watch"},
		},
	})
}

// The service keeps its token signing key and the clients' revocation state
// in the Secret lookup-service-tokens of its own namespace, which it creates
// on its first start. Kubernetes cannot restrict create by name, so create
// covers the namespace's Secrets, and get and update only that one.
func TestLookupServiceChart_GrantsItsTokenSecret(t *testing.T) {
	assertRoleGrantedToLookupService(t, platformNamespace, "educates-lookup-service-tokens", []rbacv1.PolicyRule{
		{APIGroups: []string{""}, Resources: []string{"secrets"}, Verbs: []string{"create"}},
		{
			APIGroups:     []string{""},
			Resources:     []string{"secrets"},
			ResourceNames: []string{"lookup-service-tokens"},
			Verbs:         []string{"get", "update"},
		},
	})
}
