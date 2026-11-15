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

package runtimeadmission

import (
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/educates/educates-training-platform/installer/operator/internal/platformnamespace"
)

const (
	// scopePolicy confines each training portal to its own workshop
	// environments, sessions and allocations.
	scopePolicy = "educates-training-portal-scope"

	portalLabel = "training.educates.dev/portal.name"

	// portal is the training portal whose service account the specs act
	// as; otherPortal owns the resources it must not touch.
	portal      = "alpha"
	otherPortal = "beta"
)

var trainingGroupVersion = schema.GroupVersion{Group: "training.educates.dev", Version: "v1beta1"}

var _ = Describe("The training portal scope admission policy", Ordered, func() {
	var asPortal, asSessionManager client.Client

	BeforeAll(func() {
		bindServiceAccount(portal+"-ui", "training-portal", trainingPortalRole)
		asPortal = impersonating(serviceAccountUser(portal+"-ui", "training-portal"))
		asSessionManager = impersonating(serviceAccountUser(platformnamespace.Name, "session-manager"))

		By("waiting until the API server enforces the policy")
		Eventually(func(g Gomega) {
			err := asPortal.Create(ctx, environment(portal+"-w99", otherPortal), client.DryRunAll)
			g.Expect(err).To(BeRefusedByScopePolicy())
		}, 30*time.Second, 250*time.Millisecond).Should(Succeed())
	})

	AfterEach(func() {
		deleteAllTrainingResources()
	})

	It("fails closed, so the API server refuses a request it cannot evaluate", func() {
		policy := &admissionregistrationv1.ValidatingAdmissionPolicy{}
		Expect(k8sClient.Get(ctx, client.ObjectKey{Name: scopePolicy}, policy)).To(Succeed())
		Expect(policy.Spec.FailurePolicy).To(HaveValue(Equal(admissionregistrationv1.Fail)))
	})

	DescribeTable("admits a portal creating, changing and deleting its own resources",
		func(obj *unstructured.Unstructured) {
			Expect(asPortal.Create(ctx, obj)).To(Succeed())

			By("adding and removing a finalizer, which keeps the portal label")
			Expect(asPortal.Patch(ctx, obj, finalizers("example.com/hold")())).To(Succeed())
			Expect(asPortal.Patch(ctx, obj, finalizers()())).To(Succeed())

			Expect(asPortal.Delete(ctx, obj)).To(Succeed())
		},
		Entry("a workshop environment", environment(portal+"-w01", portal)),
		Entry("a workshop environment without spec.environment",
			withoutEnvironmentContent(environment(portal+"-w01", portal))),
		Entry("a workshop environment numbered past 99", environment(portal+"-w100", portal)),
		Entry("a workshop session", session(portal+"-w01-s001", portal, portal+"-w01")),
		Entry("a workshop allocation",
			allocation(portal+"-w01-s001", portal, portal+"-w01", portal+"-w01-s001")),
	)

	DescribeTable("refuses a portal what it admits session-manager",
		func(existing []*unstructured.Unstructured, attempt func(client.Client) error) {
			for _, obj := range existing {
				Expect(k8sClient.Create(ctx, obj.DeepCopy())).To(Succeed())
			}

			Expect(attempt(asPortal)).To(BeRefusedByScopePolicy())
			Expect(attempt(asSessionManager)).To(Succeed())
		},
		Entry("creating an environment labelled with another portal",
			nil, creating(environment(portal+"-w01", otherPortal))),
		Entry("creating an unlabelled environment",
			nil, creating(environment(portal+"-w01", ""))),
		Entry("creating a session labelled with another portal",
			nil, creating(session(portal+"-w01-s001", otherPortal, portal+"-w01"))),
		Entry("creating an allocation labelled with another portal",
			nil, creating(allocation(portal+"-w01-s001", otherPortal, portal+"-w01", portal+"-w01-s001"))),

		Entry("relabelling another portal's environment as its own",
			[]*unstructured.Unstructured{environment(portal+"-w01", otherPortal)},
			patching(environment(portal+"-w01", ""), labelledAsPortals)),
		Entry("relabelling an unlabelled environment as its own",
			[]*unstructured.Unstructured{environment(portal+"-w01", "")},
			patching(environment(portal+"-w01", ""), labelledAsPortals)),
		Entry("relabelling another portal's session as its own",
			[]*unstructured.Unstructured{session(portal+"-w01-s001", otherPortal, portal+"-w01")},
			patching(session(portal+"-w01-s001", "", portal+"-w01"), labelledAsPortals)),
		Entry("relabelling another portal's allocation as its own",
			[]*unstructured.Unstructured{
				allocation(portal+"-w01-s001", otherPortal, portal+"-w01", portal+"-w01-s001"),
			},
			patching(allocation(portal+"-w01-s001", "", portal+"-w01", portal+"-w01-s001"), labelledAsPortals)),

		Entry("changing another portal's environment",
			[]*unstructured.Unstructured{environment(otherPortal+"-w01", otherPortal)},
			patching(environment(otherPortal+"-w01", ""), finalizers("example.com/hold"))),
		Entry("changing another portal's session",
			[]*unstructured.Unstructured{session(otherPortal+"-w01-s001", otherPortal, otherPortal+"-w01")},
			patching(session(otherPortal+"-w01-s001", "", otherPortal+"-w01"), finalizers("example.com/hold"))),
		Entry("changing another portal's allocation",
			[]*unstructured.Unstructured{
				allocation(otherPortal+"-w01-s001", otherPortal, otherPortal+"-w01", otherPortal+"-w01-s001"),
			},
			patching(allocation(otherPortal+"-w01-s001", "", otherPortal+"-w01", otherPortal+"-w01-s001"),
				finalizers("example.com/hold"))),

		Entry("deleting another portal's environment",
			[]*unstructured.Unstructured{environment(otherPortal+"-w01", otherPortal)},
			deleting(environment(otherPortal+"-w01", ""))),
		Entry("deleting another portal's session",
			[]*unstructured.Unstructured{session(otherPortal+"-w01-s001", otherPortal, otherPortal+"-w01")},
			deleting(session(otherPortal+"-w01-s001", "", otherPortal+"-w01"))),
		Entry("deleting another portal's allocation",
			[]*unstructured.Unstructured{
				allocation(otherPortal+"-w01-s001", otherPortal, otherPortal+"-w01", otherPortal+"-w01-s001"),
			},
			deleting(allocation(otherPortal+"-w01-s001", "", otherPortal+"-w01", otherPortal+"-w01-s001"))),

		Entry("creating an environment named after another portal",
			nil, creating(environment(otherPortal+"-w01", portal))),
		Entry("creating an environment named without the -w infix",
			nil, creating(environment(portal+"-01", portal))),
		Entry("creating an environment named without a number",
			nil, creating(environment(portal+"-w", portal))),
		Entry("creating an environment named with more than a number",
			nil, creating(environment(portal+"-w01x", portal))),
		Entry("creating an environment named under another environment",
			nil, creating(environment(portal+"-w01-w02", portal))),
		Entry("creating an environment named under a portal whose name extends its own",
			nil, creating(environment(portal+"x-w01", portal))),

		Entry("creating a session in another portal's environment",
			nil, creating(session(portal+"-w01-s001", portal, otherPortal+"-w01"))),
		Entry("creating a session in an environment not named as the portal's",
			nil, creating(session(portal+"-w01-s001", portal, portal+"-01"))),
		Entry("moving its own session to another portal's environment",
			[]*unstructured.Unstructured{session(portal+"-w01-s001", portal, portal+"-w01")},
			patching(session(portal+"-w01-s001", "", portal+"-w01"), environmentNamed(otherPortal+"-w01"))),
		Entry("creating a session named after another of its environments",
			nil, creating(session(portal+"-w02-s001", portal, portal+"-w01"))),
		Entry("creating a session named after another portal's environment",
			nil, creating(session(otherPortal+"-w01-s001", portal, portal+"-w01"))),
		Entry("creating a session named without the hyphen after its environment",
			nil, creating(session(portal+"-w01s001", portal, portal+"-w01"))),
		Entry("creating a session named after an environment whose name extends its own",
			nil, creating(session(portal+"-w10-s001", portal, portal+"-w1"))),
		Entry("moving its own session to another of its environments",
			[]*unstructured.Unstructured{session(portal+"-w01-s001", portal, portal+"-w01")},
			patching(session(portal+"-w01-s001", "", portal+"-w01"), environmentNamed(portal+"-w02"))),
		Entry("creating an allocation in another portal's environment",
			nil, creating(allocation(portal+"-w01-s001", portal, otherPortal+"-w01", otherPortal+"-w01-s001"))),
		Entry("creating an allocation in an environment not named as the portal's",
			nil, creating(allocation(portal+"-w01-s001", portal, portal+"-01", portal+"-01-s001"))),

		Entry("creating an allocation of a session of another of its environments",
			nil, creating(allocation(portal+"-w01-s001", portal, portal+"-w01", portal+"-w02-s001"))),
		Entry("creating an allocation of a session of another portal's environment",
			nil, creating(allocation(portal+"-w01-s001", portal, portal+"-w01", otherPortal+"-w01-s001"))),
		Entry("creating an allocation of a session named without the hyphen after its environment",
			nil, creating(allocation(portal+"-w01-s001", portal, portal+"-w01", portal+"-w01s001"))),
		Entry("creating an allocation of a session of an environment whose name extends its own",
			nil, creating(allocation(portal+"-w1-s001", portal, portal+"-w1", portal+"-w10-s001"))),

		Entry("creating an environment with objects",
			nil, creating(withEnvironmentContent(environment(portal+"-w01", portal), "objects", injectedObject))),
		Entry("creating an environment with Secrets",
			nil, creating(withEnvironmentContent(environment(portal+"-w01", portal), "secrets", copiedSecret))),
		Entry("adding objects to its own environment",
			[]*unstructured.Unstructured{environment(portal+"-w01", portal)},
			patching(environment(portal+"-w01", ""), environmentContent("objects", injectedObject))),
		Entry("adding Secrets to its own environment",
			[]*unstructured.Unstructured{environment(portal+"-w01", portal)},
			patching(environment(portal+"-w01", ""), environmentContent("secrets", copiedSecret))),
	)
})

