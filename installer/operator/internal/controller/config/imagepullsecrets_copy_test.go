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

package config

import (
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	configv1alpha1 "github.com/educates/educates-training-platform/installer/operator/api/config/v1alpha1"
	"github.com/educates/educates-training-platform/installer/operator/internal/ownership"
	"github.com/educates/educates-training-platform/installer/operator/internal/platformnamespace"
)

// makePullSecret returns a kubernetes.io/dockerconfigjson Secret in
// namespace holding credentials for mirror.example.com with the given
// password, so a test can tell one version of it from another.
func makePullSecret(namespace, name, password string) *corev1.Secret {
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
		Type:       corev1.SecretTypeDockerConfigJson,
		Data: map[string][]byte{
			corev1.DockerConfigJsonKey: fmt.Appendf(nil,
				`{"auths":{"mirror.example.com":{"username":"educates","password":%q}}}`, password),
		},
	}
}

// pullSecretRefs returns LocalObjectReferences to the named Secrets.
func pullSecretRefs(names ...string) []configv1alpha1.LocalObjectReference {
	refs := make([]configv1alpha1.LocalObjectReference, 0, len(names))
	for _, name := range names {
		refs = append(refs, configv1alpha1.LocalObjectReference{Name: name})
	}
	return refs
}

// inlineSpecWithPullSecrets is validInlineSpec naming the given image
// pull Secrets.
func inlineSpecWithPullSecrets(names ...string) configv1alpha1.EducatesClusterConfigSpec {
	spec := validInlineSpec()
	spec.Inline.ImageRegistry = &configv1alpha1.ImageRegistry{PullSecrets: pullSecretRefs(names...)}
	return spec
}

// getSecret returns the named Secret, or nil while it does not exist.
func getSecret(namespace, name string) *corev1.Secret {
	s := &corev1.Secret{}
	if err := k8sClient.Get(ctx, types.NamespacedName{Namespace: namespace, Name: name}, s); err != nil {
		if apierrors.IsNotFound(err) {
			return nil
		}
		Expect(err).NotTo(HaveOccurred())
	}
	return s
}

// expectPullSecretCopy asserts that namespace holds a copy of source,
// its type and data, labelled as the operator's and controlled by the
// EducatesClusterConfig, so it is garbage collected with it.
func expectPullSecretCopy(namespace string, source *corev1.Secret) {
	GinkgoHelper()
	copied := getSecret(namespace, source.Name)
	Expect(copied).NotTo(BeNil(), "no copy of %s in namespace %s", source.Name, namespace)
	Expect(copied.Type).To(Equal(source.Type))
	Expect(copied.Data).To(Equal(source.Data))
	Expect(copied.Labels).To(HaveKeyWithValue(ownership.ManagedByLabel, ownership.ManagedByValue))

	owner := &configv1alpha1.EducatesClusterConfig{}
	Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "cluster"}, owner)).To(Succeed())
	Expect(metav1.IsControlledBy(copied, owner)).To(BeTrue(), "copy of %s in %s is not controlled by the EducatesClusterConfig", source.Name, namespace)
}

