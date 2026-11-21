package scenarios

import (
	"context"
	"encoding/json"
	"slices"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	utilrand "k8s.io/apimachinery/pkg/util/rand"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	group      = "secrets.educates.dev"
	version    = "v1beta1"
	apiVersion = group + "/" + version

	kindSecretCopier   = "SecretCopier"
	kindSecretExporter = "SecretExporter"
	kindSecretImporter = "SecretImporter"
	kindSecretInjector = "SecretInjector"

	// typeDockerConfigJSON and typeDockerCfg are the Secret types of image
	// pull credentials.
	typeDockerConfigJSON = "kubernetes.io/dockerconfigjson"
	typeDockerCfg        = "kubernetes.io/dockercfg"

	// scenarioLabel labels the namespaces a scenario selects by label
	// with the scenario's id, so that no other namespace matches.
	scenarioLabel = "scenario"

	// holdFinalizer keeps a scenario's Secret from being removed until the
	// scenario ends.
	holdFinalizer = "scenarios.secrets.educates.dev/hold"
)

// Labels of scenarios, described in the package documentation.
const (
	expectedDifference = "expected-difference"
	liveOnly           = "live-only"

	areaCopying      = "copying"
	areaSelection    = "selection"
	areaOwnership    = "ownership"
	areaExportImport = "export-import"
	areaInjection    = "injection"
	areaTargetFields = "target-fields"
	areaFeedback     = "feedback"
	areaAdmission    = "admission"
	areaConvergence  = "convergence"
)

// scenario creates the resources of one scenario under names unique to it,
// and deletes them when the scenario ends.
type scenario struct {
	id  string
	ctx context.Context
}

// newScenario starts a scenario.
func newScenario() *scenario {
	return &scenario{id: "smt-" + utilrand.String(6), ctx: context.Background()}
}

// name returns a name unique to the scenario, ending in part.
func (s *scenario) name(part string) string {
	return s.id + "-" + part
}

// scope is the label set of the namespaces the scenario selects by label.
func (s *scenario) scope() map[string]string {
	return map[string]string{scenarioLabel: s.id}
}

// namespace creates the namespace named s.name(part) with labels.
func (s *scenario) namespace(part string, labels map[string]string) *corev1.Namespace {
	GinkgoHelper()
	return s.namespaceNamed(s.name(part), labels)
}

// namespaceNamed creates the namespace name with labels.
func (s *scenario) namespaceNamed(name string, labels map[string]string) *corev1.Namespace {
	GinkgoHelper()
	ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: name, Labels: labels}}
	s.create(ns)
	return ns
}

// ownedNamespace creates the namespace s.name(part), owned by the namespace
// owner.
func (s *scenario) ownedNamespace(part string, owner *corev1.Namespace) *corev1.Namespace {
	GinkgoHelper()
	ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{
		Name: s.name(part),
		OwnerReferences: []metav1.OwnerReference{{
			APIVersion: "v1", Kind: "Namespace", Name: owner.Name, UID: owner.UID,
		}},
	}}
	s.create(ns)
	return ns
}

// create creates obj, and deletes it when the scenario ends. A namespaced
// object goes with its namespace instead.
func (s *scenario) create(obj client.Object) {
	GinkgoHelper()
	Expect(target.Client.Create(s.ctx, obj)).To(Succeed())
	if obj.GetNamespace() == "" {
		DeferCleanup(s.deleteObject, obj)
	}
}

// createRejected asserts that the API server rejects obj as invalid.
func (s *scenario) createRejected(obj client.Object) {
	GinkgoHelper()
	err := target.Client.Create(s.ctx, obj)
	if err == nil {
		DeferCleanup(s.deleteObject, obj)
	}
	Expect(apierrors.IsInvalid(err)).To(BeTrue(), "creating %s %s should be rejected as invalid, got %v",
		obj.GetObjectKind().GroupVersionKind().Kind, obj.GetName(), err)
}