// BeRefusedByScopePolicy succeeds when an error is the scope policy
// refusing the request, rather than RBAC or anything else.
func BeRefusedByScopePolicy() OmegaMatcher {
	return MatchError(ContainSubstring("ValidatingAdmissionPolicy '%s'", scopePolicy))
}

// trainingResource returns a training resource of the kind, named name,
// labelled with owner as its portal unless owner is empty.
func trainingResource(kind, name, owner string, spec map[string]any) *unstructured.Unstructured {
	obj := &unstructured.Unstructured{Object: map[string]any{"spec": spec}}
	obj.SetGroupVersionKind(trainingGroupVersion.WithKind(kind))
	obj.SetName(name)
	if owner != "" {
		obj.SetLabels(map[string]string{portalLabel: owner})
	}
	return obj
}

// environment returns a WorkshopEnvironment as a training portal creates
// one, with no objects or Secrets for session-manager to create.
func environment(name, owner string) *unstructured.Unstructured {
	return trainingResource("WorkshopEnvironment", name, owner, map[string]any{
		"workshop":    map[string]any{"name": "lab"},
		"request":     map[string]any{"enabled": false},
		"environment": map[string]any{"objects": []any{}, "secrets": []any{}},
	})
}

// injectedObject is an object a workshop environment could ask
// session-manager to create with its own rights.
var injectedObject = map[string]any{
	"apiVersion": "rbac.authorization.k8s.io/v1",
	"kind":       "ClusterRoleBinding",
	"metadata":   map[string]any{"name": "injected"},
	"roleRef": map[string]any{
		"apiGroup": "rbac.authorization.k8s.io", "kind": "ClusterRole", "name": "cluster-admin",
	},
	"subjects": []any{map[string]any{
		"kind": "ServiceAccount", "namespace": portal + "-ui", "name": "training-portal",
	}},
}

