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
	"reflect"
	"slices"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	policyv1 "k8s.io/api/policy/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"sigs.k8s.io/yaml"

	configv1alpha1 "github.com/educates/educates-training-platform/installer/operator/api/config/v1alpha1"
	platformv1alpha1 "github.com/educates/educates-training-platform/installer/operator/api/platform/v1alpha1"
	vendoredcharts "github.com/educates/educates-training-platform/installer/operator/vendored-charts"
)

// secretsManagerManifest is the secrets-manager chart rendered with the
// values the operator gives it, each document kept as raw YAML under its
// kind and namespace/name.
type secretsManagerManifest map[string]string

// renderSecretsManagerChart renders the secrets-manager chart with the
// values the operator gives obj under the test cluster config, with extra
// set over them, as a standalone install of the chart may.
func renderSecretsManagerChart(t *testing.T, obj *platformv1alpha1.SecretsManager, cfg *configv1alpha1.EducatesClusterConfig,
	extra map[string]any) secretsManagerManifest {
	t.Helper()

	values := renderSecretsManagerValues(obj, cfg, testImageRegistryNamespace)
	maps.Copy(values, extra)

	manifest := secretsManagerManifest{}
	for doc := range renderManifests(t, vendoredcharts.SecretsManager, values) {
		var o renderedObject
		if err := yaml.Unmarshal([]byte(doc), &o); err != nil {
			t.Fatalf("parse rendered manifest: %v", err)
		}
		if o.Kind == "" {
			continue
		}
		manifest[o.Kind+" "+o.Metadata.Namespace+"/"+o.Metadata.Name] = doc
	}
	return manifest
}

// renderOperatorSecretsManagerChart renders the secrets-manager chart with
// the values the operator gives a SecretsManager with no spec.
func renderOperatorSecretsManagerChart(t *testing.T) secretsManagerManifest {
	t.Helper()
	return renderSecretsManagerChart(t, &platformv1alpha1.SecretsManager{}, valuesTestClusterConfig(), nil)
}

// has reports whether the manifest holds the object of kind named
// namespace/name, namespace empty for a cluster-scoped kind.
func (m secretsManagerManifest) has(kind, namespace, name string) bool {
	_, ok := m[kind+" "+namespace+"/"+name]
	return ok
}

// decode parses the object of kind named namespace/name into into, and fails
// the test when the manifest does not hold it.
func (m secretsManagerManifest) decode(t *testing.T, kind, namespace, name string, into any) {
	t.Helper()
	doc, ok := m[kind+" "+namespace+"/"+name]
	if !ok {
		t.Fatalf("the chart renders no %s %s/%s", kind, namespace, name)
	}
	if err := yaml.Unmarshal([]byte(doc), into); err != nil {
		t.Fatalf("parse %s %s/%s: %v", kind, namespace, name, err)
	}
}

// deployment returns the secrets-manager Deployment.
func (m secretsManagerManifest) deployment(t *testing.T) *appsv1.Deployment {
	t.Helper()
	deployment := &appsv1.Deployment{}
	m.decode(t, "Deployment", platformNamespace, secretsManagerDeploymentName, deployment)
	return deployment
}

// secretsManagerContainer returns the one container of the secrets-manager
// pod.
func secretsManagerContainer(t *testing.T, spec corev1.PodSpec) corev1.Container {
	t.Helper()
	if len(spec.Containers) != 1 {
		t.Fatalf("Deployment secrets-manager has containers %+v, want one", spec.Containers)
	}
	return spec.Containers[0]
}

// The image's entrypoint is the secrets-manager's binary, which takes its
// log level as a flag.
func TestSecretsManagerChart_PassesLogLevelAsItsOnlyFlag(t *testing.T) {
	for _, level := range []platformv1alpha1.LogLevel{platformv1alpha1.LogLevelDebug, platformv1alpha1.LogLevelError} {
		obj := &platformv1alpha1.SecretsManager{Spec: platformv1alpha1.SecretsManagerSpec{LogLevel: level}}
		c := secretsManagerContainer(t,
			renderSecretsManagerChart(t, obj, valuesTestClusterConfig(), nil).deployment(t).Spec.Template.Spec)

		if want := []string{"--log-level=" + string(level)}; !slices.Equal(c.Args, want) {
			t.Errorf("with logLevel %s, container %s has args %q, want %q", level, c.Name, c.Args, want)
		}
		if len(c.Command) != 0 || len(c.Env) != 0 {
			t.Errorf("container %s has command %q and env %+v, want neither", c.Name, c.Command, c.Env)
		}
	}
}

