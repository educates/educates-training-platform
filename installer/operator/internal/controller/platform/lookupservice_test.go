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
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	release "helm.sh/helm/v4/pkg/release/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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

func lsReadyStatus(name string) metav1.ConditionStatus {
	got := &platformv1alpha1.LookupService{}
	if err := k8sClient.Get(ctx, types.NamespacedName{Name: name}, got); err != nil {
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
	ensureNamespace("educates-config")
	Eventually(func() error {
		ns := &corev1.Namespace{}
		if err := k8sClient.Get(ctx, types.NamespacedName{Name: "educates-config"}, ns); err != nil {
			return err
		}
		ns.Labels = map[string]string{"kubernetes.io/metadata.name": "educates-config"}
		for k, v := range labels {
			ns.Labels[k] = v
		}
		ns.Annotations = annotations
		return k8sClient.Update(ctx, ns)
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

		Expect(lsReadyStatus(singletonName)).To(Equal(metav1.ConditionFalse))
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

		markDeploymentAvailable(lookupServiceDeploymentName)

		Eventually(func() metav1.ConditionStatus {
			return lsReadyStatus(singletonName)
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

	It("lets the release take over an educates-config namespace created by hand, keeping its configurations", func() {
		// envtest has no namespace controller, so a deleted namespace
		// never goes: the namespace is reset to how a hand-made one
		// looks instead.
		setConfigNamespaceMetadata(nil, nil)
		kubeconfig := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: "kubeconfig-cluster-1", Namespace: "educates-config"},
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
			g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "educates-config"}, ns)).To(Succeed())
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
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "educates-config"}, ns)).To(Succeed())
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
		markDeploymentAvailable(lookupServiceDeploymentName)
		Eventually(func() metav1.ConditionStatus {
			return lsReadyStatus(singletonName)
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
})