// copiedSecret is a Secret a workshop environment could ask session-manager
// to copy into it from another namespace.
var copiedSecret = map[string]any{"namespace": "kube-system", "name": "bootstrap-token"}

// withEnvironmentContent returns the workshop environment with the item as
// the only entry of its spec.environment list field, objects or secrets.
func withEnvironmentContent(obj *unstructured.Unstructured, field string, item map[string]any) *unstructured.Unstructured {
	content := obj.Object["spec"].(map[string]any)["environment"].(map[string]any)
	content[field] = []any{item}
	return obj
}

// withoutEnvironmentContent returns the workshop environment with no
// spec.environment at all.
func withoutEnvironmentContent(obj *unstructured.Unstructured) *unstructured.Unstructured {
	delete(obj.Object["spec"].(map[string]any), "environment")
	return obj
}

// session returns a WorkshopSession as a training portal creates one, in
// the named workshop environment.
func session(name, owner, environmentName string) *unstructured.Unstructured {
	return trainingResource("WorkshopSession", name, owner, map[string]any{
		"workshop":    map[string]any{"name": "lab"},
		"environment": map[string]any{"name": environmentName},
		"session":     map[string]any{"id": "s001"},
	})
}

// allocation returns a WorkshopAllocation as a training portal creates one,
// allocating the named session of the named workshop environment.
func allocation(name, owner, environmentName, sessionName string) *unstructured.Unstructured {
	return trainingResource("WorkshopAllocation", name, owner, map[string]any{
		"environment": map[string]any{"name": environmentName},
		"session":     map[string]any{"name": sessionName, "user": "attendee"},
	})
}