// Readiness means the caches have synced, so the Deployment becomes
// Available, and the SecretsManager Ready, only then.
func TestSecretsManagerChart_ProbesLivenessAndReadinessOn8081(t *testing.T) {
	c := secretsManagerContainer(t, renderOperatorSecretsManagerChart(t).deployment(t).Spec.Template.Spec)

	for _, probe := range []struct {
		name  string
		probe *corev1.Probe
		path  string
	}{
		{"liveness", c.LivenessProbe, "/healthz"},
		{"readiness", c.ReadinessProbe, "/readyz"},
	} {
		if probe.probe == nil || probe.probe.HTTPGet == nil {
			t.Errorf("container %s has %s probe %+v, want an HTTP GET", c.Name, probe.name, probe.probe)
			continue
		}
		if get := probe.probe.HTTPGet; get.Path != probe.path || get.Port.IntValue() != 8081 {
			t.Errorf("%s probe gets %s on port %s, want %s on 8081", probe.name, get.Path, get.Port.String(), probe.path)
		}
	}
}

// Replicas elect a leader, so a rolling update starts the new pod as a
// standby while the old one leads, which Recreate would not.
func TestSecretsManagerChart_RollsOutUpdates(t *testing.T) {
	deployment := renderOperatorSecretsManagerChart(t).deployment(t)
	if deployment.Spec.Strategy.Type != appsv1.RollingUpdateDeploymentStrategyType {
		t.Errorf("Deployment secrets-manager has strategy %q, want %q",
			deployment.Spec.Strategy.Type, appsv1.RollingUpdateDeploymentStrategyType)
	}
}

// The operator passes no replicas, so an operator install runs one pod, with
// no PodDisruptionBudget, which would block every drain of its node, and no
// anti-affinity.
func TestSecretsManagerChart_RunsOneReplicaForTheOperator(t *testing.T) {
	manifest := renderOperatorSecretsManagerChart(t)

	deployment := manifest.deployment(t)
	if deployment.Spec.Replicas == nil || *deployment.Spec.Replicas != 1 {
		t.Errorf("Deployment secrets-manager has replicas %v, want 1", deployment.Spec.Replicas)
	}
	if deployment.Spec.Template.Spec.Affinity != nil {
		t.Errorf("Deployment secrets-manager has affinity %+v, want none", deployment.Spec.Template.Spec.Affinity)
	}
	if manifest.has("PodDisruptionBudget", platformNamespace, "secrets-manager") {
		t.Errorf("the chart renders PodDisruptionBudget secrets-manager for one replica")
	}
}

// A null replicas value removes the chart's default, and the chart still
// asks for one pod, where an empty replicas field would leave the number to
// the API server.
func TestSecretsManagerChart_RunsOneReplicaForNullReplicas(t *testing.T) {
	manifest := renderSecretsManagerChart(t, &platformv1alpha1.SecretsManager{}, valuesTestClusterConfig(),
		map[string]any{"replicas": nil})

	deployment := manifest.deployment(t)
	if deployment.Spec.Replicas == nil || *deployment.Spec.Replicas != 1 {
		t.Errorf("Deployment secrets-manager has replicas %v, want 1", deployment.Spec.Replicas)
	}
	if manifest.has("PodDisruptionBudget", platformNamespace, "secrets-manager") {
		t.Errorf("the chart renders PodDisruptionBudget secrets-manager for one replica")
	}
}

