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
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	configv1alpha1 "github.com/educates/educates-training-platform/installer/operator/api/config/v1alpha1"
	"github.com/educates/educates-training-platform/installer/operator/internal/kyverno/kyvernotest"
	"github.com/educates/educates-training-platform/installer/operator/internal/pki"
	"github.com/educates/educates-training-platform/installer/operator/internal/testpki"
)

const testOperatorNamespace = "test-operator"

// reconcileTwice runs Reconcile once to add the finalizer (first call
// returns Requeue) and once more to write status. Real users hit the
// same two-pass sequence via the controller-runtime queue.
func reconcileTwice(r *EducatesClusterConfigReconciler) {
	GinkgoHelper()
	req := ctrl.Request{NamespacedName: types.NamespacedName{Name: "cluster"}}
	_, err := r.Reconcile(ctx, req)
	Expect(err).NotTo(HaveOccurred())
	_, err = r.Reconcile(ctx, req)
	Expect(err).NotTo(HaveOccurred())
}

// drainCR removes the finalizer (so Delete actually deletes), deletes
// the CR, and waits until it's gone. Used in AfterEach because the
// reconciler uses finalizers but we don't run a manager during
// envtest.
func drainCR() {
	GinkgoHelper()
	obj := &configv1alpha1.EducatesClusterConfig{}
	if err := k8sClient.Get(ctx, types.NamespacedName{Name: "cluster"}, obj); err != nil {
		if apierrors.IsNotFound(err) {
			return
		}
		Expect(err).NotTo(HaveOccurred())
	}
	obj.Finalizers = nil
	Expect(k8sClient.Update(ctx, obj)).To(Succeed())
	Expect(k8sClient.Delete(ctx, obj)).To(Succeed())
	Eventually(func() bool {
		err := k8sClient.Get(ctx, types.NamespacedName{Name: "cluster"}, obj)
		return apierrors.IsNotFound(err)
	}).Should(BeTrue())
}

func makeReconciler() *EducatesClusterConfigReconciler {
	return &EducatesClusterConfigReconciler{
		Client: k8sClient,
		// k8sClient is already uncached (constructed directly from
		// the envtest REST config), so it doubles as the APIReader.
		APIReader:         k8sClient,
		Scheme:            k8sClient.Scheme(),
		OperatorNamespace: testOperatorNamespace,
		// envtest serves no Kyverno; validInlineSpec declares it.
		KyvernoDiscovery: &kyvernotest.Discovery{},
	}
}

// kyvernoNotInstalled returns a presence-check answer for a cluster
// without Kyverno.
func kyvernoNotInstalled() *kyvernotest.Discovery {
	d := &kyvernotest.Discovery{}
	d.SetServed(false)
	return d
}

func validInlineSpec() configv1alpha1.EducatesClusterConfigSpec {
	return configv1alpha1.EducatesClusterConfigSpec{
		Mode: configv1alpha1.ClusterConfigModeInline,
		Inline: &configv1alpha1.InlineConfig{
			Ingress: configv1alpha1.InlineIngress{
				Domain:           "educates.test",
				IngressClassName: "contour",
				WildcardCertificateSecretRef: &configv1alpha1.SecretReference{
					Name: "wildcard-tls",
				},
			},
			PolicyEnforcement: configv1alpha1.InlinePolicyEnforcement{
				ClusterPolicyEngine:  configv1alpha1.ClusterPolicyEngineKyverno,
				WorkshopPolicyEngine: configv1alpha1.WorkshopPolicyEngineKyverno,
			},
		},
	}
}

func ensureNamespace(name string) {
	GinkgoHelper()
	ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: name}}
	err := k8sClient.Create(ctx, ns)
	if err != nil && !apierrors.IsAlreadyExists(err) {
		Expect(err).NotTo(HaveOccurred())
	}
}

func makeIngressClass() *networkingv1.IngressClass {
	return &networkingv1.IngressClass{
		ObjectMeta: metav1.ObjectMeta{Name: "contour"},
		Spec: networkingv1.IngressClassSpec{
			Controller: "test/example",
		},
	}
}