// creating returns an attempt to create obj.
func creating(obj *unstructured.Unstructured) func(client.Client) error {
	return func(c client.Client) error {
		return c.Create(ctx, obj.DeepCopy())
	}
}

// patching returns an attempt to patch the existing object obj names.
// The patch is built when the attempt runs.
func patching(obj *unstructured.Unstructured, patch func() client.Patch) func(client.Client) error {
	return func(c client.Client) error {
		return c.Patch(ctx, obj.DeepCopy(), patch())
	}
}

// deleting returns an attempt to delete the existing object obj names.
func deleting(obj *unstructured.Unstructured) func(client.Client) error {
	return func(c client.Client) error {
		return c.Delete(ctx, obj.DeepCopy())
	}
}

// labelledAsPortals returns a merge patch labelling an object with the
// portal the specs act as.
func labelledAsPortals() client.Patch {
	return mergePatch(map[string]any{"metadata": map[string]any{"labels": map[string]any{portalLabel: portal}}})
}

// environmentNamed returns a merge patch pointing a session or allocation
// at the named workshop environment.
func environmentNamed(name string) func() client.Patch {
	return func() client.Patch {
		return mergePatch(map[string]any{"spec": map[string]any{"environment": map[string]any{"name": name}}})
	}
}

// environmentContent returns a merge patch setting a workshop environment's
// spec.environment list field, objects or secrets, to the one item.
func environmentContent(field string, item map[string]any) func() client.Patch {
	return func() client.Patch {
		return mergePatch(map[string]any{"spec": map[string]any{"environment": map[string]any{field: []any{item}}}})
	}
}

// finalizers returns a merge patch setting an object's finalizers.
func finalizers(names ...string) func() client.Patch {
	return func() client.Patch {
		list := make([]any, 0, len(names))
		for _, name := range names {
			list = append(list, name)
		}
		return mergePatch(map[string]any{"metadata": map[string]any{"finalizers": list}})
	}
}

// mergePatch returns a JSON merge patch of the fields given.
func mergePatch(fields map[string]any) client.Patch {
	data, err := (&unstructured.Unstructured{Object: fields}).MarshalJSON()
	Expect(err).NotTo(HaveOccurred())
	return client.RawPatch(types.MergePatchType, data)
}

// deleteAllTrainingResources removes, as the suite's administrator, every
// workshop environment, session and allocation a spec left behind.
func deleteAllTrainingResources() {
	GinkgoHelper()

	for _, kind := range []string{"WorkshopEnvironment", "WorkshopSession", "WorkshopAllocation"} {
		list := &unstructured.UnstructuredList{}
		list.SetGroupVersionKind(trainingGroupVersion.WithKind(kind + "List"))
		Expect(k8sClient.List(ctx, list)).To(Succeed())
		for i := range list.Items {
			obj := &list.Items[i]
			if len(obj.GetFinalizers()) > 0 {
				Expect(k8sClient.Patch(ctx, obj, finalizers()())).To(Succeed())
			}
			Expect(client.IgnoreNotFound(k8sClient.Delete(ctx, obj))).To(Succeed())
		}
		Eventually(func(g Gomega) {
			g.Expect(k8sClient.List(ctx, list)).To(Succeed())
			g.Expect(list.Items).To(BeEmpty())
		}, 10*time.Second, 100*time.Millisecond).Should(Succeed())
	}
}