// A standalone install can run a standby: above one replica the chart keeps
// a pod through voluntary disruptions and prefers to spread the pods over
// nodes, so a standby survives the loss of the leader's node.
func TestSecretsManagerChart_SpreadsReplicasAboveOne(t *testing.T) {
	manifest := renderSecretsManagerChart(t, &platformv1alpha1.SecretsManager{}, valuesTestClusterConfig(),
		map[string]any{"replicas": 2})

	deployment := manifest.deployment(t)
	if deployment.Spec.Replicas == nil || *deployment.Spec.Replicas != 2 {
		t.Errorf("Deployment secrets-manager has replicas %v, want 2", deployment.Spec.Replicas)
	}
	podLabels := deployment.Spec.Template.Labels

	budget := &policyv1.PodDisruptionBudget{}
	manifest.decode(t, "PodDisruptionBudget", platformNamespace, "secrets-manager", budget)
	if budget.Spec.MaxUnavailable == nil || budget.Spec.MaxUnavailable.IntValue() != 1 {
		t.Errorf("PodDisruptionBudget secrets-manager has maxUnavailable %v, want 1", budget.Spec.MaxUnavailable)
	}
	if budget.Spec.Selector == nil || !selectsLabels(budget.Spec.Selector.MatchLabels, podLabels) {
		t.Errorf("PodDisruptionBudget secrets-manager selects %+v, want the secrets-manager pods %v",
			budget.Spec.Selector, podLabels)
	}

	affinity := deployment.Spec.Template.Spec.Affinity
	if affinity == nil || affinity.PodAntiAffinity == nil {
		t.Fatalf("Deployment secrets-manager has affinity %+v, want pod anti-affinity", affinity)
	}
	if len(affinity.PodAntiAffinity.RequiredDuringSchedulingIgnoredDuringExecution) != 0 {
		t.Errorf("Deployment secrets-manager requires pod anti-affinity, want it only preferred")
	}
	preferred := affinity.PodAntiAffinity.PreferredDuringSchedulingIgnoredDuringExecution
	if len(preferred) != 1 {
		t.Fatalf("Deployment secrets-manager prefers pod anti-affinity terms %+v, want one", preferred)
	}
	term := preferred[0].PodAffinityTerm
	if term.TopologyKey != corev1.LabelHostname || term.LabelSelector == nil ||
		!selectsLabels(term.LabelSelector.MatchLabels, podLabels) {
		t.Errorf("Deployment secrets-manager prefers anti-affinity term %+v, want the secrets-manager pods by %s",
			term, corev1.LabelHostname)
	}
}

// selectsLabels reports whether a non-empty selector's labels are all among
// labels.
func selectsLabels(selector, labels map[string]string) bool {
	if len(selector) == 0 {
		return false
	}
	for key, value := range selector {
		if labels[key] != value {
			return false
		}
	}
	return true
}

// The pod authenticates with the token the kubelet projects and rotates,
// never a long-lived token Secret.
func TestSecretsManagerChart_UsesAProjectedServiceAccountToken(t *testing.T) {
	manifest := renderOperatorSecretsManagerChart(t)
	spec := manifest.deployment(t).Spec.Template.Spec

	if spec.AutomountServiceAccountToken == nil || !*spec.AutomountServiceAccountToken {
		t.Errorf("Deployment secrets-manager has automountServiceAccountToken %v, want true",
			spec.AutomountServiceAccountToken)
	}
	if len(spec.Volumes) != 0 {
		t.Errorf("Deployment secrets-manager has volumes %+v, want none", spec.Volumes)
	}
	for key, doc := range manifest {
		secret := &corev1.Secret{}
		if err := yaml.Unmarshal([]byte(doc), secret); err != nil {
			t.Fatalf("parse %s: %v", key, err)
		}
		if secret.Type == corev1.SecretTypeServiceAccountToken {
			t.Errorf("the chart renders %s, a %s Secret", key, secret.Type)
		}
	}
}

func TestSecretsManagerChart_HardensThePod(t *testing.T) {
	spec := renderOperatorSecretsManagerChart(t).deployment(t).Spec.Template.Spec

	pod := spec.SecurityContext
	if pod == nil || pod.RunAsNonRoot == nil || !*pod.RunAsNonRoot ||
		pod.SeccompProfile == nil || pod.SeccompProfile.Type != corev1.SeccompProfileTypeRuntimeDefault {
		t.Errorf("Deployment secrets-manager has pod security context %+v, want runAsNonRoot and the %s seccomp profile",
			pod, corev1.SeccompProfileTypeRuntimeDefault)
	}
	c := secretsManagerContainer(t, spec)
	sc := c.SecurityContext
	if sc == nil || sc.AllowPrivilegeEscalation == nil || *sc.AllowPrivilegeEscalation ||
		sc.ReadOnlyRootFilesystem == nil || !*sc.ReadOnlyRootFilesystem ||
		sc.Capabilities == nil || !reflect.DeepEqual(sc.Capabilities.Drop, []corev1.Capability{"ALL"}) ||
		len(sc.Capabilities.Add) != 0 {
		t.Errorf("container %s has security context %+v, want no privilege escalation, a read-only root "+
			"filesystem and every capability dropped", c.Name, sc)
	}
}

