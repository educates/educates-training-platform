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
	"path/filepath"
	"sync"
	"time"

	cmv1 "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"
	cmmeta "github.com/cert-manager/cert-manager/pkg/apis/meta/v1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	releasecommon "helm.sh/helm/v4/pkg/release/common"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	crconfig "sigs.k8s.io/controller-runtime/pkg/config"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	configv1alpha1 "github.com/educates/educates-training-platform/installer/operator/api/config/v1alpha1"
	platformv1alpha1 "github.com/educates/educates-training-platform/installer/operator/api/platform/v1alpha1"
	"github.com/educates/educates-training-platform/installer/operator/internal/helm"
	vendoredcharts "github.com/educates/educates-training-platform/installer/operator/vendored-charts"
)

// validManagedSpec returns a minimal Managed-mode spec that satisfies
// Managed-mode validation: BundledContour + BundledCertManager
// with a CustomCA issuer. Used by every spec in this file as the
// shared happy-path starting point.
func validManagedSpec() configv1alpha1.EducatesClusterConfigSpec {
	return configv1alpha1.EducatesClusterConfigSpec{
		Mode: configv1alpha1.ClusterConfigModeManaged,
		Ingress: &configv1alpha1.Ingress{
			Domain:           "educates.test",
			IngressClassName: "contour",
			Controller: configv1alpha1.IngressController{
				Provider: configv1alpha1.IngressControllerProviderBundledContour,
			},
			Certificates: configv1alpha1.Certificates{
				Provider: configv1alpha1.CertificatesProviderBundledCertManager,
				BundledCertManager: &configv1alpha1.BundledCertManagerConfig{
					IssuerType: configv1alpha1.IssuerTypeCustomCA,
					CustomCA: &configv1alpha1.CustomCAConfig{
						CACertificateRef: configv1alpha1.CASecretReference{
							Name: "custom-ca",
						},
					},
				},
			},
		},
	}
}

// makeCustomCASecret returns a tls.crt + tls.key Secret in the operator
// namespace. checkCustomCASecret only verifies key presence, so byte
// values are irrelevant — the validator never parses them.
func makeCustomCASecret() *corev1.Secret {
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "custom-ca", Namespace: testOperatorNamespace},
		Data: map[string][]byte{
			"tls.crt": []byte("dummy-ca-cert"),
			"tls.key": []byte("dummy-ca-key"),
		},
	}
}

