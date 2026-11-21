package scenarios

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	eventsv1 "k8s.io/api/events/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	// annotationCopierRule and annotationSecretName mark a copy with the
	// resource whose rule wrote it and the Secret it copies.
	annotationCopierRule = "secrets.educates.dev/copier-rule"
	annotationSecretName = "secrets.educates.dev/secret-name"

	// annotationSourceVersion marks a copy with the resourceVersion of the
	// Secret it was last written from.
	annotationSourceVersion = "secrets.educates.dev/source-version"

	// eventsNamespace holds the Events of the cluster-scoped kinds.
	eventsNamespace = "default"
)

// eventually waits until assertion passes, within the target's convergence
// time.
func eventually(assertion func(g Gomega)) {
	GinkgoHelper()
	Eventually(assertion).WithTimeout(target.Timing.Converge).WithPolling(target.Timing.Poll).Should(Succeed())
}

// consistently asserts that assertion keeps passing for the target's hold
// time.
func consistently(assertion func(g Gomega)) {
	GinkgoHelper()
	consistentlyFor(target.Timing.Hold, assertion)
}

// consistentlyFor asserts that assertion keeps passing for duration.
func consistentlyFor(duration time.Duration, assertion func(g Gomega)) {
	GinkgoHelper()
	Consistently(assertion).WithTimeout(duration).WithPolling(target.Timing.Poll).Should(Succeed())
}

// get reads the latest version of a secrets.educates.dev resource.
func get(g Gomega, u *unstructured.Unstructured) *unstructured.Unstructured {
	latest := &unstructured.Unstructured{}
	latest.SetGroupVersionKind(u.GroupVersionKind())
	g.Expect(target.Client.Get(context.Background(), client.ObjectKeyFromObject(u), latest)).To(Succeed())
	return latest
}

// isGone reports whether u no longer exists.
func isGone(u *unstructured.Unstructured) bool {
	latest := &unstructured.Unstructured{}
	latest.SetGroupVersionKind(u.GroupVersionKind())
	err := target.Client.Get(context.Background(), client.ObjectKeyFromObject(u), latest)
	return apierrors.IsNotFound(err)
}

// ruleOf names a resource as the copier-rule annotation does.
func ruleOf(u *unstructured.Unstructured) string {
	return strings.ToLower(u.GetKind()) + "/" + u.GetName()
}

// uidOf is the UID of a resource, the default shared secret of an exporter.
func uidOf(u *unstructured.Unstructured) string {
	return string(u.GetUID())
}

// secretIn reads the Secret name in namespace ns, which must exist.
func secretIn(g Gomega, ns, name string) *corev1.Secret {
	secret := &corev1.Secret{}
	g.Expect(target.Client.Get(context.Background(), types.NamespacedName{Namespace: ns, Name: name}, secret)).
		To(Succeed(), "Secret %s/%s", ns, name)
	return secret
}

// expectNoSecret asserts that namespace ns holds no Secret name.
func expectNoSecret(g Gomega, ns, name string) {
	err := target.Client.Get(context.Background(), types.NamespacedName{Namespace: ns, Name: name}, &corev1.Secret{})
	g.Expect(apierrors.IsNotFound(err)).To(BeTrue(), "Secret %s/%s should not exist, got %v", ns, name, err)
}

// expectCopy asserts that namespace ns holds a copy of src named name, with
// its type and data, marked with the resource whose rule wrote it, and
// returns it.
func expectCopy(g Gomega, ns, name string, src *corev1.Secret, writer string) *corev1.Secret {
	copied := secretIn(g, ns, name)
	g.Expect(copied.Type).To(Equal(src.Type), "type of %s/%s", ns, name)
	g.Expect(copied.Data).To(Equal(src.Data), "data of %s/%s", ns, name)
	g.Expect(copied.Annotations).To(HaveKeyWithValue(annotationCopierRule, writer), "copier rule of %s/%s", ns, name)
	g.Expect(copied.Annotations).To(HaveKeyWithValue(annotationSecretName, src.Namespace+"/"+src.Name),
		"source of %s/%s", ns, name)
	return copied
}