// secretsManagerSubject is the service account the secrets-manager runs as.
var secretsManagerSubject = []rbacv1.Subject{
	{Kind: "ServiceAccount", Name: "secrets-manager", Namespace: platformNamespace},
}

// The secrets-manager reads Secrets everywhere and writes its copies,
// deleting one only to recreate it when its source changed type; adds names
// to service accounts; reads the four kinds and writes only their status;
// sets blockOwnerDeletion on the copies a SecretCopier or SecretImporter
// owns; and records Events cluster wide, those of the cluster-scoped kinds
// in the default namespace. It never deletes a collection, and never writes
// the four kinds themselves.
func TestSecretsManagerChart_GrantsWhatTheSecretsManagerUses(t *testing.T) {
	manifest := renderOperatorSecretsManagerChart(t)

	role := &rbacv1.ClusterRole{}
	manifest.decode(t, "ClusterRole", "", "educates:secrets-manager", role)
	kinds := []string{"secretcopiers", "secretexporters", "secretimporters", "secretinjectors"}
	want := []rbacv1.PolicyRule{
		{APIGroups: []string{""}, Resources: []string{"secrets"},
			Verbs: []string{"get", "list", "watch", "create", "update", "patch", "delete"}},
		{APIGroups: []string{""}, Resources: []string{"namespaces"}, Verbs: []string{"get", "list", "watch"}},
		{APIGroups: []string{""}, Resources: []string{"serviceaccounts"},
			Verbs: []string{"get", "list", "watch", "update", "patch"}},
		{APIGroups: []string{"secrets.educates.dev"}, Resources: kinds, Verbs: []string{"get", "list", "watch"}},
		{APIGroups: []string{"secrets.educates.dev"},
			Resources: []string{"secretcopiers/status", "secretexporters/status", "secretimporters/status",
				"secretinjectors/status"},
			Verbs: []string{"update", "patch"}},
		{APIGroups: []string{"secrets.educates.dev"},
			Resources: []string{"secretcopiers/finalizers", "secretimporters/finalizers"}, Verbs: []string{"update"}},
		{APIGroups: []string{"events.k8s.io"}, Resources: []string{"events"}, Verbs: []string{"create", "patch"}},
	}
	if !reflect.DeepEqual(role.Rules, want) {
		t.Errorf("ClusterRole educates:secrets-manager rules are\n%+v\nwant\n%+v", role.Rules, want)
	}

	binding := &rbacv1.ClusterRoleBinding{}
	manifest.decode(t, "ClusterRoleBinding", "", "educates:secrets-manager", binding)
	wantRoleRef := rbacv1.RoleRef{APIGroup: "rbac.authorization.k8s.io", Kind: "ClusterRole", Name: "educates:secrets-manager"}
	if binding.RoleRef != wantRoleRef || !reflect.DeepEqual(binding.Subjects, secretsManagerSubject) {
		t.Errorf("ClusterRoleBinding educates:secrets-manager binds %+v to %+v, want %+v to %+v",
			binding.RoleRef, binding.Subjects, wantRoleRef, secretsManagerSubject)
	}
}

