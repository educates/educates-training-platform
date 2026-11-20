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
	"maps"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	release "helm.sh/helm/v4/pkg/release/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	crconfig "sigs.k8s.io/controller-runtime/pkg/config"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	configv1alpha1 "github.com/educates/educates-training-platform/installer/operator/api/config/v1alpha1"
	platformv1alpha1 "github.com/educates/educates-training-platform/installer/operator/api/platform/v1alpha1"
	"github.com/educates/educates-training-platform/installer/operator/internal/helm"
	vendoredcharts "github.com/educates/educates-training-platform/installer/operator/vendored-charts"
)

// lsReadyStatus reports the Ready condition of the LookupService singleton.
func lsReadyStatus() metav1.ConditionStatus {
	got := &platformv1alpha1.LookupService{}
	if err := k8sClient.Get(ctx, types.NamespacedName{Name: singletonName}, got); err != nil {
		return metav1.ConditionUnknown
	}
	c := meta.FindStatusCondition(got.Status.Conditions, conditionReady)
	if c == nil {
		return metav1.ConditionUnknown
	}
	return c.Status
}

// setConfigNamespaceMetadata makes the educates-config namespace exist
// with exactly the given labels and annotations.
func setConfigNamespaceMetadata(labels, annotations map[string]string) {
	GinkgoHelper()
	ensureNamespace(lookupServiceConfigNamespace)
	Eventually(func() error {
		ns := &corev1.Namespace{}
		if err := k8sClient.Get(ctx, types.NamespacedName{Name: lookupServiceConfigNamespace}, ns); err != nil {
			return err
		}
		ns.Labels = map[string]string{"kubernetes.io/metadata.name": lookupServiceConfigNamespace}
		maps.Copy(ns.Labels, labels)
		ns.Annotations = annotations
		return k8sClient.Update(ctx, ns)
	}, 10*time.Second, 200*time.Millisecond).Should(Succeed())
}

// removeNamespace deletes a namespace for good. envtest runs no namespace
// controller, so the spec finalizes the namespace itself, as that
// controller does once the namespace is empty, and deletes it again.
func removeNamespace(name string) {
	GinkgoHelper()
	key := types.NamespacedName{Name: name}
	Eventually(func(g Gomega) {
		ns := &corev1.Namespace{}
		err := k8sClient.Get(ctx, key, ns)
		if apierrors.IsNotFound(err) {
			return
		}
		g.Expect(err).NotTo(HaveOccurred())
		switch {
		case ns.DeletionTimestamp.IsZero():
			g.Expect(k8sClient.Delete(ctx, ns)).To(Succeed())
		case len(ns.Spec.Finalizers) > 0:
			ns.Spec.Finalizers = nil
			g.Expect(k8sClient.SubResource("finalize").Update(ctx, ns)).To(Succeed())
		default:
			g.Expect(client.IgnoreNotFound(k8sClient.Delete(ctx, ns))).To(Succeed())
		}
		g.Expect(apierrors.IsNotFound(k8sClient.Get(ctx, key, &corev1.Namespace{}))).To(BeTrue(),
			"namespace %s still exists", name)
	}, 30*time.Second, 200*time.Millisecond).Should(Succeed())
}

// servePlainHTTPOnClusterConfig makes the makeReadyClusterConfig fixture
// publish the ingress of a plain HTTP install, as the None certificates
// provider publishes it: the http protocol and no wildcard certificate.
func servePlainHTTPOnClusterConfig() {
	GinkgoHelper()
	Eventually(func() error {
		cc := &configv1alpha1.EducatesClusterConfig{}
		if err := k8sClient.Get(ctx, types.NamespacedName{Name: configSingletonName}, cc); err != nil {
			return err
		}
		cc.Status.Ingress.Protocol = configv1alpha1.IngressProtocolHTTP
		cc.Status.Ingress.WildcardCertificateSecretRef = nil
		return k8sClient.Status().Update(ctx, cc)
	}, 10*time.Second, 200*time.Millisecond).Should(Succeed())
}

