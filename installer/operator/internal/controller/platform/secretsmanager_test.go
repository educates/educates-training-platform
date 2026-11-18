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

// setClusterConfigImageRegistry replaces the image registry the
// makeReadyClusterConfig fixture publishes in status.
func setClusterConfigImageRegistry(registry *configv1alpha1.ImageRegistry) {
	GinkgoHelper()
	Eventually(func() error {
		cc := &configv1alpha1.EducatesClusterConfig{}
		if err := k8sClient.Get(ctx, types.NamespacedName{Name: configSingletonName}, cc); err != nil {
			return err
		}
		cc.Status.ImageRegistry = registry
		return k8sClient.Status().Update(ctx, cc)
	}, 10*time.Second, 200*time.Millisecond).Should(Succeed())
}

// ingressCAChartRef is the clusterIngress.caCertificateRef chart value
// publishIngressCAOnClusterConfig's reference must render to.
func ingressCAChartRef() map[string]any {
	return map[string]any{"name": "educates-ingress-ca", "namespace": "educates-installer"}
}

// markDeploymentRolledOut creates (if missing) the named Deployment and
// gives it the status of a completed rollout: its spec observed, and
// its one replica updated and available. envtest has no controllers, so
// specs drive the transition manually.
func markDeploymentRolledOut(name string) {
	GinkgoHelper()
	setDeploymentStatus(name, rolledOutDeploymentStatus)
}

// markDeploymentRollingOut creates (if missing) the named Deployment and
// gives it the status it has right after a Helm upgrade changed its
// spec: the deployment controller hasn't observed the new spec yet, so
// the status still describes the previous one, rolled out and
// Available.
func markDeploymentRollingOut(name string) {
	GinkgoHelper()
	setDeploymentStatus(name, func(generation int64) appsv1.DeploymentStatus {
		return rolledOutDeploymentStatus(generation - 1)
	})
}

// rolledOutDeploymentStatus is the status of a one-replica Deployment
// that has rolled out the spec of observedGeneration.
func rolledOutDeploymentStatus(observedGeneration int64) appsv1.DeploymentStatus {
	return appsv1.DeploymentStatus{
		ObservedGeneration: observedGeneration,
		Replicas:           1,
		UpdatedReplicas:    1,
		ReadyReplicas:      1,
		AvailableReplicas:  1,
		Conditions: []appsv1.DeploymentCondition{{
			Type:   appsv1.DeploymentAvailable,
			Status: corev1.ConditionTrue,
		}},
	}
}

// markDaemonSetRolledOut creates (if missing) the named DaemonSet and
// gives it the status of a completed rollout on one node.
func markDaemonSetRolledOut(name string) {
	GinkgoHelper()
	setDaemonSetStatus(name, rolledOutDaemonSetStatus)
}

// markDaemonSetRollingOut creates (if missing) the named DaemonSet and
// gives it the status it has right after a Helm upgrade changed its
// spec: the status still describes the previous spec, with its pod
// ready and available.
func markDaemonSetRollingOut(name string) {
	GinkgoHelper()
	setDaemonSetStatus(name, func(generation int64) appsv1.DaemonSetStatus {
		return rolledOutDaemonSetStatus(generation - 1)
	})
}

// rolledOutDaemonSetStatus is the status of a DaemonSet on one node that
// has rolled out the spec of observedGeneration.
func rolledOutDaemonSetStatus(observedGeneration int64) appsv1.DaemonSetStatus {
	return appsv1.DaemonSetStatus{
		ObservedGeneration:     observedGeneration,
		DesiredNumberScheduled: 1,
		CurrentNumberScheduled: 1,
		UpdatedNumberScheduled: 1,
		NumberReady:            1,
		NumberAvailable:        1,
	}
}

