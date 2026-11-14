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

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ = Describe("A training portal's credentials", Ordered, func() {
	var asAdministrator client.Client

	BeforeAll(func() {
		asAdministrator = k8sClient
	})

	AfterEach(func() {
		deleteAllTrainingPortals()
	})

	DescribeTable("admits a portal setting each account's credentials one way",
		func(definition *unstructured.Unstructured) {
			Expect(asAdministrator.Create(ctx, definition.DeepCopy())).To(Succeed())
		},
		Entry("with generated credentials", trainingPortal("generated")),
		Entry("with credentials Secret references", trainingPortal("referenced", adminReference, robotReference)),
		Entry("with every credential inline", trainingPortal("inline", adminCredentials, robotCredentials, robotClient)),
		Entry("with the admin credentials inline and the robot credentials by reference",
			trainingPortal("mixed", adminCredentials, robotReference)),
		Entry("with the robot credentials inline and the admin credentials by reference",
			trainingPortal("mixed", robotCredentials, robotClient, adminReference)),
	)

	DescribeTable("refuses a portal setting the same credentials inline and by reference",
		func(definition *unstructured.Unstructured, field string) {
			err := asAdministrator.Create(ctx, definition.DeepCopy())
			Expect(err).To(MatchError(And(
				ContainSubstring(field),
				ContainSubstring("cannot be set both inline and by spec.portal.credentialSecrets"))))
		},
		Entry("the admin credentials",
			trainingPortal("both", adminCredentials, adminReference),
			"spec.portal.credentialSecrets.admin"),
		Entry("the admin password alone",
			trainingPortal("both", adminPassword, adminReference),
			"spec.portal.credentialSecrets.admin"),
		Entry("the robot credentials",
			trainingPortal("both", robotCredentials, robotReference),
			"spec.portal.credentialSecrets.robot"),
		Entry("the robot client",
			trainingPortal("both", robotClient, robotReference),
			"spec.portal.credentialSecrets.robot"),
	)

	It("refuses adding a reference to a portal that sets the same credentials inline", func() {
		obj := trainingPortal("inline", robotClient)
		Expect(asAdministrator.Create(ctx, obj)).To(Succeed())

		err := asAdministrator.Patch(ctx, obj, mergePatch(map[string]any{"spec": map[string]any{
			"portal": map[string]any{"credentialSecrets": map[string]any{"robot": credentialsSecret("lab-robot")}},
		}}))
		Expect(err).To(MatchError(ContainSubstring("spec.portal.credentialSecrets.robot")))
	})

	It("requires a referenced Secret's name and namespace", func() {
		obj := trainingPortal("unnamed", func(portalSpec map[string]any) {
			portalSpec["credentialSecrets"] = map[string]any{"admin": map[string]any{"name": "lab-admin"}}
		})

		err := asAdministrator.Create(ctx, obj)
		Expect(err).To(MatchError(ContainSubstring("spec.portal.credentialSecrets.admin.namespace: Required value")))
	})
})

// trainingPortal returns a TrainingPortal with the given changes made to
// its spec.portal.
func trainingPortal(name string, changes ...func(map[string]any)) *unstructured.Unstructured {
	portalSpec := map[string]any{"title": "Workshops"}
	for _, change := range changes {
		change(portalSpec)
	}
	obj := &unstructured.Unstructured{Object: map[string]any{
		"spec": map[string]any{
			"portal":    portalSpec,
			"workshops": []any{map[string]any{"name": "lab"}},
		},
	}}
	obj.SetGroupVersionKind(trainingGroupVersion.WithKind("TrainingPortal"))
	obj.SetName(name)
	return obj
}

// nested returns the map under key in parent, adding it when missing.
func nested(parent map[string]any, key string) map[string]any {
	child, ok := parent[key].(map[string]any)
	if !ok {
		child = map[string]any{}
		parent[key] = child
	}
	return child
}

func adminCredentials(portalSpec map[string]any) {
	nested(portalSpec, "credentials")["admin"] = map[string]any{"username": "admin", "password": "admin-password"}
}

func adminPassword(portalSpec map[string]any) {
	nested(portalSpec, "credentials")["admin"] = map[string]any{"password": "admin-password"}
}

func robotCredentials(portalSpec map[string]any) {
	nested(portalSpec, "credentials")["robot"] = map[string]any{"username": "robot", "password": "robot-password"}
}

func robotClient(portalSpec map[string]any) {
	nested(portalSpec, "clients")["robot"] = map[string]any{"id": "client-id", "secret": "client-secret"}
}

func adminReference(portalSpec map[string]any) {
	nested(portalSpec, "credentialSecrets")["admin"] = credentialsSecret("lab-admin")
}

func robotReference(portalSpec map[string]any) {
	nested(portalSpec, "credentialSecrets")["robot"] = credentialsSecret("lab-robot")
}

// credentialsSecret returns a reference to the named Secret in the
// namespace where a platform operator keeps portal credentials.
func credentialsSecret(name string) map[string]any {
	return map[string]any{"name": name, "namespace": "portal-credentials"}
}

// deleteAllTrainingPortals removes, as the suite's administrator, every
// TrainingPortal a spec left behind.
func deleteAllTrainingPortals() {
	GinkgoHelper()

	list := &unstructured.UnstructuredList{}
	list.SetGroupVersionKind(trainingGroupVersion.WithKind("TrainingPortalList"))
	Expect(k8sClient.List(ctx, list)).To(Succeed())
	for i := range list.Items {
		Expect(client.IgnoreNotFound(k8sClient.Delete(ctx, &list.Items[i]))).To(Succeed())
	}
	Eventually(func(g Gomega) {
		g.Expect(k8sClient.List(ctx, list)).To(Succeed())
		g.Expect(list.Items).To(BeEmpty())
	}, 10*time.Second, 100*time.Millisecond).Should(Succeed())
}