// settledSecret waits until the Secret name in namespace ns exists and
// keeps its resource version between two reads, and returns it.
func settledSecret(ns, name string) *corev1.Secret {
	GinkgoHelper()
	var last *corev1.Secret
	Eventually(func(g Gomega) {
		current := secretIn(g, ns, name)
		stable := last != nil && current.ResourceVersion == last.ResourceVersion
		last = current
		g.Expect(stable).To(BeTrue(), "Secret %s/%s still changing", ns, name)
	}).WithTimeout(target.Timing.Converge).WithPolling(3 * target.Timing.Poll).Should(Succeed())
	return last
}

// expectCopyUnwritten waits until the copy of src named name in namespace
// ns settles, asserts that it stays that copy, marked with writer, and is
// not written again for the target's hold time, and returns it.
func expectCopyUnwritten(ns, name string, src *corev1.Secret, writer string) *corev1.Secret {
	GinkgoHelper()
	settled := settledSecret(ns, name)
	consistently(func(g Gomega) {
		copied := expectCopy(g, ns, name, src, writer)
		g.Expect(copied.ResourceVersion).To(Equal(settled.ResourceVersion), "resource version of %s/%s", ns, name)
	})
	return settled
}

// expectOwnedBy asserts that a copy carries an owner reference to owner,
// which blocks the owner's deletion until the copy is gone.
func expectOwnedBy(g Gomega, copied *corev1.Secret, owner *unstructured.Unstructured) {
	var ref *metav1.OwnerReference
	for i := range copied.OwnerReferences {
		if copied.OwnerReferences[i].UID == owner.GetUID() {
			ref = &copied.OwnerReferences[i]
		}
	}
	g.Expect(ref).NotTo(BeNil(), "owner reference of %s/%s to %s", copied.Namespace, copied.Name, ruleOf(owner))
	g.Expect(ref.APIVersion).To(Equal(apiVersion))
	g.Expect(ref.Kind).To(Equal(owner.GetKind()))
	g.Expect(ref.Name).To(Equal(owner.GetName()))
	g.Expect(ref.BlockOwnerDeletion).To(HaveValue(BeTrue()))
}

// serviceAccountIn reads the service account name in namespace ns.
func serviceAccountIn(g Gomega, ns, name string) *corev1.ServiceAccount {
	account := &corev1.ServiceAccount{}
	g.Expect(target.Client.Get(context.Background(), types.NamespacedName{Namespace: ns, Name: name}, account)).
		To(Succeed(), "ServiceAccount %s/%s", ns, name)
	return account
}

// pullSecretNames lists the names in a service account's imagePullSecrets.
func pullSecretNames(account *corev1.ServiceAccount) []string {
	result := make([]string, 0, len(account.ImagePullSecrets))
	for _, ref := range account.ImagePullSecrets {
		result = append(result, ref.Name)
	}
	return result
}

// secretNames lists the names in a service account's secrets.
func secretNames(account *corev1.ServiceAccount) []string {
	result := make([]string, 0, len(account.Secrets))
	for _, ref := range account.Secrets {
		result = append(result, ref.Name)
	}
	return result
}

// expectReady asserts that u reports a Ready condition with status and
// reason, for its current generation, and returns the condition.
func expectReady(g Gomega, u *unstructured.Unstructured, status, reason string) map[string]any {
	latest := get(g, u)
	var ready map[string]any
	conditions, _, _ := unstructured.NestedSlice(latest.Object, "status", "conditions")
	for _, item := range conditions {
		if c, ok := item.(map[string]any); ok && c["type"] == "Ready" {
			ready = c
		}
	}
	g.Expect(ready).NotTo(BeNil(), "Ready condition of %s", ruleOf(u))
	g.Expect(ready).To(HaveKeyWithValue("status", status), "Ready status of %s", ruleOf(u))
	g.Expect(ready).To(HaveKeyWithValue("reason", reason), "Ready reason of %s", ruleOf(u))
	observed, _, _ := unstructured.NestedInt64(latest.Object, "status", "observedGeneration")
	g.Expect(observed).To(Equal(latest.GetGeneration()), "observed generation of %s", ruleOf(u))
	return ready
}