// Leader election uses the Lease secrets-manager in the release namespace,
// and controller-runtime records each change of leader as a core Event
// there.
func TestSecretsManagerChart_GrantsLeaderElectionInItsNamespace(t *testing.T) {
	manifest := renderOperatorSecretsManagerChart(t)
	name := "educates-secrets-manager-leader-election"

	role := &rbacv1.Role{}
	manifest.decode(t, "Role", platformNamespace, name, role)
	want := []rbacv1.PolicyRule{
		{APIGroups: []string{"coordination.k8s.io"}, Resources: []string{"leases"}, Verbs: []string{"create"}},
		{APIGroups: []string{"coordination.k8s.io"}, Resources: []string{"leases"},
			ResourceNames: []string{"secrets-manager"}, Verbs: []string{"get", "update"}},
		{APIGroups: []string{""}, Resources: []string{"events"}, Verbs: []string{"create", "patch"}},
	}
	if !reflect.DeepEqual(role.Rules, want) {
		t.Errorf("Role %s/%s rules are\n%+v\nwant\n%+v", platformNamespace, name, role.Rules, want)
	}

	binding := &rbacv1.RoleBinding{}
	manifest.decode(t, "RoleBinding", platformNamespace, name, binding)
	wantRoleRef := rbacv1.RoleRef{APIGroup: "rbac.authorization.k8s.io", Kind: "Role", Name: name}
	if binding.RoleRef != wantRoleRef || !reflect.DeepEqual(binding.Subjects, secretsManagerSubject) {
		t.Errorf("RoleBinding %s/%s binds %+v to %+v, want %+v to %+v", platformNamespace, name,
			binding.RoleRef, binding.Subjects, wantRoleRef, secretsManagerSubject)
	}
}

// The user-facing roles keep their names and aggregation labels, so existing
// bindings keep working. The view role leaves out SecretExporters and
// SecretImporters, whose shared secrets authorize an import.
func TestSecretsManagerChart_UserRolesKeepTheirAggregation(t *testing.T) {
	manifest := renderOperatorSecretsManagerChart(t)

	for _, tc := range []struct {
		name      string
		aggregate string
		rule      rbacv1.PolicyRule
	}{
		{"educates:rbac:admin:secrets", "rbac.authorization.k8s.io/aggregate-to-admin", rbacv1.PolicyRule{
			APIGroups: []string{"secrets.educates.dev"},
			Resources: []string{"secretcopiers", "secretexporters", "secretimporters", "secretinjectors"},
			Verbs:     []string{"create", "delete", "deletecollection", "get", "list", "patch", "update", "watch"},
		}},
		{"educates:rbac:view:secrets", "rbac.authorization.k8s.io/aggregate-to-view", rbacv1.PolicyRule{
			APIGroups: []string{"secrets.educates.dev"},
			Resources: []string{"secretcopiers", "secretinjectors"},
			Verbs:     []string{"get", "list", "watch"},
		}},
	} {
		role := &rbacv1.ClusterRole{}
		manifest.decode(t, "ClusterRole", "", tc.name, role)
		if role.Labels[tc.aggregate] != "true" {
			t.Errorf("ClusterRole %s has labels %v, want %s=true", tc.name, role.Labels, tc.aggregate)
		}
		if want := []rbacv1.PolicyRule{tc.rule}; !reflect.DeepEqual(role.Rules, want) {
			t.Errorf("ClusterRole %s rules are %+v, want %+v", tc.name, role.Rules, want)
		}
	}
}

// On OpenShift the secrets-manager's service account is bound to the
// baseline SCC the session-manager chart installs.
func TestSecretsManagerChart_BindsTheBaselineSCCOnOpenShift(t *testing.T) {
	cfg := valuesTestClusterConfig()
	if renderSecretsManagerChart(t, &platformv1alpha1.SecretsManager{}, cfg, nil).
		has("ClusterRoleBinding", "", "educates:secrets-manager:scc") {
		t.Errorf("without OpenShiftSCC, the chart renders ClusterRoleBinding educates:secrets-manager:scc")
	}

	cfg.Status.PolicyEnforcement = &configv1alpha1.StatusPolicyEnforcement{
		ClusterPolicyEngine: configv1alpha1.ClusterPolicyEngineOpenShiftSCC,
	}
	binding := &rbacv1.ClusterRoleBinding{}
	renderSecretsManagerChart(t, &platformv1alpha1.SecretsManager{}, cfg, nil).
		decode(t, "ClusterRoleBinding", "", "educates:secrets-manager:scc", binding)
	wantRoleRef := rbacv1.RoleRef{APIGroup: "rbac.authorization.k8s.io", Kind: "ClusterRole", Name: "educates:scc:baseline"}
	if binding.RoleRef != wantRoleRef || !reflect.DeepEqual(binding.Subjects, secretsManagerSubject) {
		t.Errorf("ClusterRoleBinding educates:secrets-manager:scc binds %+v to %+v, want %+v to %+v",
			binding.RoleRef, binding.Subjects, wantRoleRef, secretsManagerSubject)
	}
}