// deleteObject deletes obj, which may be gone already.
func (s *scenario) deleteObject(obj client.Object) {
	GinkgoHelper()
	Expect(client.IgnoreNotFound(target.Client.Delete(s.ctx, obj))).To(Succeed())
}

// update applies mutate to the latest version of obj and writes it, again
// on a write conflict. obj holds the version written.
func (s *scenario) update(obj client.Object, mutate func()) {
	GinkgoHelper()
	Expect(retry.RetryOnConflict(retry.DefaultRetry, func() error {
		if err := target.Client.Get(s.ctx, client.ObjectKeyFromObject(obj), obj); err != nil {
			return err
		}
		mutate()
		return target.Client.Update(s.ctx, obj)
	})).To(Succeed())
}

// patchStatus applies mutate to the status of the latest version of u, a
// secrets.educates.dev resource, and writes it through the status
// subresource, as another writer would.
func (s *scenario) patchStatus(u *unstructured.Unstructured, mutate func(status map[string]any)) {
	GinkgoHelper()
	latest := &unstructured.Unstructured{}
	latest.SetGroupVersionKind(u.GroupVersionKind())
	Expect(target.Client.Get(s.ctx, client.ObjectKeyFromObject(u), latest)).To(Succeed())
	updated := latest.DeepCopy()
	status, _, err := unstructured.NestedMap(updated.Object, "status")
	Expect(err).NotTo(HaveOccurred())
	if status == nil {
		status = map[string]any{}
	}
	mutate(status)
	Expect(unstructured.SetNestedMap(updated.Object, status, "status")).To(Succeed())
	Expect(target.Client.Status().Patch(s.ctx, updated, client.MergeFrom(latest))).To(Succeed())
}

// data builds Secret data from alternating keys and values.
func data(keysAndValues ...string) map[string][]byte {
	result := map[string][]byte{}
	for i := 0; i+1 < len(keysAndValues); i += 2 {
		result[keysAndValues[i]] = []byte(keysAndValues[i+1])
	}
	return result
}

// newSecret builds an Opaque Secret in namespace ns with data, changed by
// each of adjust, without creating it.
func newSecret(ns, name string, secretData map[string][]byte, adjust ...func(*corev1.Secret)) *corev1.Secret {
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns},
		Type:       corev1.SecretTypeOpaque,
		Data:       secretData,
	}
	for _, change := range adjust {
		change(secret)
	}
	return secret
}

// secret creates an Opaque Secret in namespace ns with data, changed first
// by each of adjust.
func (s *scenario) secret(ns, name string, secretData map[string][]byte,
	adjust ...func(*corev1.Secret)) *corev1.Secret {
	GinkgoHelper()
	secret := newSecret(ns, name, secretData, adjust...)
	s.create(secret)
	return secret
}

// pullSecret creates a kubernetes.io/dockerconfigjson Secret, changed first
// by each of adjust.
func (s *scenario) pullSecret(ns, name string, adjust ...func(*corev1.Secret)) *corev1.Secret {
	GinkgoHelper()
	return s.secret(ns, name,
		data(corev1.DockerConfigJsonKey, `{"auths":{"registry.example.com":{"auth":"dXNlcjpwYXNz"}}}`),
		append([]func(*corev1.Secret){withType(typeDockerConfigJSON)}, adjust...)...)
}

// withLabels sets labels on a Secret.
func withLabels(labels map[string]string) func(*corev1.Secret) {
	return func(secret *corev1.Secret) { secret.Labels = labels }
}

// withType sets the type of a Secret.
func withType(secretType corev1.SecretType) func(*corev1.Secret) {
	return func(secret *corev1.Secret) { secret.Type = secretType }
}

// setMapEntry sets key to value in the map m points to, creating the map.
func setMapEntry(m *map[string]string, key, value string) {
	if *m == nil {
		*m = map[string]string{}
	}
	(*m)[key] = value
}

// serviceAccount creates a service account in namespace ns.
func (s *scenario) serviceAccount(ns, name string, labels map[string]string) {
	GinkgoHelper()
	s.create(&corev1.ServiceAccount{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns, Labels: labels}})
}

