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
	"context"
	"sync"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	crconfig "sigs.k8s.io/controller-runtime/pkg/config"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	configv1alpha1 "github.com/educates/educates-training-platform/installer/operator/api/config/v1alpha1"
	platformv1alpha1 "github.com/educates/educates-training-platform/installer/operator/api/platform/v1alpha1"
	"github.com/educates/educates-training-platform/installer/operator/internal/helm"
	"github.com/educates/educates-training-platform/installer/operator/internal/ownership"
	vendoredcharts "github.com/educates/educates-training-platform/installer/operator/vendored-charts"
)

// memoryHelmFactory builds an in-memory Helm client per namespace and
// memoises the result so specs can assert against the release store.
type memoryHelmFactory struct {
	mu      sync.Mutex
	clients map[string]*helm.Client
}

func newMemoryHelmFactory() *memoryHelmFactory {
	return &memoryHelmFactory{clients: map[string]*helm.Client{}}
}

func (f *memoryHelmFactory) For(ns string) (*helm.Client, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if c, ok := f.clients[ns]; ok {
		return c, nil
	}
	c, err := helm.NewMemoryClient(ns)
	if err != nil {
		return nil, err
	}
	f.clients[ns] = c
	return c, nil
}

// makeReadyClusterConfig creates the EducatesClusterConfig singleton
// and stamps Ready=True onto its status subresource so the platform
// reconciler's gate passes. Mode + ingress are minimal — the
// reconciler only consults Status.Ready + Status.ImageRegistry +
// Status.PolicyEnforcement.
func makeReadyClusterConfig() {
	GinkgoHelper()
	cc := &configv1alpha1.EducatesClusterConfig{
		ObjectMeta: metav1.ObjectMeta{Name: configSingletonName},
		Spec: configv1alpha1.EducatesClusterConfigSpec{
			Mode: configv1alpha1.ClusterConfigModeInline,
			Inline: &configv1alpha1.InlineConfig{
				Ingress: configv1alpha1.InlineIngress{
					Domain:           "test.example.com",
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
		},
	}
	Expect(k8sClient.Create(ctx, cc)).To(Succeed())
	// Re-fetch so we have the assigned ResourceVersion before the
	// status write.
	Expect(k8sClient.Get(ctx, types.NamespacedName{Name: configSingletonName}, cc)).To(Succeed())
	cc.Status = configv1alpha1.EducatesClusterConfigStatus{
		Phase: configv1alpha1.ClusterConfigPhaseReady,
		Mode:  configv1alpha1.ClusterConfigModeInline,
		Conditions: []metav1.Condition{{
			Type:               conditionReady,
			Status:             metav1.ConditionTrue,
			Reason:             "Reconciled",
			Message:            "test fixture",
			LastTransitionTime: metav1.Now(),
		}},
		PolicyEnforcement: &configv1alpha1.StatusPolicyEnforcement{
			ClusterPolicyEngine:  configv1alpha1.ClusterPolicyEngineKyverno,
			WorkshopPolicyEngine: configv1alpha1.WorkshopPolicyEngineKyverno,
		},
		// Ingress contract — populated for LookupService specs (which
		// derive their hostname + TLS Secret from here). SecretsManager
		// specs don't read it but the field doesn't get in their way.
		Ingress: &configv1alpha1.StatusIngress{
			Domain:           "test.example.com",
			IngressClassName: "contour",
			Protocol:         configv1alpha1.IngressProtocolHTTPS,
			WildcardCertificateSecretRef: &configv1alpha1.NamespacedSecretRef{
				Namespace: "educates-installer",
				Name:      "wildcard-tls",
			},
		},
	}
	Expect(k8sClient.Status().Update(ctx, cc)).To(Succeed())
}

// publishIngressCAOnClusterConfig adds to the makeReadyClusterConfig
// fixture the CA reference a cluster with a custom CA publishes: the
// operator-owned, key-free educates-ingress-ca in the operator namespace.
func publishIngressCAOnClusterConfig() {
	GinkgoHelper()
	cc := &configv1alpha1.EducatesClusterConfig{}
	Expect(k8sClient.Get(ctx, types.NamespacedName{Name: configSingletonName}, cc)).To(Succeed())
	cc.Status.Ingress.CACertificateSecretRef = &configv1alpha1.NamespacedSecretRef{
		Namespace: "educates-installer",
		Name:      "educates-ingress-ca",
	}
	Expect(k8sClient.Status().Update(ctx, cc)).To(Succeed())
}

// ingressCAChartRef is the clusterIngress.caCertificateRef chart value
// publishIngressCAOnClusterConfig's reference must render to.
func ingressCAChartRef() map[string]any {
	return map[string]any{"name": "educates-ingress-ca", "namespace": "educates-installer"}
}

// markDeploymentAvailable creates (if missing) and patches the named
// Deployment to Available=True. envtest has no controllers, so specs
// drive the transition manually.
func markDeploymentAvailable(name string) {
	namespace := platformNamespace
	GinkgoHelper()
	one := int32(1)
	dep := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
		Spec: appsv1.DeploymentSpec{
			Replicas: &one,
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{"app": name},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"app": name}},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{{Name: "c", Image: "stub:latest"}},
				},
			},
		},
	}
	err := k8sClient.Create(ctx, dep)
	if err != nil && !apierrors.IsAlreadyExists(err) {
		Expect(err).NotTo(HaveOccurred())
	}
	Expect(k8sClient.Get(ctx, types.NamespacedName{Namespace: namespace, Name: name}, dep)).To(Succeed())
	dep.Status = appsv1.DeploymentStatus{
		Conditions: []appsv1.DeploymentCondition{{
			Type:   appsv1.DeploymentAvailable,
			Status: corev1.ConditionTrue,
		}},
	}
	Expect(k8sClient.Status().Update(ctx, dep)).To(Succeed())
}