// setDaemonSetStatus creates (if missing) the named DaemonSet in the
// platform namespace and sets its status to what status returns for the
// DaemonSet's generation.
func setDaemonSetStatus(name string, status func(generation int64) appsv1.DaemonSetStatus) {
	GinkgoHelper()
	ds := &appsv1.DaemonSet{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: platformNamespace},
		Spec: appsv1.DaemonSetSpec{
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
	err := k8sClient.Create(ctx, ds)
	if err != nil && !apierrors.IsAlreadyExists(err) {
		Expect(err).NotTo(HaveOccurred())
	}
	Expect(k8sClient.Get(ctx, types.NamespacedName{Namespace: platformNamespace, Name: name}, ds)).To(Succeed())
	ds.Status = status(ds.Generation)
	Expect(k8sClient.Status().Update(ctx, ds)).To(Succeed())
}

// markNodeNotReadyReady turns the node addDaemonSetPodOnNodeNotReady made
// Ready again, its pod still unavailable: a change only the Node watch
// reports, since the DaemonSet's status stays as it was.
func markNodeNotReadyReady() {
	GinkgoHelper()
	node := &corev1.Node{}
	Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "node-not-ready"}, node)).To(Succeed())
	node.Status.Conditions = []corev1.NodeCondition{{Type: corev1.NodeReady, Status: corev1.ConditionTrue}}
	Expect(k8sClient.Status().Update(ctx, node)).To(Succeed())
}

// addDaemonSetPodOnNodeNotReady stands in for a node that has gone not
// Ready: it creates a Node whose Ready condition is False, carrying an
// unavailable pod of the named DaemonSet in the platform namespace, and
// removes both when the spec ends. envtest runs no kubelet or node
// controller.
func addDaemonSetPodOnNodeNotReady(name string) {
	GinkgoHelper()
	nodeName := "node-not-ready"
	node := &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: nodeName}}
	Expect(k8sClient.Create(ctx, node)).To(Succeed())
	node.Status.Conditions = []corev1.NodeCondition{{Type: corev1.NodeReady, Status: corev1.ConditionFalse}}
	Expect(k8sClient.Status().Update(ctx, node)).To(Succeed())
	DeferCleanup(func() { _ = k8sClient.Delete(ctx, &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: nodeName}}) })

	ds := &appsv1.DaemonSet{}
	Expect(k8sClient.Get(ctx, types.NamespacedName{Namespace: platformNamespace, Name: name}, ds)).To(Succeed())
	controller := true
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name: name + "-on-" + nodeName, Namespace: platformNamespace,
			Labels: ds.Spec.Selector.MatchLabels,
			OwnerReferences: []metav1.OwnerReference{{
				APIVersion: "apps/v1", Kind: "DaemonSet", Name: ds.Name, UID: ds.UID, Controller: &controller,
			}},
		},
		Spec: corev1.PodSpec{
			NodeName:   nodeName,
			Containers: []corev1.Container{{Name: "c", Image: "stub:latest"}},
		},
	}
	Expect(k8sClient.Create(ctx, pod)).To(Succeed())
	pod.Status.Conditions = []corev1.PodCondition{{Type: corev1.PodReady, Status: corev1.ConditionFalse}}
	Expect(k8sClient.Status().Update(ctx, pod)).To(Succeed())
	DeferCleanup(func() {
		_ = k8sClient.Delete(ctx, &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: pod.Name, Namespace: platformNamespace}},
			client.GracePeriodSeconds(0))
	})
}