// resource builds a secrets.educates.dev resource of kind with spec.
func resource(kind, namespace, name string, spec object) *unstructured.Unstructured {
	GinkgoHelper()
	u := &unstructured.Unstructured{Object: jsonObject(object{"spec": spec})}
	u.SetAPIVersion(apiVersion)
	u.SetKind(kind)
	u.SetName(name)
	if namespace != "" {
		u.SetNamespace(namespace)
	}
	return u
}

// jsonObject turns v into the JSON object it marshals to, so that slices
// and structs of any type become the []any and map[string]any an
// unstructured object holds.
func jsonObject(v any) map[string]any {
	GinkgoHelper()
	raw, err := json.Marshal(v)
	Expect(err).NotTo(HaveOccurred())
	result := map[string]any{}
	Expect(json.Unmarshal(raw, &result)).To(Succeed())
	return result
}

// secretCopier creates a SecretCopier with rules.
func (s *scenario) secretCopier(name string, rules ...object) *unstructured.Unstructured {
	GinkgoHelper()
	copier := resource(kindSecretCopier, "", name, rulesSpec(rules...))
	s.create(copier)
	return copier
}

// secretExporter creates a SecretExporter, which exports the Secret of the
// same name in its namespace, with rules.
func (s *scenario) secretExporter(ns, name string, rules ...object) *unstructured.Unstructured {
	GinkgoHelper()
	exporter := resource(kindSecretExporter, ns, name, rulesSpec(rules...))
	s.create(exporter)
	return exporter
}

// secretImporter creates a SecretImporter with the fields of spec, which
// accepts the copy of its name in its namespace.
func (s *scenario) secretImporter(ns, name string, spec ...object) *unstructured.Unstructured {
	GinkgoHelper()
	importer := resource(kindSecretImporter, ns, name, merged(spec...))
	s.create(importer)
	return importer
}

// secretInjector creates a SecretInjector with rules.
func (s *scenario) secretInjector(name string, rules ...object) *unstructured.Unstructured {
	GinkgoHelper()
	injector := resource(kindSecretInjector, "", name, rulesSpec(rules...))
	s.create(injector)
	return injector
}

// setRules replaces the rules of a SecretCopier, SecretExporter or
// SecretInjector.
func setRules(u *unstructured.Unstructured, rules ...object) {
	GinkgoHelper()
	Expect(unstructured.SetNestedField(u.Object, jsonObject(rulesSpec(rules...))["rules"], "spec", "rules")).
		To(Succeed())
}

// holdSource puts a finalizer on src, so that it outlives the deletion of
// its namespace, and removes it when the scenario ends.
func (s *scenario) holdSource(src *corev1.Secret) {
	GinkgoHelper()
	s.update(src, func() { src.Finalizers = append(src.Finalizers, holdFinalizer) })
	DeferCleanup(func() {
		latest := &corev1.Secret{}
		err := target.Client.Get(s.ctx, client.ObjectKeyFromObject(src), latest)
		if apierrors.IsNotFound(err) {
			return
		}
		Expect(err).NotTo(HaveOccurred())
		s.update(latest, func() {
			latest.Finalizers = slices.DeleteFunc(latest.Finalizers, func(f string) bool { return f == holdFinalizer })
		})
	})
}

// terminate deletes namespace ns and waits until it is Terminating.
func (s *scenario) terminate(ns *corev1.Namespace) {
	GinkgoHelper()
	s.deleteObject(ns)
	eventually(func(g Gomega) {
		latest := &corev1.Namespace{}
		g.Expect(target.Client.Get(context.Background(), client.ObjectKeyFromObject(ns), latest)).To(Succeed())
		g.Expect(latest.Status.Phase).To(Equal(corev1.NamespaceTerminating))
	})
}

// requireGarbageCollector skips the scenario on a target without Kubernetes
// garbage collection.
func requireGarbageCollector() {
	if !target.GarbageCollector {
		Skip(target.Name + " has no garbage collector")
	}
}