// lookupServiceReadyWithURL creates the LookupService singleton with the
// prefix lookup, marks its Deployment rolled out once its release exists,
// and returns the status.url it publishes on reaching Ready.
func lookupServiceReadyWithURL(helmFac *memoryHelmFactory) string {
	GinkgoHelper()
	Expect(k8sClient.Create(ctx, &platformv1alpha1.LookupService{
		ObjectMeta: metav1.ObjectMeta{Name: singletonName},
		Spec: platformv1alpha1.LookupServiceSpec{
			Ingress: platformv1alpha1.LookupServiceIngress{Prefix: "lookup"},
		},
	})).To(Succeed())
	Eventually(func() error {
		hc, err := helmFac.For(platformNamespace)
		if err != nil {
			return err
		}
		_, err = hc.Status(lookupServiceReleaseName)
		return err
	}, 30*time.Second, 200*time.Millisecond).Should(Succeed())
	markDeploymentRolledOut(lookupServiceDeploymentName)
	Eventually(func() metav1.ConditionStatus {
		return lsReadyStatus()
	}, 30*time.Second, 200*time.Millisecond).Should(Equal(metav1.ConditionTrue))

	got := &platformv1alpha1.LookupService{}
	Expect(k8sClient.Get(ctx, types.NamespacedName{Name: singletonName}, got)).To(Succeed())
	return got.Status.URL
}

// installLookupService creates the LookupService singleton with its gates
// open and waits for its release, and with it lookup-service's CRDs.
func installLookupService(helmFac *memoryHelmFactory) *platformv1alpha1.LookupService {
	GinkgoHelper()
	makeReadyClusterConfig()
	makeReadySecretsManager()
	ls := &platformv1alpha1.LookupService{
		ObjectMeta: metav1.ObjectMeta{Name: singletonName},
		Spec: platformv1alpha1.LookupServiceSpec{
			Ingress: platformv1alpha1.LookupServiceIngress{Prefix: "lookup"},
		},
	}
	Expect(k8sClient.Create(ctx, ls)).To(Succeed())
	Eventually(func() error {
		hc, err := helmFac.For(platformNamespace)
		if err != nil {
			return err
		}
		_, err = hc.Status(lookupServiceReleaseName)
		return err
	}, 30*time.Second, 200*time.Millisecond).Should(Succeed())
	return ls
}

// deleteLookupService deletes the LookupService singleton and waits for its
// teardown to remove the reconciler's finalizer.
func deleteLookupService(ls *platformv1alpha1.LookupService) {
	GinkgoHelper()
	Expect(k8sClient.Delete(ctx, ls)).To(Succeed())
	Eventually(func() bool {
		err := k8sClient.Get(ctx, types.NamespacedName{Name: singletonName}, &platformv1alpha1.LookupService{})
		return apierrors.IsNotFound(err)
	}, 30*time.Second, 200*time.Millisecond).Should(BeTrue())
}

// lookupConfiguration returns a lookup-service configuration of the given
// kind in its configuration namespace, carrying the given finalizers.
func lookupConfiguration(kind, name string, finalizers ...string) *unstructured.Unstructured {
	obj := &unstructured.Unstructured{}
	obj.SetGroupVersionKind(schema.GroupVersionKind{Group: "lookup.educates.dev", Version: "v1beta1", Kind: kind})
	obj.SetNamespace(lookupServiceConfigNamespace)
	obj.SetName(name)
	obj.SetFinalizers(finalizers)
	return obj
}

// removeLookupConfiguration deletes a configuration whatever finalizers it
// still carries.
func removeLookupConfiguration(obj *unstructured.Unstructured) {
	GinkgoHelper()
	Eventually(func() error {
		live := &unstructured.Unstructured{}
		live.SetGroupVersionKind(obj.GroupVersionKind())
		if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(obj), live); err != nil {
			return client.IgnoreNotFound(err)
		}
		live.SetFinalizers(nil)
		if err := k8sClient.Update(ctx, live); err != nil {
			return client.IgnoreNotFound(err)
		}
		return client.IgnoreNotFound(k8sClient.Delete(ctx, live))
	}, 10*time.Second, 200*time.Millisecond).Should(Succeed())
}