func ensureNamespace(name string) {
	GinkgoHelper()
	ns := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: name}}
	err := k8sClient.Create(ctx, ns)
	if err != nil && !apierrors.IsAlreadyExists(err) {
		Expect(err).NotTo(HaveOccurred())
	}
}

func smReadyStatus(name string) metav1.ConditionStatus {
	got := &platformv1alpha1.SecretsManager{}
	if err := k8sClient.Get(ctx, types.NamespacedName{Name: name}, got); err != nil {
		return metav1.ConditionUnknown
	}
	c := meta.FindStatusCondition(got.Status.Conditions, conditionReady)
	if c == nil {
		return metav1.ConditionUnknown
	}
	return c.Status
}

var _ = Describe("SecretsManager reconciler", func() {
	var (
		mgrCancel context.CancelFunc
		mgrDone   chan error
		helmFac   *memoryHelmFactory
	)

	BeforeEach(func() {
		ensureNamespace(platformNamespace)
		helmFac = newMemoryHelmFactory()

		var mgrCtx context.Context
		mgrCtx, mgrCancel = context.WithCancel(ctx)

		mgr, err := ctrl.NewManager(cfg, ctrl.Options{
			Scheme:     k8sClient.Scheme(),
			Metrics:    metricsserver.Options{BindAddress: "0"},
			Controller: crconfig.Controller{SkipNameValidation: new(true)},
		})
		Expect(err).NotTo(HaveOccurred())

		Expect((&SecretsManagerReconciler{
			Client:        mgr.GetClient(),
			Scheme:        mgr.GetScheme(),
			HelmClientFor: helmFac.For,
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
		// Drain the SecretsManager singleton so the next spec starts
		// from a clean slate. Remove the finalizer first because the
		// previous manager isn't around to drain helm; without that,
		// Delete blocks forever waiting on cleanup.
		sm := &platformv1alpha1.SecretsManager{}
		if err := k8sClient.Get(ctx, types.NamespacedName{Name: singletonName}, sm); err == nil {
			sm.Finalizers = nil
			_ = k8sClient.Update(ctx, sm)
			_ = k8sClient.Delete(ctx, sm)
		}
		_ = k8sClient.DeleteAllOf(ctx, &configv1alpha1.EducatesClusterConfig{})
		_ = k8sClient.DeleteAllOf(ctx, &appsv1.Deployment{}, client.InNamespace(platformNamespace))
	})

	It("refuses to proceed when EducatesClusterConfig is missing", func() {
		sm := &platformv1alpha1.SecretsManager{
			ObjectMeta: metav1.ObjectMeta{Name: singletonName},
			Spec:       platformv1alpha1.SecretsManagerSpec{},
		}
		Expect(k8sClient.Create(ctx, sm)).To(Succeed())

		Eventually(func() string {
			got := &platformv1alpha1.SecretsManager{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: singletonName}, got); err != nil {
				return ""
			}
			c := meta.FindStatusCondition(got.Status.Conditions, conditionClusterConfigAvailable)
			if c == nil {
				return ""
			}
			return c.Reason
		}, 30*time.Second, 200*time.Millisecond).Should(Equal("ClusterConfigNotReady"))

		Expect(smReadyStatus(singletonName)).To(Equal(metav1.ConditionFalse))
	})

	It("installs the chart and reaches Ready=True when the Deployment is Available", func() {
		makeReadyClusterConfig()

		sm := &platformv1alpha1.SecretsManager{
			ObjectMeta: metav1.ObjectMeta{Name: singletonName},
			Spec:       platformv1alpha1.SecretsManagerSpec{},
		}
		Expect(k8sClient.Create(ctx, sm)).To(Succeed())

		// Wait for the operator to land the chart — the in-memory
		// helm client records the release in its store.
		Eventually(func() error {
			hc, err := helmFac.For(platformNamespace)
			if err != nil {
				return err
			}
			_, err = hc.Status(secretsManagerReleaseName)
			return err
		}, 30*time.Second, 200*time.Millisecond).Should(Succeed())

		// envtest has no Deployment controller, so simulate the
		// upstream secrets-manager Deployment becoming Available.
		markDeploymentAvailable(secretsManagerDeploymentName)

		Eventually(func() metav1.ConditionStatus {
			return smReadyStatus(singletonName)
		}, 30*time.Second, 200*time.Millisecond).Should(Equal(metav1.ConditionTrue))

		got := &platformv1alpha1.SecretsManager{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: singletonName}, got)).To(Succeed())
		Expect(got.Status.Phase).To(Equal(platformv1alpha1.ComponentPhaseReady))
		Expect(got.Status.InstalledVersion).To(Equal(vendoredcharts.SecretsManagerChartVersion))
		Expect(got.Status.DeploymentRef).NotTo(BeNil())
		Expect(got.Status.DeploymentRef.Namespace).To(Equal(platformNamespace))
		Expect(got.Status.DeploymentRef.Name).To(Equal(secretsManagerDeploymentName))
	})

	It("labels the existing platform namespace privileged for Pod Security admission", func() {
		// The namespace already exists at a level that refuses
		// node-ca-injector's privileged container and hostPath volume,
		// as an administrator or an earlier install left it.
		stale := &corev1.Namespace{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: platformNamespace}, stale)).To(Succeed())
		stale.Labels = map[string]string{"pod-security.kubernetes.io/enforce": "baseline"}
		Expect(k8sClient.Update(ctx, stale)).To(Succeed())

		makeReadyClusterConfig()
		sm := &platformv1alpha1.SecretsManager{
			ObjectMeta: metav1.ObjectMeta{Name: singletonName},
			Spec:       platformv1alpha1.SecretsManagerSpec{},
		}
		Expect(k8sClient.Create(ctx, sm)).To(Succeed())

		Eventually(func() map[string]string {
			ns := &corev1.Namespace{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: platformNamespace}, ns); err != nil {
				return nil
			}
			return ns.Labels
		}, 30*time.Second, 200*time.Millisecond).Should(And(
			HaveKeyWithValue("pod-security.kubernetes.io/enforce", "privileged"),
			HaveKeyWithValue("pod-security.kubernetes.io/audit", "privileged"),
			HaveKeyWithValue("pod-security.kubernetes.io/warn", "privileged"),
			HaveKeyWithValue(ownership.ManagedByLabel, ownership.ManagedByValue),
		))
	})

	It("upgrades the subchart's stale CRDs, so a field the old schema lacked is kept", func() {
		chrt, err := vendoredcharts.SecretsManager()
		Expect(err).NotTo(HaveOccurred())
		writeStaleCRDs(chrt)

		makeReadyClusterConfig()
		sm := &platformv1alpha1.SecretsManager{
			ObjectMeta: metav1.ObjectMeta{Name: singletonName},
			Spec:       platformv1alpha1.SecretsManagerSpec{},
		}
		Expect(k8sClient.Create(ctx, sm)).To(Succeed())

		expectChartCRDsApplied(chrt)

		// The stale schema prunes a rule's targetSecret.labels; the chart's
		// declares it. The API server takes a moment to serve a changed
		// schema, so re-apply until the field survives the round trip.
		copier := &unstructured.Unstructured{}
		copier.SetAPIVersion("secrets.educates.dev/v1beta1")
		copier.SetKind("SecretCopier")
		copier.SetName("crd-upgrade-check")
		Expect(unstructured.SetNestedSlice(copier.Object, []any{map[string]any{
			"sourceSecret": map[string]any{"name": "source", "namespace": "default"},
			"targetSecret": map[string]any{"labels": map[string]any{"copied": "true"}},
		}}, "spec", "rules")).To(Succeed())
		DeferCleanup(func() { _ = k8sClient.Delete(ctx, copier.DeepCopy()) })

		Eventually(func(g Gomega) {
			g.Expect(k8sClient.Apply(ctx, client.ApplyConfigurationFromUnstructured(copier.DeepCopy()),
				client.FieldOwner("crd-upgrade-check"), client.ForceOwnership)).To(Succeed())

			got := &unstructured.Unstructured{}
			got.SetGroupVersionKind(copier.GroupVersionKind())
			g.Expect(k8sClient.Get(ctx, client.ObjectKey{Name: copier.GetName()}, got)).To(Succeed())
			rules, _, err := unstructured.NestedSlice(got.Object, "spec", "rules")
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(rules).To(HaveLen(1))
			labels, _, err := unstructured.NestedStringMap(rules[0].(map[string]any), "targetSecret", "labels")
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(labels).To(Equal(map[string]string{"copied": "true"}))
		}, 30*time.Second, 200*time.Millisecond).Should(Succeed())
	})

	It("uninstalls the chart on delete", func() {
		makeReadyClusterConfig()
		sm := &platformv1alpha1.SecretsManager{
			ObjectMeta: metav1.ObjectMeta{Name: singletonName},
			Spec:       platformv1alpha1.SecretsManagerSpec{},
		}
		Expect(k8sClient.Create(ctx, sm)).To(Succeed())

		// Drive Ready first so cleanup has something to undo.
		Eventually(func() error {
			hc, err := helmFac.For(platformNamespace)
			if err != nil {
				return err
			}
			_, err = hc.Status(secretsManagerReleaseName)
			return err
		}, 30*time.Second, 200*time.Millisecond).Should(Succeed())
		markDeploymentAvailable(secretsManagerDeploymentName)
		Eventually(func() metav1.ConditionStatus {
			return smReadyStatus(singletonName)
		}, 30*time.Second, 200*time.Millisecond).Should(Equal(metav1.ConditionTrue))

		// Trigger the finalizer drain.
		Expect(k8sClient.Delete(ctx, sm)).To(Succeed())

		// helm release should be gone shortly after.
		Eventually(func() error {
			hc, err := helmFac.For(platformNamespace)
			if err != nil {
				return err
			}
			_, statusErr := hc.Status(secretsManagerReleaseName)
			return statusErr
		}, 30*time.Second, 200*time.Millisecond).Should(MatchError(helm.ErrReleaseNotFound))

		// And the CR should be gone (finalizer removed).
		Eventually(func() bool {
			err := k8sClient.Get(ctx, types.NamespacedName{Name: singletonName}, &platformv1alpha1.SecretsManager{})
			return apierrors.IsNotFound(err)
		}, 30*time.Second, 200*time.Millisecond).Should(BeTrue())
	})
})
