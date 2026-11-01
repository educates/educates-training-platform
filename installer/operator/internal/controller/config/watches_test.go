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
	"context"
	"time"

	cmv1 "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"
	cmmeta "github.com/cert-manager/cert-manager/pkg/apis/meta/v1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	crconfig "sigs.k8s.io/controller-runtime/pkg/config"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	configv1alpha1 "github.com/educates/educates-training-platform/installer/operator/api/config/v1alpha1"
	"github.com/educates/educates-training-platform/installer/operator/internal/kyverno/kyvernotest"
	"github.com/educates/educates-training-platform/installer/operator/internal/testpki"
)

// makeReadyClusterIssuer returns a self-signed ClusterIssuer resource;
// the caller is expected to also Status().Update() it to Ready=True.
func makeReadyClusterIssuer(name string) *cmv1.ClusterIssuer {
	return &cmv1.ClusterIssuer{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Spec: cmv1.IssuerSpec{
			IssuerConfig: cmv1.IssuerConfig{
				SelfSigned: &cmv1.SelfSignedIssuer{},
			},
		},
	}
}

// markClusterIssuerReady writes Ready=True to the named ClusterIssuer's
// status subresource. cert-manager itself would set this in production;
// envtest has no controller, so the test drives the transition.
func markClusterIssuerReady(name string, ready bool) {
	GinkgoHelper()
	ci := &cmv1.ClusterIssuer{}
	Expect(k8sClient.Get(ctx, types.NamespacedName{Name: name}, ci)).To(Succeed())
	status := cmmeta.ConditionTrue
	if !ready {
		status = cmmeta.ConditionFalse
	}
	ci.Status = cmv1.IssuerStatus{
		Conditions: []cmv1.IssuerCondition{{
			Type:               cmv1.IssuerConditionReady,
			Status:             status,
			LastTransitionTime: &metav1.Time{Time: time.Now()},
			Reason:             "Test",
			Message:            "set by envtest",
		}},
	}
	Expect(k8sClient.Status().Update(ctx, ci)).To(Succeed())
}

// readyConditionStatus returns the Ready condition's status, or
// ConditionUnknown if the resource or condition is missing. Used as
// the polling target for Eventually().
func readyConditionStatus() metav1.ConditionStatus {
	got := &configv1alpha1.EducatesClusterConfig{}
	if err := k8sClient.Get(ctx, types.NamespacedName{Name: "cluster"}, got); err != nil {
		return metav1.ConditionUnknown
	}
	cond := meta.FindStatusCondition(got.Status.Conditions, conditionReady)
	if cond == nil {
		return metav1.ConditionUnknown
	}
	return cond.Status
}