var _ = Describe("EducatesClusterConfig image pull Secret copies", func() {
	BeforeEach(func() {
		ensureNamespace(testOperatorNamespace)
		Expect(k8sClient.Create(ctx, makeIngressClass())).To(Succeed())
		Expect(k8sClient.Create(ctx, makeWildcardSecret(true))).To(Succeed())
	})

	AfterEach(func() {
		drainCR()
		// envtest runs no garbage collector, so the copies the
		// EducatesClusterConfig owned outlive it here.
		_ = k8sClient.DeleteAllOf(ctx, &corev1.Secret{}, client.InNamespace(testOperatorNamespace))
		_ = k8sClient.DeleteAllOf(ctx, &corev1.Secret{}, client.InNamespace(platformnamespace.Name))
		_ = k8sClient.DeleteAllOf(ctx, &networkingv1.IngressClass{})
	})

	It("copies each pull Secret into the educates namespace in Inline mode, creating it first", func() {
		source := makePullSecret(testOperatorNamespace, "mirror-pull-secret", "first")
		Expect(k8sClient.Create(ctx, source)).To(Succeed())
		Expect(k8sClient.Create(ctx, &configv1alpha1.EducatesClusterConfig{
			ObjectMeta: metav1.ObjectMeta{Name: "cluster"},
			Spec:       inlineSpecWithPullSecrets("mirror-pull-secret"),
		})).To(Succeed())

		reconcileTwice(makeReconciler())

		Expect(readyConditionStatus()).To(Equal(metav1.ConditionTrue))
		expectPullSecretCopy(platformnamespace.Name, source)

		// The namespace is created as the platform reconcilers create it:
		// privileged and owned by none of the custom resources.
		ns := &corev1.Namespace{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: platformnamespace.Name}, ns)).To(Succeed())
		Expect(ns.Labels).To(HaveKeyWithValue("pod-security.kubernetes.io/enforce", "privileged"))
		Expect(ns.OwnerReferences).To(BeEmpty())

		// The user's cluster services keep their own pull secrets.
		Expect(getSecret(certManagerNamespace, "mirror-pull-secret")).To(BeNil())
		Expect(getSecret(contourNamespace, "mirror-pull-secret")).To(BeNil())
	})

	It("keeps each copy following its source when the credentials rotate", func() {
		Expect(k8sClient.Create(ctx, makePullSecret(testOperatorNamespace, "mirror-pull-secret", "first"))).To(Succeed())
		Expect(k8sClient.Create(ctx, &configv1alpha1.EducatesClusterConfig{
			ObjectMeta: metav1.ObjectMeta{Name: "cluster"},
			Spec:       inlineSpecWithPullSecrets("mirror-pull-secret"),
		})).To(Succeed())
		r := makeReconciler()
		reconcileTwice(r)

		rotated := makePullSecret(testOperatorNamespace, "mirror-pull-secret", "rotated")
		source := getSecret(testOperatorNamespace, "mirror-pull-secret")
		source.Data = rotated.Data
		Expect(k8sClient.Update(ctx, source)).To(Succeed())
		reconcileTwice(r)

		expectPullSecretCopy(platformnamespace.Name, rotated)
	})

	It("deletes the copy of a pull Secret the spec no longer names, and keeps the others", func() {
		kept := makePullSecret(testOperatorNamespace, "mirror-pull-secret", "kept")
		Expect(k8sClient.Create(ctx, kept)).To(Succeed())
		Expect(k8sClient.Create(ctx, makePullSecret(testOperatorNamespace, "retired-pull-secret", "retired"))).To(Succeed())
		Expect(k8sClient.Create(ctx, &configv1alpha1.EducatesClusterConfig{
			ObjectMeta: metav1.ObjectMeta{Name: "cluster"},
			Spec:       inlineSpecWithPullSecrets("mirror-pull-secret", "retired-pull-secret"),
		})).To(Succeed())
		r := makeReconciler()
		reconcileTwice(r)
		Expect(getSecret(platformnamespace.Name, "retired-pull-secret")).NotTo(BeNil())

		// A Secret of the operator's in the namespace that is not a pull
		// Secret copy is not the reconciler's to delete.
		Expect(k8sClient.Create(ctx, &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "unrelated",
				Namespace: platformnamespace.Name,
				Labels:    map[string]string{ownership.ManagedByLabel: ownership.ManagedByValue},
			},
		})).To(Succeed())

		obj := &configv1alpha1.EducatesClusterConfig{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "cluster"}, obj)).To(Succeed())
		obj.Spec.Inline.ImageRegistry.PullSecrets = pullSecretRefs("mirror-pull-secret")
		Expect(k8sClient.Update(ctx, obj)).To(Succeed())
		reconcileTwice(r)

		Expect(getSecret(platformnamespace.Name, "retired-pull-secret")).To(BeNil())
		expectPullSecretCopy(platformnamespace.Name, kept)
		Expect(getSecret(platformnamespace.Name, "unrelated")).NotTo(BeNil())

		// Removing the image registry removes the last copy.
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "cluster"}, obj)).To(Succeed())
		obj.Spec.Inline.ImageRegistry = nil
		Expect(k8sClient.Update(ctx, obj)).To(Succeed())
		reconcileTwice(r)
		Expect(getSecret(platformnamespace.Name, "mirror-pull-secret")).To(BeNil())
	})

	It("flips to Degraded naming a pull Secret missing from the operator namespace, and recovers once it exists", func() {
		Expect(k8sClient.Create(ctx, &configv1alpha1.EducatesClusterConfig{
			ObjectMeta: metav1.ObjectMeta{Name: "cluster"},
			Spec:       inlineSpecWithPullSecrets("mirror-pull-secret"),
		})).To(Succeed())
		r := makeReconciler()
		reconcileTwice(r)

		Expect(readyConditionStatus()).To(Equal(metav1.ConditionFalse))
		Expect(validationSucceededMessage()).To(And(
			ContainSubstring("spec.inline.imageRegistry.pullSecrets[0]"),
			ContainSubstring(`"mirror-pull-secret"`),
			ContainSubstring("not found"),
		))

		source := makePullSecret(testOperatorNamespace, "mirror-pull-secret", "late")
		Expect(k8sClient.Create(ctx, source)).To(Succeed())
		reconcileTwice(r)

		Expect(readyConditionStatus()).To(Equal(metav1.ConditionTrue))
		expectPullSecretCopy(platformnamespace.Name, source)
	})

	It("leaves the source untouched when the operator runs in the educates namespace", func() {
		// The operator namespace already holds the Secrets, and a copy
		// applied over a source would make the EducatesClusterConfig its
		// controller, so the source would be garbage collected with it.
		ensureNamespace(platformnamespace.Name)
		wildcard := makeWildcardSecret(true)
		wildcard.Namespace = platformnamespace.Name
		Expect(k8sClient.Create(ctx, wildcard)).To(Succeed())
		Expect(k8sClient.Create(ctx, makePullSecret(platformnamespace.Name, "mirror-pull-secret", "source"))).To(Succeed())
		Expect(k8sClient.Create(ctx, &configv1alpha1.EducatesClusterConfig{
			ObjectMeta: metav1.ObjectMeta{Name: "cluster"},
			Spec:       inlineSpecWithPullSecrets("mirror-pull-secret"),
		})).To(Succeed())

		r := makeReconciler()
		r.OperatorNamespace = platformnamespace.Name
		reconcileTwice(r)

		Expect(readyConditionStatus()).To(Equal(metav1.ConditionTrue))
		source := getSecret(platformnamespace.Name, "mirror-pull-secret")
		Expect(source.OwnerReferences).To(BeEmpty())
		Expect(source.Labels).NotTo(HaveKey(imagePullSecretCopyLabel))
	})

	It("flips to Degraded when a pull Secret is not a registry credentials Secret", func() {
		Expect(k8sClient.Create(ctx, makeSecret(testOperatorNamespace, "mirror-pull-secret", map[string][]byte{
			"password": []byte("not-a-docker-config"),
		}))).To(Succeed())
		Expect(k8sClient.Create(ctx, &configv1alpha1.EducatesClusterConfig{
			ObjectMeta: metav1.ObjectMeta{Name: "cluster"},
			Spec:       inlineSpecWithPullSecrets("mirror-pull-secret"),
		})).To(Succeed())
		reconcileTwice(makeReconciler())

		Expect(readyConditionStatus()).To(Equal(metav1.ConditionFalse))
		Expect(validationSucceededMessage()).To(And(
			ContainSubstring("spec.inline.imageRegistry.pullSecrets[0]"),
			ContainSubstring(string(corev1.SecretTypeOpaque)),
			ContainSubstring(string(corev1.SecretTypeDockerConfigJson)),
		))
		Expect(getSecret(platformnamespace.Name, "mirror-pull-secret")).To(BeNil())
	})
})