// setDeploymentStatus creates (if missing) the named Deployment in the
// platform namespace and sets its status to what status returns for
// the Deployment's generation.
func setDeploymentStatus(name string, status func(generation int64) appsv1.DeploymentStatus) {
	GinkgoHelper()
	namespace := platformNamespace
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
	dep.Status = status(dep.Generation)
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

func smReadyStatus() metav1.ConditionStatus {
	got := &platformv1alpha1.SecretsManager{}
	if err := k8sClient.Get(ctx, types.NamespacedName{Name: singletonName}, got); err != nil {
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
			APIReader:     mgr.GetAPIReader(),
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

		Expect(smReadyStatus()).To(Equal(metav1.ConditionFalse))
	})

	It("installs the chart and reaches Ready=True when the Deployment has rolled out", func() {
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
		// upstream secrets-manager Deployment rolling out.
		markDeploymentRolledOut(secretsManagerDeploymentName)

		Eventually(func() metav1.ConditionStatus {
			return smReadyStatus()
		}, 30*time.Second, 200*time.Millisecond).Should(Equal(metav1.ConditionTrue))

		got := &platformv1alpha1.SecretsManager{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: singletonName}, got)).To(Succeed())
		Expect(got.Status.Phase).To(Equal(platformv1alpha1.ComponentPhaseReady))
		Expect(got.Status.InstalledVersion).To(Equal(vendoredcharts.SecretsManagerChartVersion))
		Expect(got.Status.DeploymentRef).NotTo(BeNil())
		Expect(got.Status.DeploymentRef.Namespace).To(Equal(platformNamespace))
		Expect(got.Status.DeploymentRef.Name).To(Equal(secretsManagerDeploymentName))
	})

	It("keeps Ready=False until the Deployment has rolled out its spec", func() {
		makeReadyClusterConfig()

		sm := &platformv1alpha1.SecretsManager{
			ObjectMeta: metav1.ObjectMeta{Name: singletonName},
			Spec:       platformv1alpha1.SecretsManagerSpec{},
		}
		Expect(k8sClient.Create(ctx, sm)).To(Succeed())
		Eventually(func() error {
			hc, err := helmFac.For(platformNamespace)
			if err != nil {
				return err
			}
			_, err = hc.Status(secretsManagerReleaseName)
			return err
		}, 30*time.Second, 200*time.Millisecond).Should(Succeed())

		// Available=True here describes the replica of the previous
		// spec, which the rollout is about to replace.
		markDeploymentRollingOut(secretsManagerDeploymentName)
		Consistently(func() metav1.ConditionStatus {
			return smReadyStatus()
		}, 3*time.Second, 200*time.Millisecond).ShouldNot(Equal(metav1.ConditionTrue))

		markDeploymentRolledOut(secretsManagerDeploymentName)
		Eventually(func() metav1.ConditionStatus {
			return smReadyStatus()
		}, 30*time.Second, 200*time.Millisecond).Should(Equal(metav1.ConditionTrue))
	})

	// A changed or removed spec.imageRegistry reaches the runtime only
	// through the cluster config's status, so the release must follow it.
	It("follows the cluster config's image registry into the release when it is set and removed", func() {
		makeReadyClusterConfig()
		Expect(k8sClient.Create(ctx, &platformv1alpha1.SecretsManager{
			ObjectMeta: metav1.ObjectMeta{Name: singletonName},
		})).To(Succeed())

		releaseConfig := func() map[string]any {
			hc, err := helmFac.For(platformNamespace)
			if err != nil {
				return nil
			}
			rel, err := hc.Status(secretsManagerReleaseName)
			if err != nil {
				return nil
			}
			return rel.Config
		}
		Eventually(releaseConfig, 30*time.Second, 200*time.Millisecond).ShouldNot(BeNil())
		Expect(releaseConfig()).NotTo(HaveKey("development"))
		Expect(releaseConfig()).NotTo(HaveKey("imagePullSecrets"))

		setClusterConfigImageRegistry(&configv1alpha1.ImageRegistry{
			Prefix:      "mirror.example.com/educates",
			PullSecrets: []configv1alpha1.LocalObjectReference{{Name: "mirror-pull"}},
		})
		Eventually(releaseConfig, 30*time.Second, 200*time.Millisecond).Should(And(
			HaveKey("development"),
			HaveKeyWithValue("imagePullSecrets", []any{map[string]any{"name": "mirror-pull"}})))

		setClusterConfigImageRegistry(&configv1alpha1.ImageRegistry{})
		Eventually(releaseConfig, 30*time.Second, 200*time.Millisecond).Should(And(
			Not(HaveKey("development")),
			Not(HaveKey("imagePullSecrets"))))
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
		markDeploymentRolledOut(secretsManagerDeploymentName)
		Eventually(func() metav1.ConditionStatus {
			return smReadyStatus()
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