// statusValue reads the status field at path of the latest version of u.
func statusValue(g Gomega, u *unstructured.Unstructured, path ...string) any {
	latest := get(g, u)
	value, found, err := unstructured.NestedFieldNoCopy(latest.Object, append([]string{"status"}, path...)...)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(found).To(BeTrue(), "status.%s of %s", strings.Join(path, "."), ruleOf(u))
	return value
}

// summaryOf reads the status summary of the latest version of u.
func summaryOf(g Gomega, u *unstructured.Unstructured) map[string]any {
	summary, ok := statusValue(g, u, "summary").(map[string]any)
	g.Expect(ok).To(BeTrue(), "status.summary of %s is an object", ruleOf(u))
	return summary
}

// statusRule reads the status entry of rule index of the latest version of
// u.
func statusRule(g Gomega, u *unstructured.Unstructured, index int) map[string]any {
	rules, ok := statusValue(g, u, "rules").([]any)
	g.Expect(ok).To(BeTrue(), "status.rules of %s is a list", ruleOf(u))
	g.Expect(len(rules)).To(BeNumerically(">", index), "status.rules of %s", ruleOf(u))
	rule, ok := rules[index].(map[string]any)
	g.Expect(ok).To(BeTrue(), "status.rules[%d] of %s is an object", index, ruleOf(u))
	return rule
}

// expectCounts asserts the counts of a status summary or rule entry.
func expectCounts(g Gomega, fields map[string]any, want map[string]int64) {
	for name, value := range want {
		g.Expect(fields).To(HaveKey(name), "status count %s", name)
		g.Expect(fields[name]).To(BeNumerically("==", value), "status count %s", name)
	}
}

// jsonText renders v as JSON.
func jsonText(g Gomega, v any) string {
	raw, err := json.Marshal(v)
	g.Expect(err).NotTo(HaveOccurred())
	return string(raw)
}

// eventsAbout lists the events.k8s.io Events in namespace ns regarding u
// with reason.
func eventsAbout(g Gomega, ns string, u *unstructured.Unstructured, reason string) []eventsv1.Event {
	list := &eventsv1.EventList{}
	g.Expect(target.Client.List(context.Background(), list, client.InNamespace(ns))).To(Succeed())
	var found []eventsv1.Event
	for _, event := range list.Items {
		if event.Regarding.UID == u.GetUID() && event.Reason == reason {
			found = append(found, event)
		}
	}
	return found
}

// tableColumns reads u as kubectl get prints it, mapping the name of each
// column to its cell.
func tableColumns(g Gomega, u *unstructured.Unstructured, plural string) map[string]string {
	path := fmt.Sprintf("/apis/%s/%s", group, version)
	if u.GetNamespace() != "" {
		path += "/namespaces/" + u.GetNamespace()
	}
	path += "/" + plural + "/" + u.GetName()

	httpClient, err := rest.HTTPClientFor(target.Config)
	g.Expect(err).NotTo(HaveOccurred())
	request, err := http.NewRequestWithContext(context.Background(), http.MethodGet,
		strings.TrimSuffix(target.Config.Host, "/")+path, nil)
	g.Expect(err).NotTo(HaveOccurred())
	request.Header.Set("Accept", "application/json;as=Table;v=v1;g=meta.k8s.io")
	response, err := httpClient.Do(request)
	g.Expect(err).NotTo(HaveOccurred())
	defer func() { _ = response.Body.Close() }()
	body, err := io.ReadAll(response.Body)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(response.StatusCode).To(Equal(http.StatusOK), "GET %s: %s", path, body)

	table := &metav1.Table{}
	g.Expect(json.Unmarshal(body, table)).To(Succeed())
	g.Expect(table.Rows).To(HaveLen(1))
	columns := map[string]string{}
	for i, column := range table.ColumnDefinitions {
		if i < len(table.Rows[0].Cells) {
			columns[column.Name] = fmt.Sprint(table.Rows[0].Cells[i])
		}
	}
	return columns
}