var _ = Describe("EducatesClusterConfig watches (manager-driven)", func() {
	var mgrCancel context.CancelFunc
	var mgrDone chan error
	var mgrCache cache.Cache

	BeforeEach(func() {
		ensureNamespace(testOperatorNamespace)
		ensureNamespace(testCASecretsNamespace)

		var mgrCtx context.Context
		mgrCtx, mgrCancel = context.WithCancel(ctx)

		mgr, err := ctrl.NewManager(cfg, ctrl.Options{
			Scheme: k8sClient.Scheme(),
			Cache: cache.Options{
				ByObject: map[client.Object]cache.ByObject{
					&corev1.Secret{}: {
						Namespaces: map[string]cache.Config{
							testOperatorNamespace:  {},
							testCASecretsNamespace: {},
						},
					},
				},
			},
			// Disable the metrics server in-test; envtest doesn't need it
			// and binding a port can collide across specs.
			Metrics: metricsserver.Options{BindAddress: "0"},
			// Skip controller-name uniqueness check: each spec spins up
			// its own manager, but controller-runtime's name registry is
			// process-global, so the second spec's SetupWithManager would
			// otherwise reject the duplicate.
			Controller: crconfig.Controller{SkipNameValidation: new(true)},
		})
		Expect(err).NotTo(HaveOccurred())
		mgrCache = mgr.GetCache()

		Expect((&EducatesClusterConfigReconciler{
			Client:            mgr.GetClient(),
			APIReader:         mgr.GetAPIReader(),
			Scheme:            mgr.GetScheme(),
			OperatorNamespace: testOperatorNamespace,
			// envtest serves no Kyverno; validInlineSpec declares it.
			KyvernoDiscovery: &kyvernotest.Discovery{},
		}).SetupWithManager(mgr)).To(Succeed())

		mgrDone = make(chan error, 1)
		go func() {
			defer GinkgoRecover()
			mgrDone <- mgr.Start(mgrCtx)
		}()
	})

	AfterEach(func() {
		mgrCancel()
		Eventually(mgrDone, 10*time.Second).Should(Receive())
		drainCR()
		_ = k8sClient.DeleteAllOf(ctx, &corev1.Secret{}, client.InNamespace(testOperatorNamespace))
		_ = k8sClient.DeleteAllOf(ctx, &corev1.Secret{}, client.InNamespace(testCASecretsNamespace))
		_ = k8sClient.DeleteAllOf(ctx, &networkingv1.IngressClass{})
		_ = k8sClient.DeleteAllOf(ctx, &cmv1.ClusterIssuer{})
	})

	// Inline mode also resyncs every inlineSteadyStateResync, so these
	// specs wait for less than that: only the Secret watch can deliver
	// the change in time.
	watchDelivery := inlineSteadyStateResync - 5*time.Second

	It("flips status to Degraded when a wildcard Secret in another namespace is deleted", func() {
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
		Eventually(readyConditionStatus, 30*time.Second, 200*time.Millisecond).
			Should(Equal(metav1.ConditionTrue), "expected Ready=True after initial reconcile")

		Expect(k8sClient.Delete(ctx, wildcard)).To(Succeed())

		Eventually(readyConditionStatus, watchDelivery, 200*time.Millisecond).
			Should(Equal(metav1.ConditionFalse), "expected Ready=False after Secret deletion")
	})

	It("refreshes educates-ingress-ca when the CA Secret in another namespace changes", func() {
		Expect(k8sClient.Create(ctx, makeIngressClass())).To(Succeed())
		first, second := testpki.NewRootCA(), testpki.NewRootCA()
		// The wildcard certificate must keep verifying against the CA, so
		// the change adds a second root to the bundle.
		Expect(k8sClient.Create(ctx, makeWildcardSecretIssuedBy(first))).To(Succeed())
		bundle := append(append([]byte{}, first.CertPEM...), second.CertPEM...)
		trust := makeSecret(testCASecretsNamespace, "byo-ca", map[string][]byte{"ca.crt": first.CertPEM})
		Expect(k8sClient.Create(ctx, trust)).To(Succeed())

		spec := validInlineSpec()
		spec.Inline.Ingress.CACertificateSecretRef = &configv1alpha1.SecretReference{
			Name:      "byo-ca",
			Namespace: testCASecretsNamespace,
		}
		Expect(k8sClient.Create(ctx, &configv1alpha1.EducatesClusterConfig{
			ObjectMeta: metav1.ObjectMeta{Name: "cluster"},
			Spec:       spec,
		})).To(Succeed())
		Eventually(ingressCAData, 30*time.Second, 200*time.Millisecond).
			Should(Equal(map[string][]byte{"ca.crt": first.CertPEM}))

		Expect(k8sClient.Get(ctx, types.NamespacedName{Namespace: testCASecretsNamespace, Name: "byo-ca"}, trust)).To(Succeed())
		trust.Data = map[string][]byte{"ca.crt": bundle}
		Expect(k8sClient.Update(ctx, trust)).To(Succeed())

		Eventually(ingressCAData, watchDelivery, 200*time.Millisecond).
			Should(Equal(map[string][]byte{"ca.crt": bundle}))
	})

	It("flips status from Ready to Degraded when the wildcard Secret is deleted", func() {
		Expect(k8sClient.Create(ctx, makeIngressClass())).To(Succeed())
		Expect(k8sClient.Create(ctx, makeWildcardSecret(true))).To(Succeed())

		obj := &configv1alpha1.EducatesClusterConfig{
			ObjectMeta: metav1.ObjectMeta{Name: "cluster"},
			Spec:       validInlineSpec(),
		}
		Expect(k8sClient.Create(ctx, obj)).To(Succeed())

		Eventually(readyConditionStatus, 30*time.Second, 200*time.Millisecond).
			Should(Equal(metav1.ConditionTrue), "expected Ready=True after initial reconcile")

		// Delete the Secret. The Secret watch should map back to the
		// singleton Reconcile, which finds the missing Secret and writes
		// Degraded.
		Expect(k8sClient.Delete(ctx, &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: "wildcard-tls", Namespace: testOperatorNamespace},
		})).To(Succeed())

		Eventually(readyConditionStatus, 30*time.Second, 200*time.Millisecond).
			Should(Equal(metav1.ConditionFalse), "expected Ready=False after Secret deletion")
	})

	It("flips status from Ready to Degraded when a referenced ClusterIssuer is deleted", func() {
		Expect(k8sClient.Create(ctx, makeIngressClass())).To(Succeed())
		Expect(k8sClient.Create(ctx, makeWildcardSecret(true))).To(Succeed())
		Expect(k8sClient.Create(ctx, makeReadyClusterIssuer("test-issuer"))).To(Succeed())
		markClusterIssuerReady("test-issuer", true)

		spec := validInlineSpec()
		spec.Inline.Ingress.ClusterIssuerRef = &configv1alpha1.LocalObjectReference{Name: "test-issuer"}

		obj := &configv1alpha1.EducatesClusterConfig{
			ObjectMeta: metav1.ObjectMeta{Name: "cluster"},
			Spec:       spec,
		}
		Expect(k8sClient.Create(ctx, obj)).To(Succeed())

		Eventually(readyConditionStatus, 30*time.Second, 200*time.Millisecond).
			Should(Equal(metav1.ConditionTrue), "expected Ready=True after initial reconcile")

		// The ClusterIssuer watch is a deferred (unstructured) informer
		// registered at runtime by CRDWatcher, not at manager startup. Its
		// initial LIST/WATCH sync races the Delete below: if the delete
		// lands before the informer is streaming, the DELETE event is
		// dropped and — with no periodic resync — nothing ever re-triggers
		// the reconcile, wedging status at Ready (this is the CI flake).
		// Gate on the deferred informer being live by reading the issuer
		// back through the manager cache with the *same* unstructured GVK
		// the watch uses (a typed read would hit a separate informer, per
		// checkClusterIssuer). A cache hit proves the informer is synced
		// and will observe the delete.
		issuerGVK := schema.GroupVersionKind{
			Group:   cmv1.SchemeGroupVersion.Group,
			Version: cmv1.SchemeGroupVersion.Version,
			Kind:    "ClusterIssuer",
		}
		Eventually(func() error {
			u := &unstructured.Unstructured{}
			u.SetGroupVersionKind(issuerGVK)
			return mgrCache.Get(ctx, types.NamespacedName{Name: "test-issuer"}, u)
		}, 30*time.Second, 200*time.Millisecond).
			Should(Succeed(), "deferred ClusterIssuer watch did not become live")

		// Delete the ClusterIssuer. The ClusterIssuer watch should map back
		// to the singleton Reconcile, which finds the missing issuer and
		// writes Degraded.
		Expect(k8sClient.Delete(ctx, &cmv1.ClusterIssuer{
			ObjectMeta: metav1.ObjectMeta{Name: "test-issuer"},
		})).To(Succeed())

		Eventually(readyConditionStatus, 30*time.Second, 200*time.Millisecond).
			Should(Equal(metav1.ConditionFalse), "expected Ready=False after ClusterIssuer deletion")
	})
})