// markDeploymentAvailable creates the named Deployment (if missing) and
// sets Status.Conditions[Available]=True. cert-manager would normally
// drive this; envtest has no controllers, so the spec drives the
// transition manually. Replicas/selector are nominal — the operator's
// readiness gate only inspects the Available condition.
func markDeploymentAvailable(name, namespace string) {
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

// markDaemonSetReady creates the named DaemonSet (if missing) and
// sets its Status to DesiredNumberScheduled=NumberReady=1 so the
// reconciler's ensureContourReady sees envoy as Ready. envtest runs
// no DaemonSet controller, hence this helper.
func markDaemonSetReady() {
	name, namespace := envoyDaemonSet, contourNamespace
	GinkgoHelper()
	ds := &appsv1.DaemonSet{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
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
	Expect(k8sClient.Get(ctx, types.NamespacedName{Namespace: namespace, Name: name}, ds)).To(Succeed())
	ds.Status = appsv1.DaemonSetStatus{
		DesiredNumberScheduled: 1,
		NumberReady:            1,
	}
	Expect(k8sClient.Status().Update(ctx, ds)).To(Succeed())
}

// resurrectStuckNamespace force-finalizes a namespace that's been
// left in Terminating state by a previous spec. envtest runs no
// namespace controller, so a Delete on a namespace with kubernetes
// finalizers (which every namespace has by default) leaves it stuck
// forever; without intervention, subsequent specs trying to create
// resources in it hit "namespace is being terminated" 403s. Calling
// the /finalize subresource with empty spec.finalizers is the
// canonical way to bypass the finalizer controller for tests.
func resurrectStuckNamespace(name string) {
	GinkgoHelper()
	ns := &corev1.Namespace{}
	err := k8sClient.Get(ctx, types.NamespacedName{Name: name}, ns)
	if apierrors.IsNotFound(err) {
		return
	}
	Expect(err).NotTo(HaveOccurred())
	if ns.DeletionTimestamp.IsZero() {
		return
	}
	ns.Spec.Finalizers = nil
	Expect(k8sClient.SubResource("finalize").Update(ctx, ns)).To(Succeed())
	Eventually(func() bool {
		err := k8sClient.Get(ctx, types.NamespacedName{Name: name}, ns)
		return apierrors.IsNotFound(err)
	}, 5*time.Second, 100*time.Millisecond).Should(BeTrue())
}

// markCertificateReady flips the named Certificate's Ready condition
// to True; cert-manager would normally do this after issuance. envtest
// has no cert-manager controller, hence this helper.
func markCertificateReady() {
	name, namespace := wildcardCertificate, testOperatorNamespace
	GinkgoHelper()
	cert := &cmv1.Certificate{}
	Expect(k8sClient.Get(ctx, types.NamespacedName{Namespace: namespace, Name: name}, cert)).To(Succeed())
	cert.Status = cmv1.CertificateStatus{
		Conditions: []cmv1.CertificateCondition{{
			Type:   cmv1.CertificateConditionReady,
			Status: cmmeta.ConditionTrue,
		}},
	}
	Expect(k8sClient.Status().Update(ctx, cert)).To(Succeed())
}

// memoryHelmFactory builds an in-memory Helm client per namespace and
// memoises the result. Returning a stable client per namespace lets
// tests assert against the release store the reconciler writes to.
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

var _ = Describe("EducatesClusterConfig Managed-mode reconciler", func() {
	var (
		mgrCancel context.CancelFunc
		mgrDone   chan error
		helmFac   *memoryHelmFactory
	)

	BeforeEach(func() {
		ensureNamespace(testOperatorNamespace)
		// Previous specs that exercised cleanupManaged leave the
		// cert-manager + contour namespaces in Terminating; envtest
		// has no namespace controller to actually delete them.
		// Resurrect so markDeploymentAvailable / markDaemonSetReady
		// in subsequent specs can create resources inside.
		resurrectStuckNamespace(certManagerNamespace)
		resurrectStuckNamespace(contourNamespace)
		resurrectStuckNamespace(externalDNSNamespace)
		resurrectStuckNamespace(kyvernoNamespace)
		helmFac = newMemoryHelmFactory()

		var mgrCtx context.Context
		mgrCtx, mgrCancel = context.WithCancel(ctx)

		mgr, err := ctrl.NewManager(cfg, ctrl.Options{
			Scheme: k8sClient.Scheme(),
			Cache: cache.Options{
				ByObject: map[client.Object]cache.ByObject{
					&corev1.Secret{}: {
						Namespaces: map[string]cache.Config{
							testOperatorNamespace: {},
						},
					},
				},
			},
			Metrics:    metricsserver.Options{BindAddress: "0"},
			Controller: crconfig.Controller{SkipNameValidation: new(true)},
		})
		Expect(err).NotTo(HaveOccurred())

		Expect((&EducatesClusterConfigReconciler{
			Client:            mgr.GetClient(),
			APIReader:         mgr.GetAPIReader(),
			Scheme:            mgr.GetScheme(),
			OperatorNamespace: testOperatorNamespace,
			HelmClientFor:     helmFac.For,
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
		// Platform CRs first: a lingering singleton would block the
		// next spec's finalizer drain via the deletion-ordering guard.
		// No platform reconciler runs in this suite, so no finalizers.
		_ = k8sClient.DeleteAllOf(ctx, &platformv1alpha1.SecretsManager{})
		_ = k8sClient.DeleteAllOf(ctx, &platformv1alpha1.LookupService{})
		_ = k8sClient.DeleteAllOf(ctx, &platformv1alpha1.SessionManager{})
		drainCR()
		_ = k8sClient.DeleteAllOf(ctx, &corev1.Secret{}, client.InNamespace(testOperatorNamespace))
		_ = k8sClient.DeleteAllOf(ctx, &cmv1.Certificate{}, client.InNamespace(testOperatorNamespace))
		_ = k8sClient.DeleteAllOf(ctx, &appsv1.Deployment{}, client.InNamespace(certManagerNamespace))
		_ = k8sClient.DeleteAllOf(ctx, &corev1.Secret{}, client.InNamespace(certManagerNamespace))
		_ = k8sClient.DeleteAllOf(ctx, &appsv1.Deployment{}, client.InNamespace(contourNamespace))
		_ = k8sClient.DeleteAllOf(ctx, &appsv1.DaemonSet{}, client.InNamespace(contourNamespace))
		_ = k8sClient.DeleteAllOf(ctx, &appsv1.Deployment{}, client.InNamespace(externalDNSNamespace))
		_ = k8sClient.DeleteAllOf(ctx, &appsv1.Deployment{}, client.InNamespace(kyvernoNamespace))
		_ = k8sClient.DeleteAllOf(ctx, &networkingv1.IngressClass{})
		_ = k8sClient.DeleteAllOf(ctx, &cmv1.ClusterIssuer{})
		// Intentionally do NOT delete the cert-manager namespace: envtest
		// has no kube-controller-manager, so a namespace Delete leaves it
		// stuck in Terminating with finalizers cert-manager-style. The
		// next spec would then 403 on resource creation inside it. The
		// resources within are wiped above; the namespace itself is
		// reusable across specs.
	})

	It("installs cert-manager from the embedded chart and records its version", func() {
		Expect(k8sClient.Create(ctx, makeCustomCASecret())).To(Succeed())

		obj := &configv1alpha1.EducatesClusterConfig{
			ObjectMeta: metav1.ObjectMeta{Name: "cluster"},
			Spec:       validManagedSpec(),
		}
		Expect(k8sClient.Create(ctx, obj)).To(Succeed())

		// status.bundledChartVersions[cert-manager] populated after install.
		Eventually(func() string {
			got := &configv1alpha1.EducatesClusterConfig{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: "cluster"}, got); err != nil {
				return ""
			}
			return got.Status.BundledChartVersions["cert-manager"]
		}, 30*time.Second, 200*time.Millisecond).Should(Equal(vendoredcharts.CertManagerVersion))

		// cert-manager namespace is created with the operator's managed-by
		// label and owned by the EducatesClusterConfig.
		Eventually(func() error {
			ns := &corev1.Namespace{}
			return k8sClient.Get(ctx, types.NamespacedName{Name: certManagerNamespace}, ns)
		}, 30*time.Second, 200*time.Millisecond).Should(Succeed())

		ns := &corev1.Namespace{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: certManagerNamespace}, ns)).To(Succeed())
		Expect(ns.Labels).To(HaveKeyWithValue("app.kubernetes.io/managed-by", managedByLabelValue))
		Expect(ns.OwnerReferences).To(HaveLen(1))
		Expect(ns.OwnerReferences[0].Kind).To(Equal("EducatesClusterConfig"))

		// The in-memory Helm store holds a cert-manager release.
		hc, err := helmFac.For(certManagerNamespace)
		Expect(err).NotTo(HaveOccurred())
		rel, err := hc.Status(certManagerReleaseName)
		Expect(err).NotTo(HaveOccurred())
		Expect(rel.Chart.Metadata.Name).To(Equal("cert-manager"))

		// With cert-manager Deployments absent (no controller in envtest),
		// readiness gate keeps CertificatesReady=False/WaitingForCertManager.
		// The happy-path "Deployments Available + Certificate Ready" flow
		// is covered by the dedicated spec below.
		got := &configv1alpha1.EducatesClusterConfig{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "cluster"}, got)).To(Succeed())
		cond := meta.FindStatusCondition(got.Status.Conditions, conditionCertificatesReady)
		Expect(cond).NotTo(BeNil())
		Expect(cond.Status).To(Equal(metav1.ConditionFalse))
		Expect(cond.Reason).To(Equal("WaitingForCertManager"))
		Expect(got.Status.Phase).To(Equal(configv1alpha1.ClusterConfigPhaseInstalling))
	})

	It("surfaces a failed cert-manager Helm release as Degraded instead of reporting Ready", func() {
		Expect(k8sClient.Create(ctx, makeCustomCASecret())).To(Succeed())

		obj := &configv1alpha1.EducatesClusterConfig{
			ObjectMeta: metav1.ObjectMeta{Name: "cluster"},
			Spec:       validManagedSpec(),
		}

		// Stage a failed cert-manager release whose inputs match what the
		// reconciler will render, so EnsureRelease holds it (rather than
		// reinstalling) — standing in for a chart that fails to apply.
		// Seeding before the CR exists guarantees the reconciler observes
		// the failed release on its first cert-manager pass.
		hc, err := helmFac.For(certManagerNamespace)
		Expect(err).NotTo(HaveOccurred())
		chrt, err := vendoredcharts.CertManager()
		Expect(err).NotTo(HaveOccurred())
		Expect(hc.SeedRelease(
			certManagerReleaseName, 1, releasecommon.StatusFailed,
			chrt, renderCertManagerValues(obj),
			"ClusterIssuer is invalid: spec.ca.secretName: Required value",
		)).To(Succeed())

		Expect(k8sClient.Create(ctx, obj)).To(Succeed())

		// CertificatesReady flips False with reason ReleaseFailed.
		Eventually(func() string {
			got := &configv1alpha1.EducatesClusterConfig{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: "cluster"}, got); err != nil {
				return ""
			}
			cond := meta.FindStatusCondition(got.Status.Conditions, conditionCertificatesReady)
			if cond == nil {
				return ""
			}
			return cond.Reason
		}, 30*time.Second, 200*time.Millisecond).Should(Equal("ReleaseFailed"))

		got := &configv1alpha1.EducatesClusterConfig{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "cluster"}, got)).To(Succeed())
		Expect(got.Status.Phase).To(Equal(configv1alpha1.ClusterConfigPhaseDegraded))
		cond := meta.FindStatusCondition(got.Status.Conditions, conditionCertificatesReady)
		Expect(cond.Status).To(Equal(metav1.ConditionFalse))
		// The Helm failure detail is surfaced verbatim in the condition.
		Expect(cond.Message).To(ContainSubstring("ClusterIssuer is invalid"))

		// The release must remain the single failed revision — the
		// matching fingerprint means no reinstall churn.
		live, err := hc.Status(certManagerReleaseName)
		Expect(err).NotTo(HaveOccurred())
		Expect(live.Version).To(Equal(1))
		Expect(live.Info.Status).To(Equal(releasecommon.StatusFailed))
	})

	It("flips to Degraded when the CustomCA Secret is missing", func() {
		obj := &configv1alpha1.EducatesClusterConfig{
			ObjectMeta: metav1.ObjectMeta{Name: "cluster"},
			Spec:       validManagedSpec(),
		}
		Expect(k8sClient.Create(ctx, obj)).To(Succeed())

		Eventually(readyConditionStatus, 30*time.Second, 200*time.Millisecond).
			Should(Equal(metav1.ConditionFalse))

		got := &configv1alpha1.EducatesClusterConfig{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "cluster"}, got)).To(Succeed())
		Expect(got.Status.Phase).To(Equal(configv1alpha1.ClusterConfigPhaseDegraded))

		// Helm release must NOT exist — validator failed before install.
		hc, err := helmFac.For(certManagerNamespace)
		Expect(err).NotTo(HaveOccurred())
		_, statusErr := hc.Status(certManagerReleaseName)
		Expect(statusErr).To(MatchError(helm.ErrReleaseNotFound))
	})

	It("rejects not-yet-supported providers with explicit validation errors", func() {
		spec := validManagedSpec()
		spec.Ingress.Certificates.Provider = configv1alpha1.CertificatesProviderStaticCertificate
		spec.Ingress.Certificates.BundledCertManager = nil
		spec.Ingress.Certificates.StaticCertificate = &configv1alpha1.StaticCertificateConfig{
			TLSSecretRef: configv1alpha1.LocalObjectReference{Name: "wildcard-tls"},
		}

		obj := &configv1alpha1.EducatesClusterConfig{
			ObjectMeta: metav1.ObjectMeta{Name: "cluster"},
			Spec:       spec,
		}
		Expect(k8sClient.Create(ctx, obj)).To(Succeed())

		Eventually(func() string {
			got := &configv1alpha1.EducatesClusterConfig{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: "cluster"}, got); err != nil {
				return ""
			}
			cond := meta.FindStatusCondition(got.Status.Conditions, conditionValidationSucceeded)
			if cond == nil {
				return ""
			}
			return cond.Message
		}, 30*time.Second, 200*time.Millisecond).Should(ContainSubstring("not yet supported"))
	})

	It("reaches Ready=True once cert-manager Deployments are Available and the wildcard Certificate is Issued", func() {
		Expect(k8sClient.Create(ctx, makeCustomCASecret())).To(Succeed())

		obj := &configv1alpha1.EducatesClusterConfig{
			ObjectMeta: metav1.ObjectMeta{Name: "cluster"},
			Spec:       validManagedSpec(),
		}
		Expect(k8sClient.Create(ctx, obj)).To(Succeed())

		// Wait for the operator to create the cert-manager namespace
		// (signal that the install pipeline has progressed past
		// validation + helm.Status/Install).
		Eventually(func() error {
			ns := &corev1.Namespace{}
			return k8sClient.Get(ctx, types.NamespacedName{Name: certManagerNamespace}, ns)
		}, 30*time.Second, 200*time.Millisecond).Should(Succeed())

		// envtest has no controllers to roll out Deployments, so stand
		// up the three cert-manager Deployments with Available=True
		// manually. The operator's readiness gate observes them via the
		// Deployment watch.
		for _, name := range certManagerDeployments {
			markDeploymentAvailable(name, certManagerNamespace)
		}

		// Wait for the operator to apply the wildcard Certificate.
		// cert-manager would normally set status.conditions[Ready]=True
		// after issuance; envtest has no cert-manager controller, so
		// the test forces the transition.
		Eventually(func() error {
			cert := &cmv1.Certificate{}
			return k8sClient.Get(ctx, types.NamespacedName{Namespace: testOperatorNamespace, Name: wildcardCertificate}, cert)
		}, 30*time.Second, 200*time.Millisecond).Should(Succeed())
		markCertificateReady()

		// Wait for the operator to reach the Contour phase + create
		// its namespace, then drive the contour Deployment + envoy
		// DaemonSet to Ready (no controllers in envtest).
		Eventually(func() error {
			ns := &corev1.Namespace{}
			return k8sClient.Get(ctx, types.NamespacedName{Name: contourNamespace}, ns)
		}, 30*time.Second, 200*time.Millisecond).Should(Succeed())
		markDeploymentAvailable(contourControllerDeployment, contourNamespace)
		markDaemonSetReady()

		Eventually(readyConditionStatus, 30*time.Second, 200*time.Millisecond).
			Should(Equal(metav1.ConditionTrue))

		got := &configv1alpha1.EducatesClusterConfig{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "cluster"}, got)).To(Succeed())
		Expect(got.Status.Phase).To(Equal(configv1alpha1.ClusterConfigPhaseReady))

		// status.bundledChartVersions now includes both cert-manager
		// and contour.
		Expect(got.Status.BundledChartVersions).To(HaveKeyWithValue("cert-manager", vendoredcharts.CertManagerVersion))
		Expect(got.Status.BundledChartVersions).To(HaveKeyWithValue("contour", vendoredcharts.ContourChartVersion))

		// status.ingress published with the wildcard secret + issuer ref.
		Expect(got.Status.Ingress).NotTo(BeNil())
		Expect(got.Status.Ingress.Domain).To(Equal("educates.test"))
		Expect(got.Status.Ingress.IngressClassName).To(Equal("contour"))
		Expect(got.Status.Ingress.WildcardCertificateSecretRef.Namespace).To(Equal(testOperatorNamespace))
		Expect(got.Status.Ingress.WildcardCertificateSecretRef.Name).To(Equal(wildcardTLSSecretName))
		Expect(got.Status.Ingress.ClusterIssuerRef).NotTo(BeNil())
		Expect(got.Status.Ingress.ClusterIssuerRef.Name).To(Equal(wildcardClusterIssuer))

		// status.ingress also publishes the CustomCA Secret so that the
		// platform reconcilers hand it to the component charts, which
		// mount it to trust ingress endpoints signed by the private CA.
		// The fixture leaves the ref's namespace empty, so it resolves
		// to the operator namespace.
		Expect(got.Status.Ingress.CACertificateSecretRef).NotTo(BeNil())
		Expect(got.Status.Ingress.CACertificateSecretRef.Namespace).To(Equal(testOperatorNamespace))
		Expect(got.Status.Ingress.CACertificateSecretRef.Name).To(Equal("custom-ca"))

		// CustomCA Secret was copied into cert-manager namespace.
		copied := &corev1.Secret{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Namespace: certManagerNamespace, Name: customCASecretName}, copied)).To(Succeed())
		Expect(copied.Type).To(Equal(corev1.SecretTypeTLS))
		Expect(copied.Data).To(HaveKey("tls.crt"))
		Expect(copied.Data).To(HaveKey("tls.key"))
	})

	It("tears down installed resources in reverse order on delete", func() {
		Expect(k8sClient.Create(ctx, makeCustomCASecret())).To(Succeed())

		obj := &configv1alpha1.EducatesClusterConfig{
			ObjectMeta: metav1.ObjectMeta{Name: "cluster"},
			Spec:       validManagedSpec(),
		}
		Expect(k8sClient.Create(ctx, obj)).To(Succeed())

		// Drive the CR to Ready first so cleanup actually has something
		// to undo. Same staging as the happy-path spec above.
		Eventually(func() error {
			ns := &corev1.Namespace{}
			return k8sClient.Get(ctx, types.NamespacedName{Name: certManagerNamespace}, ns)
		}, 30*time.Second, 200*time.Millisecond).Should(Succeed())
		for _, name := range certManagerDeployments {
			markDeploymentAvailable(name, certManagerNamespace)
		}
		Eventually(func() error {
			cert := &cmv1.Certificate{}
			return k8sClient.Get(ctx, types.NamespacedName{Namespace: testOperatorNamespace, Name: wildcardCertificate}, cert)
		}, 30*time.Second, 200*time.Millisecond).Should(Succeed())
		markCertificateReady()
		Eventually(func() error {
			ns := &corev1.Namespace{}
			return k8sClient.Get(ctx, types.NamespacedName{Name: contourNamespace}, ns)
		}, 30*time.Second, 200*time.Millisecond).Should(Succeed())
		markDeploymentAvailable(contourControllerDeployment, contourNamespace)
		markDaemonSetReady()
		Eventually(readyConditionStatus, 30*time.Second, 200*time.Millisecond).
			Should(Equal(metav1.ConditionTrue))

		// Both helm releases exist at this point.
		cmClient, err := helmFac.For(certManagerNamespace)
		Expect(err).NotTo(HaveOccurred())
		_, err = cmClient.Status(certManagerReleaseName)
		Expect(err).NotTo(HaveOccurred())
		contourClient, err := helmFac.For(contourNamespace)
		Expect(err).NotTo(HaveOccurred())
		_, err = contourClient.Status(contourReleaseName)
		Expect(err).NotTo(HaveOccurred())

		// Delete the CR; let the reconciler's finalizer drain run.
		Expect(k8sClient.Delete(ctx, obj)).To(Succeed())

		// The CR is gone once the finalizer is removed by cleanupManaged.
		Eventually(func() bool {
			got := &configv1alpha1.EducatesClusterConfig{}
			err := k8sClient.Get(ctx, types.NamespacedName{Name: "cluster"}, got)
			return apierrors.IsNotFound(err)
		}, 30*time.Second, 200*time.Millisecond).Should(BeTrue())

		// Certificate, ClusterIssuer, and copied CustomCA Secret deleted.
		// envtest has no namespace controller, so the cert-manager
		// namespace lingers in Terminating — we don't assert on it.
		Expect(apierrors.IsNotFound(k8sClient.Get(ctx, types.NamespacedName{Namespace: testOperatorNamespace, Name: wildcardCertificate}, &cmv1.Certificate{}))).To(BeTrue())
		Expect(apierrors.IsNotFound(k8sClient.Get(ctx, types.NamespacedName{Name: wildcardClusterIssuer}, &cmv1.ClusterIssuer{}))).To(BeTrue())
		Expect(apierrors.IsNotFound(k8sClient.Get(ctx, types.NamespacedName{Namespace: certManagerNamespace, Name: customCASecretName}, &corev1.Secret{}))).To(BeTrue())

		// Both helm releases are uninstalled.
		_, statusErr := cmClient.Status(certManagerReleaseName)
		Expect(statusErr).To(MatchError(helm.ErrReleaseNotFound))
		_, statusErr = contourClient.Status(contourReleaseName)
		Expect(statusErr).To(MatchError(helm.ErrReleaseNotFound))
	})

	It("refuses to drain cluster services while platform CRs exist, then unblocks once they are gone", func() {
		Expect(k8sClient.Create(ctx, makeCustomCASecret())).To(Succeed())

		obj := &configv1alpha1.EducatesClusterConfig{
			ObjectMeta: metav1.ObjectMeta{Name: "cluster"},
			Spec:       validManagedSpec(),
		}
		Expect(k8sClient.Create(ctx, obj)).To(Succeed())

		// Let the install pipeline progress at least past the
		// cert-manager helm install so teardown has something real
		// to (not) undo.
		Eventually(func() error {
			ns := &corev1.Namespace{}
			return k8sClient.Get(ctx, types.NamespacedName{Name: certManagerNamespace}, ns)
		}, 30*time.Second, 200*time.Millisecond).Should(Succeed())

		// A platform component CR exists (no platform reconciler runs
		// in this suite, so it carries no finalizer of its own).
		sm := &platformv1alpha1.SecretsManager{
			ObjectMeta: metav1.ObjectMeta{Name: "cluster"},
		}
		Expect(k8sClient.Create(ctx, sm)).To(Succeed())

		// Deleting the EducatesClusterConfig must NOT drain cluster
		// services: status surfaces the ordering requirement instead.
		Expect(k8sClient.Delete(ctx, obj)).To(Succeed())

		Eventually(func() string {
			got := &configv1alpha1.EducatesClusterConfig{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: "cluster"}, got); err != nil {
				return ""
			}
			cond := meta.FindStatusCondition(got.Status.Conditions, conditionReady)
			if cond == nil {
				return ""
			}
			return cond.Reason
		}, 30*time.Second, 200*time.Millisecond).Should(Equal(reasonPlatformCRsPresent))

		got := &configv1alpha1.EducatesClusterConfig{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "cluster"}, got)).To(Succeed())
		Expect(got.Status.Phase).To(Equal(configv1alpha1.ClusterConfigPhaseUninstalling))
		cond := meta.FindStatusCondition(got.Status.Conditions, conditionReady)
		Expect(cond.Message).To(ContainSubstring("SecretsManager"))

		// The guard must hold, not just lag: the CR stays terminating
		// and the cert-manager release stays installed.
		Consistently(func() error {
			return k8sClient.Get(ctx, types.NamespacedName{Name: "cluster"}, &configv1alpha1.EducatesClusterConfig{})
		}, 3*time.Second, 500*time.Millisecond).Should(Succeed())
		cmClient, err := helmFac.For(certManagerNamespace)
		Expect(err).NotTo(HaveOccurred())
		_, err = cmClient.Status(certManagerReleaseName)
		Expect(err).NotTo(HaveOccurred())

		// Deleting the platform CR unblocks the drain end-to-end.
		Expect(k8sClient.Delete(ctx, sm)).To(Succeed())

		Eventually(func() bool {
			err := k8sClient.Get(ctx, types.NamespacedName{Name: "cluster"}, &configv1alpha1.EducatesClusterConfig{})
			return apierrors.IsNotFound(err)
		}, 30*time.Second, 200*time.Millisecond).Should(BeTrue())

		_, statusErr := cmClient.Status(certManagerReleaseName)
		Expect(statusErr).To(MatchError(helm.ErrReleaseNotFound))
	})

	It("flips to Degraded with CertManagerCRDsMissing when cert-manager CRDs are deleted out from under it", func() {
		// Restore cert-manager CRDs at end-of-spec regardless of
		// success — Ginkgo runs specs in random order and the rest
		// of the suite relies on these CRDs being present.
		DeferCleanup(func() {
			_, err := envtest.InstallCRDs(cfg, envtest.CRDInstallOptions{
				Paths: []string{filepath.Join("testdata", "crds", "cert-manager")},
			})
			Expect(err).NotTo(HaveOccurred())
		})

		Expect(k8sClient.Create(ctx, makeCustomCASecret())).To(Succeed())

		obj := &configv1alpha1.EducatesClusterConfig{
			ObjectMeta: metav1.ObjectMeta{Name: "cluster"},
			Spec:       validManagedSpec(),
		}
		Expect(k8sClient.Create(ctx, obj)).To(Succeed())

		// Drive to Ready first so the operator has actually invested in
		// cert-manager state — same staging as the happy-path spec.
		Eventually(func() error {
			ns := &corev1.Namespace{}
			return k8sClient.Get(ctx, types.NamespacedName{Name: certManagerNamespace}, ns)
		}, 30*time.Second, 200*time.Millisecond).Should(Succeed())
		for _, name := range certManagerDeployments {
			markDeploymentAvailable(name, certManagerNamespace)
		}
		Eventually(func() error {
			cert := &cmv1.Certificate{}
			return k8sClient.Get(ctx, types.NamespacedName{Namespace: testOperatorNamespace, Name: wildcardCertificate}, cert)
		}, 30*time.Second, 200*time.Millisecond).Should(Succeed())
		markCertificateReady()
		Eventually(func() error {
			ns := &corev1.Namespace{}
			return k8sClient.Get(ctx, types.NamespacedName{Name: contourNamespace}, ns)
		}, 30*time.Second, 200*time.Millisecond).Should(Succeed())
		markDeploymentAvailable(contourControllerDeployment, contourNamespace)
		markDaemonSetReady()
		Eventually(readyConditionStatus, 30*time.Second, 200*time.Millisecond).
			Should(Equal(metav1.ConditionTrue))

		// Yank the cert-manager CRDs out from under the running
		// operator. In production this would be `kubectl delete crd
		// certificates.cert-manager.io clusterissuers.cert-manager.io`
		// (or `helm uninstall cert-manager` cascading the delete).
		for _, name := range []string{
			"certificates.cert-manager.io",
			"clusterissuers.cert-manager.io",
		} {
			crd := &apiextensionsv1.CustomResourceDefinition{
				ObjectMeta: metav1.ObjectMeta{Name: name},
			}
			Expect(k8sClient.Delete(ctx, crd)).To(Succeed())
		}

		// Touching the CR with an annotation triggers a fresh
		// reconcile. The reconciler's first SSA against ClusterIssuer
		// (or Certificate) returns NoMatchError; the classifier picks
		// it up and routes to handleCertManagerCRDsMissing.
		Eventually(func() error {
			got := &configv1alpha1.EducatesClusterConfig{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: "cluster"}, got); err != nil {
				return err
			}
			if got.Annotations == nil {
				got.Annotations = map[string]string{}
			}
			got.Annotations["test.educates.dev/poke"] = time.Now().Format(time.RFC3339Nano)
			return k8sClient.Update(ctx, got)
		}, 30*time.Second, 200*time.Millisecond).Should(Succeed())

		Eventually(func() string {
			got := &configv1alpha1.EducatesClusterConfig{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: "cluster"}, got); err != nil {
				return ""
			}
			cond := meta.FindStatusCondition(got.Status.Conditions, conditionCertificatesReady)
			if cond == nil {
				return ""
			}
			return cond.Reason
		}, 30*time.Second, 200*time.Millisecond).Should(Equal("CertManagerCRDsMissing"))

		got := &configv1alpha1.EducatesClusterConfig{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "cluster"}, got)).To(Succeed())
		Expect(got.Status.Phase).To(Equal(configv1alpha1.ClusterConfigPhaseDegraded))
	})

	It("installs external-dns (Route53/IRSA) and reaches Ready when DNS+ingress+cert are all up", func() {
		Expect(k8sClient.Create(ctx, makeCustomCASecret())).To(Succeed())

		spec := validManagedSpec()
		spec.DNS = &configv1alpha1.DNS{
			Provider: configv1alpha1.DNSProviderBundledExternalDNS,
			BundledExternalDNS: &configv1alpha1.BundledExternalDNSConfig{
				Provider: configv1alpha1.DNS01ProviderRoute53,
				Route53: &configv1alpha1.ExternalDNSRoute53Config{
					HostedZoneID: "Z0123456789ABCDEF",
					IAMRoleARN:   "arn:aws:iam::123456789012:role/external-dns",
				},
			},
		}

		obj := &configv1alpha1.EducatesClusterConfig{
			ObjectMeta: metav1.ObjectMeta{Name: "cluster"},
			Spec:       spec,
		}
		Expect(k8sClient.Create(ctx, obj)).To(Succeed())

		// Drive cert-manager + Contour to Ready first (same staging
		// as the existing happy-path spec).
		Eventually(func() error {
			ns := &corev1.Namespace{}
			return k8sClient.Get(ctx, types.NamespacedName{Name: certManagerNamespace}, ns)
		}, 30*time.Second, 200*time.Millisecond).Should(Succeed())
		for _, name := range certManagerDeployments {
			markDeploymentAvailable(name, certManagerNamespace)
		}
		Eventually(func() error {
			cert := &cmv1.Certificate{}
			return k8sClient.Get(ctx, types.NamespacedName{Namespace: testOperatorNamespace, Name: wildcardCertificate}, cert)
		}, 30*time.Second, 200*time.Millisecond).Should(Succeed())
		markCertificateReady()

		Eventually(func() error {
			ns := &corev1.Namespace{}
			return k8sClient.Get(ctx, types.NamespacedName{Name: contourNamespace}, ns)
		}, 30*time.Second, 200*time.Millisecond).Should(Succeed())
		markDeploymentAvailable(contourControllerDeployment, contourNamespace)
		markDaemonSetReady()

		// Now the external-dns phase should fire and create its
		// namespace + Deployment; drive the Deployment to Available.
		Eventually(func() error {
			ns := &corev1.Namespace{}
			return k8sClient.Get(ctx, types.NamespacedName{Name: externalDNSNamespace}, ns)
		}, 30*time.Second, 200*time.Millisecond).Should(Succeed())
		markDeploymentAvailable(externalDNSControllerDeploy, externalDNSNamespace)

		Eventually(readyConditionStatus, 30*time.Second, 200*time.Millisecond).
			Should(Equal(metav1.ConditionTrue))

		got := &configv1alpha1.EducatesClusterConfig{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "cluster"}, got)).To(Succeed())
		Expect(got.Status.Phase).To(Equal(configv1alpha1.ClusterConfigPhaseReady))
		Expect(got.Status.BundledChartVersions).To(HaveKeyWithValue("external-dns", vendoredcharts.ExternalDNSChartVersion))

		// DNSReady condition flipped True with the BundledExternalDNS
		// reason.
		dnsReady := meta.FindStatusCondition(got.Status.Conditions, conditionDNSReady)
		Expect(dnsReady).NotTo(BeNil())
		Expect(dnsReady.Status).To(Equal(metav1.ConditionTrue))
		Expect(dnsReady.Reason).To(Equal("BundledExternalDNSReady"))
	})

	It("rejects Route53 with neither IRSA nor static credentials", func() {
		Expect(k8sClient.Create(ctx, makeCustomCASecret())).To(Succeed())

		spec := validManagedSpec()
		spec.DNS = &configv1alpha1.DNS{
			Provider: configv1alpha1.DNSProviderBundledExternalDNS,
			BundledExternalDNS: &configv1alpha1.BundledExternalDNSConfig{
				Provider: configv1alpha1.DNS01ProviderRoute53,
				Route53: &configv1alpha1.ExternalDNSRoute53Config{
					HostedZoneID: "Z0123456789ABCDEF",
					// neither IAMRoleARN nor CredentialsSecretRef → degraded
				},
			},
		}

		obj := &configv1alpha1.EducatesClusterConfig{
			ObjectMeta: metav1.ObjectMeta{Name: "cluster"},
			Spec:       spec,
		}
		Expect(k8sClient.Create(ctx, obj)).To(Succeed())

		// Drive cert-manager + Contour to Ready so the operator
		// reaches the external-dns validator.
		Eventually(func() error {
			ns := &corev1.Namespace{}
			return k8sClient.Get(ctx, types.NamespacedName{Name: certManagerNamespace}, ns)
		}, 30*time.Second, 200*time.Millisecond).Should(Succeed())
		for _, name := range certManagerDeployments {
			markDeploymentAvailable(name, certManagerNamespace)
		}
		Eventually(func() error {
			cert := &cmv1.Certificate{}
			return k8sClient.Get(ctx, types.NamespacedName{Namespace: testOperatorNamespace, Name: wildcardCertificate}, cert)
		}, 30*time.Second, 200*time.Millisecond).Should(Succeed())
		markCertificateReady()
		Eventually(func() error {
			ns := &corev1.Namespace{}
			return k8sClient.Get(ctx, types.NamespacedName{Name: contourNamespace}, ns)
		}, 30*time.Second, 200*time.Millisecond).Should(Succeed())
		markDeploymentAvailable(contourControllerDeployment, contourNamespace)
		markDaemonSetReady()

		// Validator surfaces a Degraded with a useful message.
		Eventually(func() string {
			got := &configv1alpha1.EducatesClusterConfig{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: "cluster"}, got); err != nil {
				return ""
			}
			cond := meta.FindStatusCondition(got.Status.Conditions, conditionValidationSucceeded)
			if cond == nil {
				return ""
			}
			return cond.Message
		}, 30*time.Second, 200*time.Millisecond).Should(ContainSubstring("exactly one of iamRoleARN or credentialsSecretRef"))
	})

	It("installs Kyverno and reaches Ready when all four controllers are Available", func() {
		Expect(k8sClient.Create(ctx, makeCustomCASecret())).To(Succeed())

		spec := validManagedSpec()
		spec.PolicyEnforcement = &configv1alpha1.PolicyEnforcement{
			ClusterPolicy: configv1alpha1.ClusterPolicyConfig{
				Engine: configv1alpha1.ClusterPolicyEngineKyverno,
			},
			WorkshopPolicy: configv1alpha1.WorkshopPolicyConfig{
				Engine: configv1alpha1.WorkshopPolicyEngineKyverno,
			},
			Kyverno: &configv1alpha1.KyvernoConfig{
				Provider: configv1alpha1.KyvernoProviderBundled,
			},
		}

		obj := &configv1alpha1.EducatesClusterConfig{
			ObjectMeta: metav1.ObjectMeta{Name: "cluster"},
			Spec:       spec,
		}
		Expect(k8sClient.Create(ctx, obj)).To(Succeed())

		// Drive prior phases (cert-manager, Contour) to Ready first.
		Eventually(func() error {
			ns := &corev1.Namespace{}
			return k8sClient.Get(ctx, types.NamespacedName{Name: certManagerNamespace}, ns)
		}, 30*time.Second, 200*time.Millisecond).Should(Succeed())
		for _, name := range certManagerDeployments {
			markDeploymentAvailable(name, certManagerNamespace)
		}
		Eventually(func() error {
			cert := &cmv1.Certificate{}
			return k8sClient.Get(ctx, types.NamespacedName{Namespace: testOperatorNamespace, Name: wildcardCertificate}, cert)
		}, 30*time.Second, 200*time.Millisecond).Should(Succeed())
		markCertificateReady()
		Eventually(func() error {
			ns := &corev1.Namespace{}
			return k8sClient.Get(ctx, types.NamespacedName{Name: contourNamespace}, ns)
		}, 30*time.Second, 200*time.Millisecond).Should(Succeed())
		markDeploymentAvailable(contourControllerDeployment, contourNamespace)
		markDaemonSetReady()

		// Now wait for Kyverno's namespace to appear, then drive
		// each of the four controller Deployments to Available.
		Eventually(func() error {
			ns := &corev1.Namespace{}
			return k8sClient.Get(ctx, types.NamespacedName{Name: kyvernoNamespace}, ns)
		}, 30*time.Second, 200*time.Millisecond).Should(Succeed())
		for _, name := range kyvernoDeployments {
			markDeploymentAvailable(name, kyvernoNamespace)
		}

		Eventually(readyConditionStatus, 30*time.Second, 200*time.Millisecond).
			Should(Equal(metav1.ConditionTrue))

		got := &configv1alpha1.EducatesClusterConfig{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "cluster"}, got)).To(Succeed())
		Expect(got.Status.Phase).To(Equal(configv1alpha1.ClusterConfigPhaseReady))
		Expect(got.Status.BundledChartVersions).To(HaveKeyWithValue("kyverno", vendoredcharts.KyvernoChartVersion))

		policyReady := meta.FindStatusCondition(got.Status.Conditions, conditionPolicyEnforcementReady)
		Expect(policyReady).NotTo(BeNil())
		Expect(policyReady.Status).To(Equal(metav1.ConditionTrue))
		Expect(policyReady.Reason).To(Equal("BundledKyvernoReady"))
	})

	// ACME-DNS01 validator coverage. Driving an end-to-end ACME
	// install is impossible in envtest (no real cert-manager, no DNS
	// provider), so these specs cover the validator branches and the
	// ClusterIssuer shape only.
	It("accepts ACME + Route53 with IAMRoleARN and writes an ACME ClusterIssuer", func() {
		spec := validManagedSpec()
		spec.Ingress.Certificates.BundledCertManager = &configv1alpha1.BundledCertManagerConfig{
			IssuerType: configv1alpha1.IssuerTypeACME,
			ACME: &configv1alpha1.ACMEConfig{
				Email: "ops@example.com",
				Solvers: configv1alpha1.ACMESolvers{
					DNS01: configv1alpha1.ACMEDNS01Solver{
						Provider: configv1alpha1.DNS01ProviderRoute53,
						Route53: &configv1alpha1.Route53Config{
							HostedZoneID: "Z0123456789ABCDEF",
							Region:       "us-east-1",
							IAMRoleARN:   "arn:aws:iam::123456789012:role/cert-manager",
						},
					},
				},
			},
		}

		obj := &configv1alpha1.EducatesClusterConfig{
			ObjectMeta: metav1.ObjectMeta{Name: "cluster"},
			Spec:       spec,
		}
		Expect(k8sClient.Create(ctx, obj)).To(Succeed())

		// Drive cert-manager to Ready so the operator reaches
		// ensureClusterIssuer.
		Eventually(func() error {
			ns := &corev1.Namespace{}
			return k8sClient.Get(ctx, types.NamespacedName{Name: certManagerNamespace}, ns)
		}, 30*time.Second, 200*time.Millisecond).Should(Succeed())
		for _, name := range certManagerDeployments {
			markDeploymentAvailable(name, certManagerNamespace)
		}

		Eventually(func(g Gomega) {
			ci := &cmv1.ClusterIssuer{}
			g.Expect(k8sClient.Get(ctx, types.NamespacedName{Name: wildcardClusterIssuer}, ci)).To(Succeed())
			g.Expect(ci.Spec.ACME).NotTo(BeNil())
			g.Expect(ci.Spec.ACME.Email).To(Equal("ops@example.com"))
			g.Expect(ci.Spec.ACME.Server).To(Equal(letsEncryptProdServer))
			g.Expect(ci.Spec.ACME.Solvers).To(HaveLen(1))
			g.Expect(ci.Spec.ACME.Solvers[0].DNS01).NotTo(BeNil())
			g.Expect(ci.Spec.ACME.Solvers[0].DNS01.Route53).NotTo(BeNil())
			g.Expect(ci.Spec.ACME.Solvers[0].DNS01.Route53.HostedZoneID).To(Equal("Z0123456789ABCDEF"))
			g.Expect(ci.Spec.ACME.Solvers[0].DNS01.Route53.Role).To(Equal("arn:aws:iam::123456789012:role/cert-manager"))
			g.Expect(ci.Spec.CA).To(BeNil())
		}, 30*time.Second, 200*time.Millisecond).Should(Succeed())
	})

	It("rejects ACME + Route53 without IAMRoleARN", func() {
		spec := validManagedSpec()
		spec.Ingress.Certificates.BundledCertManager = &configv1alpha1.BundledCertManagerConfig{
			IssuerType: configv1alpha1.IssuerTypeACME,
			ACME: &configv1alpha1.ACMEConfig{
				Email: "ops@example.com",
				Solvers: configv1alpha1.ACMESolvers{
					DNS01: configv1alpha1.ACMEDNS01Solver{
						Provider: configv1alpha1.DNS01ProviderRoute53,
						Route53: &configv1alpha1.Route53Config{
							HostedZoneID: "Z0123456789ABCDEF",
						},
					},
				},
			},
		}
		obj := &configv1alpha1.EducatesClusterConfig{
			ObjectMeta: metav1.ObjectMeta{Name: "cluster"},
			Spec:       spec,
		}
		Expect(k8sClient.Create(ctx, obj)).To(Succeed())

		Eventually(func() string {
			got := &configv1alpha1.EducatesClusterConfig{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: "cluster"}, got); err != nil {
				return ""
			}
			cond := meta.FindStatusCondition(got.Status.Conditions, conditionValidationSucceeded)
			if cond == nil {
				return ""
			}
			return cond.Message
		}, 30*time.Second, 200*time.Millisecond).Should(ContainSubstring("iamRoleARN"))
	})

	It("rejects ACME + CloudDNS without workloadIdentityServiceAccount", func() {
		spec := validManagedSpec()
		spec.Ingress.Certificates.BundledCertManager = &configv1alpha1.BundledCertManagerConfig{
			IssuerType: configv1alpha1.IssuerTypeACME,
			ACME: &configv1alpha1.ACMEConfig{
				Email: "ops@example.com",
				Solvers: configv1alpha1.ACMESolvers{
					DNS01: configv1alpha1.ACMEDNS01Solver{
						Provider: configv1alpha1.DNS01ProviderCloudDNS,
						CloudDNS: &configv1alpha1.CloudDNSConfig{
							Project: "my-gcp-project",
						},
					},
				},
			},
		}
		obj := &configv1alpha1.EducatesClusterConfig{
			ObjectMeta: metav1.ObjectMeta{Name: "cluster"},
			Spec:       spec,
		}
		Expect(k8sClient.Create(ctx, obj)).To(Succeed())

		Eventually(func() string {
			got := &configv1alpha1.EducatesClusterConfig{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: "cluster"}, got); err != nil {
				return ""
			}
			cond := meta.FindStatusCondition(got.Status.Conditions, conditionValidationSucceeded)
			if cond == nil {
				return ""
			}
			return cond.Message
		}, 30*time.Second, 200*time.Millisecond).Should(ContainSubstring("workloadIdentityServiceAccount"))
	})

	It("rejects ACME with an unsupported DNS01 provider (Cloudflare)", func() {
		spec := validManagedSpec()
		spec.Ingress.Certificates.BundledCertManager = &configv1alpha1.BundledCertManagerConfig{
			IssuerType: configv1alpha1.IssuerTypeACME,
			ACME: &configv1alpha1.ACMEConfig{
				Email: "ops@example.com",
				Solvers: configv1alpha1.ACMESolvers{
					DNS01: configv1alpha1.ACMEDNS01Solver{
						Provider: configv1alpha1.DNS01ProviderCloudflare,
					},
				},
			},
		}
		obj := &configv1alpha1.EducatesClusterConfig{
			ObjectMeta: metav1.ObjectMeta{Name: "cluster"},
			Spec:       spec,
		}
		Expect(k8sClient.Create(ctx, obj)).To(Succeed())

		Eventually(func() string {
			got := &configv1alpha1.EducatesClusterConfig{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: "cluster"}, got); err != nil {
				return ""
			}
			cond := meta.FindStatusCondition(got.Status.Conditions, conditionValidationSucceeded)
			if cond == nil {
				return ""
			}
			return cond.Message
		}, 30*time.Second, 200*time.Millisecond).Should(ContainSubstring("not yet supported"))
	})
})