// makeWildcardSecret returns the wildcard Secret validInlineSpec
// references, holding a certificate for *.educates.test from a new
// root CA, with or without its tls.crt.
func makeWildcardSecret(withTLSCrt bool) *corev1.Secret {
	s := makeWildcardSecretIssuedBy(testpki.NewRootCA())
	if !withTLSCrt {
		delete(s.Data, "tls.crt")
	}
	return s
}

// makeWildcardSecretIssuedBy returns the wildcard Secret validInlineSpec
// references, holding a certificate for *.educates.test that ca issued.
func makeWildcardSecretIssuedBy(ca *testpki.Certificate) *corev1.Secret {
	leaf := ca.NewLeaf(testpki.WithDNSNames("*.educates.test"))
	// Type intentionally Opaque — kubernetes.io/tls Secrets are
	// apiserver-validated to require both tls.crt + tls.key, which would
	// block tests that exercise the "missing key" validator path.
	return makeSecret(testOperatorNamespace, "wildcard-tls", map[string][]byte{
		"tls.crt": leaf.ChainPEM(),
		"tls.key": leaf.KeyPEM,
	})
}

var _ = Describe("EducatesClusterConfig Inline-mode reconciler", func() {
	BeforeEach(func() {
		ensureNamespace(testOperatorNamespace)
		ensureNamespace(testCASecretsNamespace)
	})

	AfterEach(func() {
		drainCR()
		// Best-effort cleanup of supporting resources; ignore not-found.
		_ = k8sClient.DeleteAllOf(ctx, &corev1.Secret{}, client.InNamespace(testOperatorNamespace))
		_ = k8sClient.DeleteAllOf(ctx, &corev1.Secret{}, client.InNamespace(testCASecretsNamespace))
		_ = k8sClient.DeleteAllOf(ctx, &networkingv1.IngressClass{})
	})

	It("flips to Ready and publishes status when all refs validate", func() {
		Expect(k8sClient.Create(ctx, makeIngressClass())).To(Succeed())
		Expect(k8sClient.Create(ctx, makeWildcardSecret(true))).To(Succeed())

		obj := &configv1alpha1.EducatesClusterConfig{
			ObjectMeta: metav1.ObjectMeta{Name: "cluster"},
			Spec:       validInlineSpec(),
		}
		Expect(k8sClient.Create(ctx, obj)).To(Succeed())

		reconcileTwice(makeReconciler())

		got := &configv1alpha1.EducatesClusterConfig{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "cluster"}, got)).To(Succeed())

		Expect(got.Finalizers).To(ContainElement(finalizerName))
		Expect(got.Status.Phase).To(Equal(configv1alpha1.ClusterConfigPhaseReady))
		Expect(got.Status.Mode).To(Equal(configv1alpha1.ClusterConfigModeInline))
		Expect(got.Status.Ingress).NotTo(BeNil())
		Expect(got.Status.Ingress.Domain).To(Equal("educates.test"))
		Expect(got.Status.Ingress.IngressClassName).To(Equal("contour"))
		Expect(got.Status.Ingress.WildcardCertificateSecretRef.Namespace).To(Equal(testOperatorNamespace))
		Expect(got.Status.Ingress.WildcardCertificateSecretRef.Name).To(Equal("wildcard-tls"))
		Expect(got.Status.PolicyEnforcement).NotTo(BeNil())
		Expect(got.Status.PolicyEnforcement.ClusterPolicyEngine).To(Equal(configv1alpha1.ClusterPolicyEngineKyverno))
		Expect(got.Status.ImageRegistry).NotTo(BeNil()) // empty but populated

		ready := meta.FindStatusCondition(got.Status.Conditions, conditionReady)
		Expect(ready).NotTo(BeNil())
		Expect(ready.Status).To(Equal(metav1.ConditionTrue))

		val := meta.FindStatusCondition(got.Status.Conditions, conditionValidationSucceeded)
		Expect(val).NotTo(BeNil())
		Expect(val.Status).To(Equal(metav1.ConditionTrue))

		certs := meta.FindStatusCondition(got.Status.Conditions, conditionCertificatesReady)
		Expect(certs).NotTo(BeNil())
		Expect(certs.Status).To(Equal(metav1.ConditionTrue))
		Expect(certs.Reason).To(Equal("InlineCertificatesValid"))
	})

	// Inline mode asserts what the cluster already runs, and Educates
	// workshop security needs Kyverno itself, so a declared Kyverno
	// engine is checked like any other declared resource.
	It("flips to Degraded while a declared Kyverno engine has no Kyverno, and recovers once it is served", func() {
		Expect(k8sClient.Create(ctx, makeIngressClass())).To(Succeed())
		Expect(k8sClient.Create(ctx, makeWildcardSecret(true))).To(Succeed())
		Expect(k8sClient.Create(ctx, &configv1alpha1.EducatesClusterConfig{
			ObjectMeta: metav1.ObjectMeta{Name: "cluster"},
			Spec:       validInlineSpec(),
		})).To(Succeed())

		r := makeReconciler()
		kyvernoDiscovery := kyvernoNotInstalled()
		r.KyvernoDiscovery = kyvernoDiscovery
		reconcileTwice(r)

		got := &configv1alpha1.EducatesClusterConfig{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "cluster"}, got)).To(Succeed())
		Expect(got.Status.Phase).To(Equal(configv1alpha1.ClusterConfigPhaseDegraded))
		val := meta.FindStatusCondition(got.Status.Conditions, conditionValidationSucceeded)
		Expect(val).NotTo(BeNil())
		Expect(val.Status).To(Equal(metav1.ConditionFalse))
		Expect(val.Message).To(And(
			ContainSubstring("spec.inline.policyEnforcement:"),
			ContainSubstring("the API server does not serve")))
		policy := meta.FindStatusCondition(got.Status.Conditions, conditionPolicyEnforcementReady)
		Expect(policy).NotTo(BeNil())
		Expect(policy.Status).To(Equal(metav1.ConditionFalse))
		Expect(policy.Reason).To(Equal("KyvernoNotFound"))

		kyvernoDiscovery.SetServed(true)
		reconcileTwice(r)

		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "cluster"}, got)).To(Succeed())
		Expect(got.Status.Phase).To(Equal(configv1alpha1.ClusterConfigPhaseReady))
		policy = meta.FindStatusCondition(got.Status.Conditions, conditionPolicyEnforcementReady)
		Expect(policy).NotTo(BeNil())
		Expect(policy.Status).To(Equal(metav1.ConditionTrue))
	})

	It("does not look for Kyverno when both declared engines are None", func() {
		Expect(k8sClient.Create(ctx, makeIngressClass())).To(Succeed())
		Expect(k8sClient.Create(ctx, makeWildcardSecret(true))).To(Succeed())
		spec := validInlineSpec()
		spec.Inline.PolicyEnforcement = configv1alpha1.InlinePolicyEnforcement{
			ClusterPolicyEngine:  configv1alpha1.ClusterPolicyEngineNone,
			WorkshopPolicyEngine: configv1alpha1.WorkshopPolicyEngineNone,
		}
		Expect(k8sClient.Create(ctx, &configv1alpha1.EducatesClusterConfig{
			ObjectMeta: metav1.ObjectMeta{Name: "cluster"},
			Spec:       spec,
		})).To(Succeed())

		r := makeReconciler()
		r.KyvernoDiscovery = kyvernoNotInstalled()
		reconcileTwice(r)

		got := &configv1alpha1.EducatesClusterConfig{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "cluster"}, got)).To(Succeed())
		Expect(got.Status.Phase).To(Equal(configv1alpha1.ClusterConfigPhaseReady))
		Expect(got.Status.PolicyEnforcement).To(Equal(&configv1alpha1.StatusPolicyEnforcement{
			ClusterPolicyEngine:  configv1alpha1.ClusterPolicyEngineNone,
			WorkshopPolicyEngine: configv1alpha1.WorkshopPolicyEngineNone,
		}))
	})

	DescribeTable("reports an unusable wildcard certificate with the same reason as StaticCertificate",
		func(setup func(spec *configv1alpha1.EducatesClusterConfigSpec) []*corev1.Secret, wantReason string) {
			Expect(k8sClient.Create(ctx, makeIngressClass())).To(Succeed())
			spec := validInlineSpec()
			for _, s := range setup(&spec) {
				Expect(k8sClient.Create(ctx, s)).To(Succeed())
			}
			Expect(k8sClient.Create(ctx, &configv1alpha1.EducatesClusterConfig{
				ObjectMeta: metav1.ObjectMeta{Name: "cluster"},
				Spec:       spec,
			})).To(Succeed())

			reconcileTwice(makeReconciler())

			got := &configv1alpha1.EducatesClusterConfig{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "cluster"}, got)).To(Succeed())
			Expect(got.Status.Phase).To(Equal(configv1alpha1.ClusterConfigPhaseDegraded))
			Expect(meta.IsStatusConditionFalse(got.Status.Conditions, conditionReady)).To(BeTrue())
			certs := meta.FindStatusCondition(got.Status.Conditions, conditionCertificatesReady)
			Expect(certs).NotTo(BeNil())
			Expect(certs.Status).To(Equal(metav1.ConditionFalse))
			Expect(certs.Reason).To(Equal(wantReason))
		},
		Entry("when the wildcard Secret is missing",
			func(*configv1alpha1.EducatesClusterConfigSpec) []*corev1.Secret { return nil },
			pki.ReasonWildcardCertificateNotFound),
		Entry("when the wildcard Secret has no tls.crt",
			func(*configv1alpha1.EducatesClusterConfigSpec) []*corev1.Secret {
				return []*corev1.Secret{makeWildcardSecret(false)}
			},
			pki.ReasonWildcardCertificateIncomplete),
		Entry("when tls.key does not parse",
			func(*configv1alpha1.EducatesClusterConfigSpec) []*corev1.Secret {
				s := makeWildcardSecret(true)
				s.Data["tls.key"] = []byte("not a key")
				return []*corev1.Secret{s}
			},
			pki.ReasonWildcardCertificateUnparseable),
		Entry("when tls.key belongs to another certificate",
			func(*configv1alpha1.EducatesClusterConfigSpec) []*corev1.Secret {
				s := makeWildcardSecret(true)
				s.Data["tls.key"] = makeWildcardSecret(true).Data["tls.key"]
				return []*corev1.Secret{s}
			},
			pki.ReasonWildcardCertificateKeyMismatch),
		Entry("when its SANs do not cover the ingress domain",
			func(spec *configv1alpha1.EducatesClusterConfigSpec) []*corev1.Secret {
				spec.Inline.Ingress.Domain = "elsewhere.test"
				return []*corev1.Secret{makeWildcardSecret(true)}
			},
			pki.ReasonWildcardCertificateDomainMismatch),
		Entry("when it has expired",
			func(*configv1alpha1.EducatesClusterConfigSpec) []*corev1.Secret {
				leaf := testpki.NewRootCA().NewLeaf(testpki.WithDNSNames("*.educates.test"),
					testpki.WithValidity(time.Now().Add(-48*time.Hour), time.Now().Add(-24*time.Hour)))
				return []*corev1.Secret{makeSecret(testOperatorNamespace, "wildcard-tls", map[string][]byte{
					"tls.crt": leaf.CertPEM,
					"tls.key": leaf.KeyPEM,
				})}
			},
			pki.ReasonWildcardCertificateExpired),
		Entry("when its chain does not verify against the CA",
			func(spec *configv1alpha1.EducatesClusterConfigSpec) []*corev1.Secret {
				spec.Inline.Ingress.CACertificateSecretRef = &configv1alpha1.SecretReference{Name: "byo-ca"}
				return []*corev1.Secret{
					makeWildcardSecret(true),
					makeSecret(testOperatorNamespace, "byo-ca", map[string][]byte{"ca.crt": testpki.NewRootCA().CertPEM}),
				}
			},
			pki.ReasonWildcardCertificateUntrusted),
	)

	It("flips to Degraded when the wildcard Secret is missing", func() {
		Expect(k8sClient.Create(ctx, makeIngressClass())).To(Succeed())
		// No wildcard Secret created.

		obj := &configv1alpha1.EducatesClusterConfig{
			ObjectMeta: metav1.ObjectMeta{Name: "cluster"},
			Spec:       validInlineSpec(),
		}
		Expect(k8sClient.Create(ctx, obj)).To(Succeed())

		reconcileTwice(makeReconciler())

		got := &configv1alpha1.EducatesClusterConfig{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "cluster"}, got)).To(Succeed())
		Expect(got.Status.Phase).To(Equal(configv1alpha1.ClusterConfigPhaseDegraded))

		ready := meta.FindStatusCondition(got.Status.Conditions, conditionReady)
		Expect(ready.Status).To(Equal(metav1.ConditionFalse))
		Expect(ready.Message).To(ContainSubstring("wildcardCertificateSecretRef"))
		Expect(ready.Message).To(ContainSubstring("not found"))
	})

	It("flips to Degraded when the wildcard Secret is missing tls.crt", func() {
		Expect(k8sClient.Create(ctx, makeIngressClass())).To(Succeed())
		Expect(k8sClient.Create(ctx, makeWildcardSecret(false))).To(Succeed())

		obj := &configv1alpha1.EducatesClusterConfig{
			ObjectMeta: metav1.ObjectMeta{Name: "cluster"},
			Spec:       validInlineSpec(),
		}
		Expect(k8sClient.Create(ctx, obj)).To(Succeed())

		reconcileTwice(makeReconciler())

		got := &configv1alpha1.EducatesClusterConfig{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "cluster"}, got)).To(Succeed())
		Expect(got.Status.Phase).To(Equal(configv1alpha1.ClusterConfigPhaseDegraded))

		ready := meta.FindStatusCondition(got.Status.Conditions, conditionReady)
		Expect(ready.Status).To(Equal(metav1.ConditionFalse))
		Expect(ready.Message).To(ContainSubstring(`"tls.crt"`))
	})

	It("flips to Degraded when the IngressClass is missing", func() {
		// No IngressClass created.
		Expect(k8sClient.Create(ctx, makeWildcardSecret(true))).To(Succeed())

		obj := &configv1alpha1.EducatesClusterConfig{
			ObjectMeta: metav1.ObjectMeta{Name: "cluster"},
			Spec:       validInlineSpec(),
		}
		Expect(k8sClient.Create(ctx, obj)).To(Succeed())

		reconcileTwice(makeReconciler())

		got := &configv1alpha1.EducatesClusterConfig{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "cluster"}, got)).To(Succeed())
		Expect(got.Status.Phase).To(Equal(configv1alpha1.ClusterConfigPhaseDegraded))

		ready := meta.FindStatusCondition(got.Status.Conditions, conditionReady)
		Expect(ready.Message).To(ContainSubstring("IngressClass"))
		Expect(ready.Message).To(ContainSubstring(`"contour"`))
	})

	It("flips to Degraded when an optional CA Secret is referenced but missing", func() {
		Expect(k8sClient.Create(ctx, makeIngressClass())).To(Succeed())
		Expect(k8sClient.Create(ctx, makeWildcardSecret(true))).To(Succeed())

		spec := validInlineSpec()
		spec.Inline.Ingress.CACertificateSecretRef = &configv1alpha1.SecretReference{Name: "ca-bundle"}

		obj := &configv1alpha1.EducatesClusterConfig{
			ObjectMeta: metav1.ObjectMeta{Name: "cluster"},
			Spec:       spec,
		}
		Expect(k8sClient.Create(ctx, obj)).To(Succeed())

		reconcileTwice(makeReconciler())

		got := &configv1alpha1.EducatesClusterConfig{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "cluster"}, got)).To(Succeed())
		Expect(got.Status.Phase).To(Equal(configv1alpha1.ClusterConfigPhaseDegraded))

		ready := meta.FindStatusCondition(got.Status.Conditions, conditionReady)
		Expect(ready.Message).To(ContainSubstring("caCertificateSecretRef"))
		Expect(certificatesReadyReason()).To(Equal(pki.ReasonCATrustSecretNotFound))
	})

	It("publishes a wildcard Secret in another namespace with that namespace", func() {
		Expect(k8sClient.Create(ctx, makeIngressClass())).To(Succeed())
		wildcard := makeWildcardSecret(true)
		wildcard.Namespace = testCASecretsNamespace
		Expect(k8sClient.Create(ctx, wildcard)).To(Succeed())

		spec := validInlineSpec()
		spec.Inline.Ingress.WildcardCertificateSecretRef.Namespace = testCASecretsNamespace
		Expect(k8sClient.Create(ctx, &configv1alpha1.EducatesClusterConfig{
			ObjectMeta: metav1.ObjectMeta{Name: "cluster"},
			Spec:       spec,
		})).To(Succeed())

		reconcileTwice(makeReconciler())

		got := &configv1alpha1.EducatesClusterConfig{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "cluster"}, got)).To(Succeed())
		Expect(got.Status.Phase).To(Equal(configv1alpha1.ClusterConfigPhaseReady))
		Expect(got.Status.Ingress.WildcardCertificateSecretRef).To(Equal(&configv1alpha1.NamespacedSecretRef{
			Namespace: testCASecretsNamespace,
			Name:      "wildcard-tls",
		}))
	})

	It("flips to Degraded when the wildcard Secret is missing from the referenced namespace", func() {
		Expect(k8sClient.Create(ctx, makeIngressClass())).To(Succeed())
		// The Secret exists, but in the operator namespace only.
		Expect(k8sClient.Create(ctx, makeWildcardSecret(true))).To(Succeed())

		spec := validInlineSpec()
		spec.Inline.Ingress.WildcardCertificateSecretRef.Namespace = testCASecretsNamespace
		Expect(k8sClient.Create(ctx, &configv1alpha1.EducatesClusterConfig{
			ObjectMeta: metav1.ObjectMeta{Name: "cluster"},
			Spec:       spec,
		})).To(Succeed())

		reconcileTwice(makeReconciler())

		got := &configv1alpha1.EducatesClusterConfig{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "cluster"}, got)).To(Succeed())
		Expect(got.Status.Phase).To(Equal(configv1alpha1.ClusterConfigPhaseDegraded))
		ready := meta.FindStatusCondition(got.Status.Conditions, conditionReady)
		Expect(ready.Message).To(ContainSubstring("wildcardCertificateSecretRef"))
		Expect(ready.Message).To(ContainSubstring(testCASecretsNamespace + "/wildcard-tls not found"))
	})

	It("returns CertificatesReady to True once the wildcard certificate is fixed", func() {
		Expect(k8sClient.Create(ctx, makeIngressClass())).To(Succeed())
		wildcard := makeWildcardSecret(true)
		valid := wildcard.Data["tls.key"]
		wildcard.Data["tls.key"] = makeWildcardSecret(true).Data["tls.key"]
		Expect(k8sClient.Create(ctx, wildcard)).To(Succeed())
		Expect(k8sClient.Create(ctx, &configv1alpha1.EducatesClusterConfig{
			ObjectMeta: metav1.ObjectMeta{Name: "cluster"},
			Spec:       validInlineSpec(),
		})).To(Succeed())
		r := makeReconciler()
		reconcileTwice(r)
		Expect(certificatesReadyReason()).To(Equal(pki.ReasonWildcardCertificateKeyMismatch))

		wildcard.Data["tls.key"] = valid
		Expect(k8sClient.Update(ctx, wildcard)).To(Succeed())
		_, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: types.NamespacedName{Name: "cluster"}})
		Expect(err).NotTo(HaveOccurred())

		got := &configv1alpha1.EducatesClusterConfig{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "cluster"}, got)).To(Succeed())
		Expect(got.Status.Phase).To(Equal(configv1alpha1.ClusterConfigPhaseReady))
		Expect(meta.IsStatusConditionTrue(got.Status.Conditions, conditionCertificatesReady)).To(BeTrue())
		Expect(meta.IsStatusConditionTrue(got.Status.Conditions, conditionValidationSucceeded)).To(BeTrue())
	})

	It("publishes educates-ingress-ca holding only the CA Secret's ca.crt, even when it also holds a key", func() {
		Expect(k8sClient.Create(ctx, makeIngressClass())).To(Succeed())
		ca := testpki.NewRootCA()
		Expect(k8sClient.Create(ctx, makeWildcardSecretIssuedBy(ca))).To(Succeed())
		Expect(k8sClient.Create(ctx, makeSecret(testCASecretsNamespace, "byo-ca", map[string][]byte{
			"ca.crt":  ca.CertPEM,
			"tls.crt": ca.CertPEM,
			"tls.key": ca.KeyPEM,
		}))).To(Succeed())

		spec := validInlineSpec()
		spec.Inline.Ingress.CACertificateSecretRef = &configv1alpha1.SecretReference{
			Name:      "byo-ca",
			Namespace: testCASecretsNamespace,
		}
		Expect(k8sClient.Create(ctx, &configv1alpha1.EducatesClusterConfig{
			ObjectMeta: metav1.ObjectMeta{Name: "cluster"},
			Spec:       spec,
		})).To(Succeed())

		reconcileTwice(makeReconciler())

		got := &configv1alpha1.EducatesClusterConfig{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "cluster"}, got)).To(Succeed())
		Expect(got.Status.Phase).To(Equal(configv1alpha1.ClusterConfigPhaseReady))
		Expect(got.Status.Ingress.CACertificateSecretRef).To(Equal(&configv1alpha1.NamespacedSecretRef{
			Namespace: testOperatorNamespace,
			Name:      ingressCASecretName,
		}))
		Expect(ingressCAData()).To(Equal(map[string][]byte{"ca.crt": ca.CertPEM}))

		derived := &corev1.Secret{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Namespace: testOperatorNamespace, Name: ingressCASecretName}, derived)).To(Succeed())
		Expect(derived.Type).To(Equal(corev1.SecretTypeOpaque))
		Expect(derived.OwnerReferences).To(HaveLen(1))
		Expect(derived.OwnerReferences[0].Kind).To(Equal("EducatesClusterConfig"))
	})

	It("flips to Degraded when the CA Secret has no ca.crt", func() {
		Expect(k8sClient.Create(ctx, makeIngressClass())).To(Succeed())
		Expect(k8sClient.Create(ctx, makeWildcardSecret(true))).To(Succeed())
		ca := testpki.NewRootCA()
		Expect(k8sClient.Create(ctx, makeSecret(testOperatorNamespace, "byo-ca", map[string][]byte{
			"tls.crt": ca.CertPEM,
			"tls.key": ca.KeyPEM,
		}))).To(Succeed())

		spec := validInlineSpec()
		spec.Inline.Ingress.CACertificateSecretRef = &configv1alpha1.SecretReference{Name: "byo-ca"}
		Expect(k8sClient.Create(ctx, &configv1alpha1.EducatesClusterConfig{
			ObjectMeta: metav1.ObjectMeta{Name: "cluster"},
			Spec:       spec,
		})).To(Succeed())

		reconcileTwice(makeReconciler())

		got := &configv1alpha1.EducatesClusterConfig{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "cluster"}, got)).To(Succeed())
		Expect(got.Status.Phase).To(Equal(configv1alpha1.ClusterConfigPhaseDegraded))
		ready := meta.FindStatusCondition(got.Status.Conditions, conditionReady)
		Expect(ready.Message).To(ContainSubstring("caCertificateSecretRef"))
		Expect(ready.Message).To(ContainSubstring(`"ca.crt"`))
		Expect(certificatesReadyReason()).To(Equal(pki.ReasonCATrustSecretIncomplete))
		Expect(ingressCAData()).To(BeNil())
	})

	It("clears the finalizer on delete", func() {
		Expect(k8sClient.Create(ctx, makeIngressClass())).To(Succeed())
		Expect(k8sClient.Create(ctx, makeWildcardSecret(true))).To(Succeed())

		obj := &configv1alpha1.EducatesClusterConfig{
			ObjectMeta: metav1.ObjectMeta{Name: "cluster"},
			Spec:       validInlineSpec(),
		}
		Expect(k8sClient.Create(ctx, obj)).To(Succeed())
		reconcileTwice(makeReconciler())

		// Mark for deletion: the finalizer keeps it around until the
		// reconciler explicitly removes it.
		Expect(k8sClient.Delete(ctx, obj)).To(Succeed())

		// One more Reconcile pass: should drain the finalizer and let
		// the apiserver remove the object.
		_, err := makeReconciler().Reconcile(ctx, ctrl.Request{NamespacedName: types.NamespacedName{Name: "cluster"}})
		Expect(err).NotTo(HaveOccurred())

		got := &configv1alpha1.EducatesClusterConfig{}
		err = k8sClient.Get(ctx, types.NamespacedName{Name: "cluster"}, got)
		Expect(apierrors.IsNotFound(err)).To(BeTrue())
	})
})