var _ = Describe("LookupService reconciler", func() {
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

		Expect((&LookupServiceReconciler{
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
		ls := &platformv1alpha1.LookupService{}
		if err := k8sClient.Get(ctx, types.NamespacedName{Name: singletonName}, ls); err == nil {
			ls.Finalizers = nil
			_ = k8sClient.Update(ctx, ls)
			_ = k8sClient.Delete(ctx, ls)
		}
		_ = k8sClient.DeleteAllOf(ctx, &platformv1alpha1.SecretsManager{})
		_ = k8sClient.DeleteAllOf(ctx, &configv1alpha1.EducatesClusterConfig{})
		_ = k8sClient.DeleteAllOf(ctx, &appsv1.Deployment{}, client.InNamespace(platformNamespace))
	})

	It("refuses to proceed when EducatesClusterConfig is missing", func() {
		ls := &platformv1alpha1.LookupService{
			ObjectMeta: metav1.ObjectMeta{Name: singletonName},
			Spec: platformv1alpha1.LookupServiceSpec{
				Ingress: platformv1alpha1.LookupServiceIngress{Prefix: "lookup"},
			},
		}
		Expect(k8sClient.Create(ctx, ls)).To(Succeed())

		Eventually(func() string {
			got := &platformv1alpha1.LookupService{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: singletonName}, got); err != nil {
				return ""
			}
			c := meta.FindStatusCondition(got.Status.Conditions, conditionClusterConfigAvailable)
			if c == nil {
				return ""
			}
			return c.Reason
		}, 30*time.Second, 200*time.Millisecond).Should(Equal("ClusterConfigNotReady"))

		Expect(lsReadyStatus()).To(Equal(metav1.ConditionFalse))
	})

	It("publishes an https status.url on an HTTPS install", func() {
		makeReadyClusterConfig()
		makeReadySecretsManager()

		Expect(lookupServiceReadyWithURL(helmFac)).To(Equal("https://lookup.test.example.com"))
	})

	It("publishes an http status.url on a plain HTTP install", func() {
		makeReadyClusterConfig()
		servePlainHTTPOnClusterConfig()
		makeReadySecretsManager()

		Expect(lookupServiceReadyWithURL(helmFac)).To(Equal("http://lookup.test.example.com"))
	})

	It("installs the chart, derives status.url, and reaches Ready", func() {
		makeReadyClusterConfig()
		makeReadySecretsManager()

		ls := &platformv1alpha1.LookupService{
			ObjectMeta: metav1.ObjectMeta{Name: singletonName},
			Spec: platformv1alpha1.LookupServiceSpec{
				Ingress: platformv1alpha1.LookupServiceIngress{Prefix: "lookup"},
			},
		}
		Expect(k8sClient.Create(ctx, ls)).To(Succeed())

		Eventually(func() error {
			hc, err := helmFac.For(platformNamespace)
			if err != nil {
				return err
			}
			_, err = hc.Status(lookupServiceReleaseName)
			return err
		}, 30*time.Second, 200*time.Millisecond).Should(Succeed())

		markDeploymentRolledOut(lookupServiceDeploymentName)

		Eventually(func() metav1.ConditionStatus {
			return lsReadyStatus()
		}, 30*time.Second, 200*time.Millisecond).Should(Equal(metav1.ConditionTrue))

		got := &platformv1alpha1.LookupService{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: singletonName}, got)).To(Succeed())
		Expect(got.Status.Phase).To(Equal(platformv1alpha1.ComponentPhaseReady))
		Expect(got.Status.InstalledVersion).To(Equal(vendoredcharts.LookupServiceChartVersion))
		Expect(got.Status.URL).To(Equal("https://lookup.test.example.com"))
		Expect(got.Status.DeploymentRef).NotTo(BeNil())
		Expect(got.Status.DeploymentRef.Namespace).To(Equal(platformNamespace))
		Expect(got.Status.DeploymentRef.Name).To(Equal(lookupServiceDeploymentName))
	})

	It("keeps Ready=False until the Deployment has rolled out its spec", func() {
		makeReadyClusterConfig()
		makeReadySecretsManager()

		ls := &platformv1alpha1.LookupService{
			ObjectMeta: metav1.ObjectMeta{Name: singletonName},
			Spec: platformv1alpha1.LookupServiceSpec{
				Ingress: platformv1alpha1.LookupServiceIngress{Prefix: "lookup"},
			},
		}
		Expect(k8sClient.Create(ctx, ls)).To(Succeed())
		Eventually(func() error {
			hc, err := helmFac.For(platformNamespace)
			if err != nil {
				return err
			}
			_, err = hc.Status(lookupServiceReleaseName)
			return err
		}, 30*time.Second, 200*time.Millisecond).Should(Succeed())

		// Available=True here describes the replica of the previous
		// spec, which the rollout is about to replace.
		markDeploymentRollingOut(lookupServiceDeploymentName)
		Consistently(func() metav1.ConditionStatus {
			return lsReadyStatus()
		}, 3*time.Second, 200*time.Millisecond).ShouldNot(Equal(metav1.ConditionTrue))

		markDeploymentRolledOut(lookupServiceDeploymentName)
		Eventually(func() metav1.ConditionStatus {
			return lsReadyStatus()
		}, 30*time.Second, 200*time.Millisecond).Should(Equal(metav1.ConditionTrue))
	})

	It("waits for SecretsManager to run the embedded secrets-manager chart before installing", func() {
		// lookup-service's SecretCopier resources are written against the
		// secrets.educates.dev CRDs, which reach the new release's schemas
		// only once the secrets-manager release converges after an
		// operator upgrade.
		makeReadyClusterConfig()
		makeReadySecretsManagerAt("0.0.0-previous")

		ls := &platformv1alpha1.LookupService{
			ObjectMeta: metav1.ObjectMeta{Name: singletonName},
			Spec: platformv1alpha1.LookupServiceSpec{
				Ingress: platformv1alpha1.LookupServiceIngress{Prefix: "lookup"},
			},
		}
		Expect(k8sClient.Create(ctx, ls)).To(Succeed())

		Eventually(func(g Gomega) {
			got := &platformv1alpha1.LookupService{}
			g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: singletonName}, got)).To(Succeed())
			c := meta.FindStatusCondition(got.Status.Conditions, conditionReady)
			g.Expect(c).NotTo(BeNil())
			g.Expect(c.Reason).To(Equal("WaitingForSecretsManager"))
			g.Expect(c.Message).To(ContainSubstring("0.0.0-previous"))
		}, 30*time.Second, 200*time.Millisecond).Should(Succeed())
		hc, err := helmFac.For(platformNamespace)
		Expect(err).NotTo(HaveOccurred())
		_, err = hc.Status(lookupServiceReleaseName)
		Expect(err).To(MatchError(helm.ErrReleaseNotFound))

		setSecretsManagerInstalledVersion(vendoredcharts.SecretsManagerChartVersion)

		Eventually(func() error {
			_, err := hc.Status(lookupServiceReleaseName)
			return err
		}, 30*time.Second, 200*time.Millisecond).Should(Succeed())
	})

	It("upgrades the subchart's stale CRDs to the chart's schemas", func() {
		chrt, err := vendoredcharts.LookupService()
		Expect(err).NotTo(HaveOccurred())
		writeStaleCRDs(chrt)

		makeReadyClusterConfig()
		makeReadySecretsManager()
		ls := &platformv1alpha1.LookupService{
			ObjectMeta: metav1.ObjectMeta{Name: singletonName},
			Spec: platformv1alpha1.LookupServiceSpec{
				Ingress: platformv1alpha1.LookupServiceIngress{Prefix: "lookup"},
			},
		}
		Expect(k8sClient.Create(ctx, ls)).To(Succeed())

		expectChartCRDsApplied(chrt)
	})

	It("carries the cluster's CA trust Secret, with its namespace, into chart values", func() {
		makeReadyClusterConfig()
		makeReadySecretsManager()
		publishIngressCAOnClusterConfig()

		Expect(k8sClient.Create(ctx, &platformv1alpha1.LookupService{
			ObjectMeta: metav1.ObjectMeta{Name: singletonName},
			Spec: platformv1alpha1.LookupServiceSpec{
				Ingress: platformv1alpha1.LookupServiceIngress{Prefix: "lookup"},
			},
		})).To(Succeed())

		var rel *release.Release
		Eventually(func() error {
			hc, err := helmFac.For(platformNamespace)
			if err != nil {
				return err
			}
			rel, err = hc.Status(lookupServiceReleaseName)
			return err
		}, 30*time.Second, 200*time.Millisecond).Should(Succeed())

		ci, ok := rel.Config["clusterIngress"].(map[string]any)
		Expect(ok).To(BeTrue(), "clusterIngress missing from rendered values")
		Expect(ci["caCertificateRef"]).To(Equal(ingressCAChartRef()))
	})

	It("passes spec.logLevel to the chart's log level, following a change", func() {
		helmRelease := func() (*release.Release, error) {
			hc, err := helmFac.For(platformNamespace)
			if err != nil {
				return nil, err
			}
			return hc.Status(lookupServiceReleaseName)
		}
		releaseLogLevel := func() any {
			rel, err := helmRelease()
			if err != nil {
				return err
			}
			return rel.Config["logLevel"]
		}

		ls := installLookupService(helmFac)
		Eventually(releaseLogLevel, 30*time.Second, 200*time.Millisecond).Should(Equal("info"))

		Eventually(func() error {
			live := &platformv1alpha1.LookupService{}
			if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(ls), live); err != nil {
				return err
			}
			live.Spec.LogLevel = platformv1alpha1.LogLevelDebug
			return k8sClient.Update(ctx, live)
		}, 10*time.Second, 200*time.Millisecond).Should(Succeed())

		Eventually(releaseLogLevel, 30*time.Second, 200*time.Millisecond).Should(Equal("debug"))
	})

	It("lets the release take over an educates-config namespace created by hand, keeping its configurations", func() {
		// envtest has no namespace controller, so a deleted namespace
		// never goes: the namespace is reset to how a hand-made one
		// looks instead.
		setConfigNamespaceMetadata(nil, nil)
		kubeconfig := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: "kubeconfig-cluster-1", Namespace: lookupServiceConfigNamespace},
			StringData: map[string]string{"config": "kubeconfig"},
		}
		if err := k8sClient.Create(ctx, kubeconfig); err != nil && !apierrors.IsAlreadyExists(err) {
			Expect(err).NotTo(HaveOccurred())
		}

		makeReadyClusterConfig()
		makeReadySecretsManager()
		Expect(k8sClient.Create(ctx, &platformv1alpha1.LookupService{
			ObjectMeta: metav1.ObjectMeta{Name: singletonName},
			Spec: platformv1alpha1.LookupServiceSpec{
				Ingress: platformv1alpha1.LookupServiceIngress{Prefix: "lookup"},
			},
		})).To(Succeed())

		Eventually(func(g Gomega) {
			ns := &corev1.Namespace{}
			g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: lookupServiceConfigNamespace}, ns)).To(Succeed())
			g.Expect(ns.Labels).To(HaveKeyWithValue("app.kubernetes.io/managed-by", "Helm"))
			g.Expect(ns.Annotations).To(HaveKeyWithValue("meta.helm.sh/release-name", lookupServiceReleaseName))
			g.Expect(ns.Annotations).To(HaveKeyWithValue("meta.helm.sh/release-namespace", platformNamespace))
		}, 30*time.Second, 200*time.Millisecond).Should(Succeed())

		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(kubeconfig), &corev1.Secret{})).To(Succeed())
	})

	It("leaves an educates-config namespace another Helm release owns to Helm", func() {
		otherRelease := map[string]string{
			"meta.helm.sh/release-name":      "other",
			"meta.helm.sh/release-namespace": "elsewhere",
		}
		setConfigNamespaceMetadata(map[string]string{"app.kubernetes.io/managed-by": "Helm"}, otherRelease)

		makeReadyClusterConfig()
		makeReadySecretsManager()
		Expect(k8sClient.Create(ctx, &platformv1alpha1.LookupService{
			ObjectMeta: metav1.ObjectMeta{Name: singletonName},
			Spec: platformv1alpha1.LookupServiceSpec{
				Ingress: platformv1alpha1.LookupServiceIngress{Prefix: "lookup"},
			},
		})).To(Succeed())

		// The memory Helm client installs without checking ownership; the
		// release existing shows the reconciler got past the namespace.
		Eventually(func() error {
			hc, err := helmFac.For(platformNamespace)
			if err != nil {
				return err
			}
			_, err = hc.Status(lookupServiceReleaseName)
			return err
		}, 30*time.Second, 200*time.Millisecond).Should(Succeed())

		ns := &corev1.Namespace{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: lookupServiceConfigNamespace}, ns)).To(Succeed())
		Expect(ns.Annotations).To(HaveKeyWithValue("meta.helm.sh/release-name", "other"))
		Expect(ns.Annotations).To(HaveKeyWithValue("meta.helm.sh/release-namespace", "elsewhere"))
	})

	It("uninstalls the chart on delete", func() {
		makeReadyClusterConfig()
		makeReadySecretsManager()
		ls := &platformv1alpha1.LookupService{
			ObjectMeta: metav1.ObjectMeta{Name: singletonName},
			Spec: platformv1alpha1.LookupServiceSpec{
				Ingress: platformv1alpha1.LookupServiceIngress{Prefix: "lookup"},
			},
		}
		Expect(k8sClient.Create(ctx, ls)).To(Succeed())

		Eventually(func() error {
			hc, err := helmFac.For(platformNamespace)
			if err != nil {
				return err
			}
			_, err = hc.Status(lookupServiceReleaseName)
			return err
		}, 30*time.Second, 200*time.Millisecond).Should(Succeed())
		markDeploymentRolledOut(lookupServiceDeploymentName)
		Eventually(func() metav1.ConditionStatus {
			return lsReadyStatus()
		}, 30*time.Second, 200*time.Millisecond).Should(Equal(metav1.ConditionTrue))

		Expect(k8sClient.Delete(ctx, ls)).To(Succeed())

		Eventually(func() error {
			hc, err := helmFac.For(platformNamespace)
			if err != nil {
				return err
			}
			_, statusErr := hc.Status(lookupServiceReleaseName)
			return statusErr
		}, 30*time.Second, 200*time.Millisecond).Should(MatchError(helm.ErrReleaseNotFound))

		Eventually(func() bool {
			err := k8sClient.Get(ctx, types.NamespacedName{Name: singletonName}, &platformv1alpha1.LookupService{})
			return apierrors.IsNotFound(err)
		}, 30*time.Second, 200*time.Millisecond).Should(BeTrue())
	})

	It("removes kopf's finalizer from lookup-service's configurations on delete, keeping other finalizers", func() {
		const kopfFinalizer = "kopf.zalando.org/KopfFinalizerMarker"
		const otherFinalizer = "example.com/keep"

		ls := installLookupService(helmFac)
		ensureNamespace(lookupServiceConfigNamespace)
		configurations := []*unstructured.Unstructured{
			lookupConfiguration("ClusterConfig", "finalizers-cluster", kopfFinalizer),
			lookupConfiguration("ClientConfig", "finalizers-client", kopfFinalizer, otherFinalizer),
			lookupConfiguration("TenantConfig", "finalizers-tenant", kopfFinalizer),
		}
		for _, obj := range configurations {
			// The CRDs the release applied may not be established yet.
			Eventually(func() error {
				return k8sClient.Create(ctx, obj.DeepCopy())
			}, 30*time.Second, 200*time.Millisecond).Should(Succeed())
			DeferCleanup(removeLookupConfiguration, obj)
		}

		deleteLookupService(ls)

		for _, obj := range configurations {
			live := &unstructured.Unstructured{}
			live.SetGroupVersionKind(obj.GroupVersionKind())
			Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(obj), live)).To(Succeed())
			Expect(live.GetFinalizers()).NotTo(ContainElement(kopfFinalizer), "%s %s", obj.GetKind(), obj.GetName())
			if obj.GetKind() == "ClientConfig" {
				Expect(live.GetFinalizers()).To(Equal([]string{otherFinalizer}))
			}
		}
	})

	It("finishes teardown when lookup-service's configuration CRDs are absent", func() {
		ls := installLookupService(helmFac)
		chrt, err := vendoredcharts.LookupService()
		Expect(err).NotTo(HaveOccurred())
		for _, crd := range chartCRDs(chrt) {
			Expect(client.IgnoreNotFound(k8sClient.Delete(ctx, crd))).To(Succeed())
			Eventually(func() bool {
				err := k8sClient.Get(ctx, client.ObjectKeyFromObject(crd), &apiextensionsv1.CustomResourceDefinition{})
				return apierrors.IsNotFound(err)
			}, 30*time.Second, 200*time.Millisecond).Should(BeTrue(), "CRD %s", crd.Name)
		}

		deleteLookupService(ls)
	})

	It("finishes teardown when the educates-config namespace is absent", func() {
		removeNamespace(lookupServiceConfigNamespace)
		ls := installLookupService(helmFac)
		// The memory Helm client creates none of the chart's resources.
		err := k8sClient.Get(ctx, types.NamespacedName{Name: lookupServiceConfigNamespace}, &corev1.Namespace{})
		Expect(apierrors.IsNotFound(err)).To(BeTrue())

		deleteLookupService(ls)
	})
})
