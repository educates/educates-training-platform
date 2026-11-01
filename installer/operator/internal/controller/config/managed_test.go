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
	"bufio"
	"bytes"
	"context"
	"errors"
	"io"
	"path/filepath"
	"sync"
	"time"

	cmv1 "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"
	cmmeta "github.com/cert-manager/cert-manager/pkg/apis/meta/v1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	chart "helm.sh/helm/v4/pkg/chart/v2"
	releasecommon "helm.sh/helm/v4/pkg/release/common"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	utilyaml "k8s.io/apimachinery/pkg/util/yaml"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	crconfig "sigs.k8s.io/controller-runtime/pkg/config"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
	"sigs.k8s.io/yaml"

	configv1alpha1 "github.com/educates/educates-training-platform/installer/operator/api/config/v1alpha1"
	platformv1alpha1 "github.com/educates/educates-training-platform/installer/operator/api/platform/v1alpha1"
	"github.com/educates/educates-training-platform/installer/operator/internal/helm"
	"github.com/educates/educates-training-platform/installer/operator/internal/kyverno/kyvernotest"
	"github.com/educates/educates-training-platform/installer/operator/internal/ownership"
	"github.com/educates/educates-training-platform/installer/operator/internal/pki"
	"github.com/educates/educates-training-platform/installer/operator/internal/testpki"
	vendoredcharts "github.com/educates/educates-training-platform/installer/operator/vendored-charts"
)

// testCASecretsNamespace stands in for educates-secrets: a namespace
// other than the operator namespace that CA Secret references may name
// and that the operator's Secret cache covers.
const testCASecretsNamespace = "test-ca-secrets"

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
						CAKeyPairSecretRef: configv1alpha1.SecretReference{
							Name: "custom-ca",
						},
					},
				},
			},
		},
		PolicyEnforcement: noPolicyEnforcement(),
	}
}

// noPolicyEnforcement sets both policy engines to None, so a spec that
// exercises the other cluster services has the operator neither install
// Kyverno nor look for one.
func noPolicyEnforcement() *configv1alpha1.PolicyEnforcement {
	return &configv1alpha1.PolicyEnforcement{
		ClusterPolicy:  configv1alpha1.ClusterPolicyConfig{Engine: configv1alpha1.ClusterPolicyEngineNone},
		WorkshopPolicy: configv1alpha1.WorkshopPolicyConfig{Engine: configv1alpha1.WorkshopPolicyEngineNone},
	}
}

// validationSucceededMessage returns the ValidationSucceeded condition's message,
// or "" before the reconciler has set it.
func validationSucceededMessage() string {
	got := &configv1alpha1.EducatesClusterConfig{}
	if err := k8sClient.Get(ctx, types.NamespacedName{Name: "cluster"}, got); err != nil {
		return ""
	}
	cond := meta.FindStatusCondition(got.Status.Conditions, conditionValidationSucceeded)
	if cond == nil {
		return ""
	}
	return cond.Message
}

// policyEnforcementReadyCondition returns the PolicyEnforcementReady
// condition, or nil before the reconciler has set it.
func policyEnforcementReadyCondition() *metav1.Condition {
	got := &configv1alpha1.EducatesClusterConfig{}
	if err := k8sClient.Get(ctx, types.NamespacedName{Name: "cluster"}, got); err != nil {
		return nil
	}
	return meta.FindStatusCondition(got.Status.Conditions, conditionPolicyEnforcementReady)
}

// expectNoReleaseInstalled asserts the operator installed no Helm release
// for the named cluster service.
func expectNoReleaseInstalled(helmFac *memoryHelmFactory, svc clusterServiceRelease) {
	GinkgoHelper()
	hc, err := helmFac.For(svc.namespace)
	Expect(err).NotTo(HaveOccurred())
	_, err = hc.Status(svc.releaseName)
	Expect(err).To(MatchError(helm.ErrReleaseNotFound))
}

// clusterServiceRelease names the Helm release, and the namespace it
// lives in, that the operator installs a bundled cluster service as.
type clusterServiceRelease struct {
	namespace   string
	releaseName string
	loadChart   func() (*chart.Chart, error)
}

var (
	certManagerRelease = clusterServiceRelease{certManagerNamespace, certManagerReleaseName, vendoredcharts.CertManager}
	contourRelease     = clusterServiceRelease{contourNamespace, contourReleaseName, vendoredcharts.Contour}
	externalDNSRelease = clusterServiceRelease{externalDNSNamespace, externalDNSReleaseName, vendoredcharts.ExternalDNS}
	kyvernoRelease     = clusterServiceRelease{kyvernoNamespace, kyvernoReleaseName, vendoredcharts.Kyverno}
)

// deleteClusterConfigOverSeededRelease seeds a deployed release of svc
// in its namespace through helmFac, creates an EducatesClusterConfig
// with spec, deletes it once its finalizer is in place and waits for
// the teardown to finish. It returns the Helm client for svc's
// namespace.
func deleteClusterConfigOverSeededRelease(helmFac *memoryHelmFactory, svc clusterServiceRelease, spec configv1alpha1.EducatesClusterConfigSpec) *helm.Client {
	ensureNamespace(svc.namespace)
	// Later specs use the service's namespace appearing as their sign
	// that its install has started, so delete it afterwards; the next
	// BeforeEach finalizes it away.
	DeferCleanup(func() {
		_ = k8sClient.Delete(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: svc.namespace}})
	})
	hc, err := helmFac.For(svc.namespace)
	Expect(err).NotTo(HaveOccurred())
	chrt, err := svc.loadChart()
	Expect(err).NotTo(HaveOccurred())
	Expect(hc.SeedRelease(
		svc.releaseName, 1, releasecommon.StatusDeployed,
		chrt, map[string]any{}, "Install complete",
	)).To(Succeed())

	obj := &configv1alpha1.EducatesClusterConfig{
		ObjectMeta: metav1.ObjectMeta{Name: "cluster"},
		Spec:       spec,
	}
	Expect(k8sClient.Create(ctx, obj)).To(Succeed())

	// Wait for the finalizer so the delete below runs the reconciler's
	// teardown rather than removing the CR outright.
	Eventually(func() []string {
		got := &configv1alpha1.EducatesClusterConfig{}
		if err := k8sClient.Get(ctx, types.NamespacedName{Name: "cluster"}, got); err != nil {
			return nil
		}
		return got.Finalizers
	}, 30*time.Second, 200*time.Millisecond).Should(ContainElement(finalizerName))

	Expect(k8sClient.Delete(ctx, obj)).To(Succeed())
	Eventually(func() bool {
		err := k8sClient.Get(ctx, types.NamespacedName{Name: "cluster"}, &configv1alpha1.EducatesClusterConfig{})
		return apierrors.IsNotFound(err)
	}, 30*time.Second, 200*time.Millisecond).Should(BeTrue())
	return hc
}

// staticCertificateSpec returns a Managed-mode spec that serves the
// wildcard certificate in Secret wildcard-tls, verified against the CA
// trust Secret wildcard-ca, both in testCASecretsNamespace.
func staticCertificateSpec() configv1alpha1.EducatesClusterConfigSpec {
	spec := validManagedSpec()
	spec.Ingress.Certificates = configv1alpha1.Certificates{
		Provider: configv1alpha1.CertificatesProviderStaticCertificate,
		StaticCertificate: &configv1alpha1.StaticCertificateConfig{
			TLSSecretRef: configv1alpha1.SecretReference{
				Name:      "wildcard-tls",
				Namespace: testCASecretsNamespace,
			},
			CACertificateSecretRef: &configv1alpha1.SecretReference{
				Name:      "wildcard-ca",
				Namespace: testCASecretsNamespace,
			},
		},
	}
	return spec
}

// staticWildcardSecret returns the wildcard Secret staticCertificateSpec
// references, holding the given tls.crt and tls.key; a nil value leaves
// its key out.
func staticWildcardSecret(certPEM, keyPEM []byte) *corev1.Secret {
	data := map[string][]byte{}
	if certPEM != nil {
		data["tls.crt"] = certPEM
	}
	if keyPEM != nil {
		data["tls.key"] = keyPEM
	}
	return makeSecret(testCASecretsNamespace, "wildcard-tls", data)
}

// staticCASecret returns the CA trust Secret staticCertificateSpec
// references, holding ca's certificate.
func staticCASecret(ca *testpki.Certificate) *corev1.Secret {
	return makeSecret(testCASecretsNamespace, "wildcard-ca", map[string][]byte{"ca.crt": ca.CertPEM})
}

// makeCustomCASecret returns the CA key pair validManagedSpec references:
// a self-signed root CA's tls.crt + tls.key in the operator namespace,
// with no ca.crt, the shape the local CLI cache produces.
func makeCustomCASecret() *corev1.Secret {
	root := testpki.NewRootCA()
	return makeSecret(testOperatorNamespace, "custom-ca", map[string][]byte{
		"tls.crt": root.CertPEM,
		"tls.key": root.KeyPEM,
	})
}

// makeSecret returns a Secret with the given data, for CA key pairs and
// CA trust Secrets built from testpki material.
func makeSecret(namespace, name string, data map[string][]byte) *corev1.Secret {
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
		Data:       data,
	}
}

// certificatesReadyReason returns the reason of the singleton's
// CertificatesReady condition, or "" when it is not set yet.
func certificatesReadyReason() string {
	got := &configv1alpha1.EducatesClusterConfig{}
	if err := k8sClient.Get(ctx, types.NamespacedName{Name: "cluster"}, got); err != nil {
		return ""
	}
	cond := meta.FindStatusCondition(got.Status.Conditions, conditionCertificatesReady)
	if cond == nil {
		return ""
	}
	return cond.Reason
}

// ingressCAData returns the data of the derived CA trust Secret, or nil
// while it does not exist.
func ingressCAData() map[string][]byte {
	s := &corev1.Secret{}
	if err := k8sClient.Get(ctx, types.NamespacedName{Namespace: testOperatorNamespace, Name: ingressCASecretName}, s); err != nil {
		return nil
	}
	return s.Data
}

// wildcardTLSData returns the data of the operator-owned wildcard
// Secret, or nil while it does not exist.
func wildcardTLSData() map[string][]byte {
	s := &corev1.Secret{}
	if err := k8sClient.Get(ctx, types.NamespacedName{Namespace: testOperatorNamespace, Name: wildcardTLSSecretName}, s); err != nil {
		return nil
	}
	return s.Data
}

// driveStaticCertificateToReady takes a StaticCertificate config to
// Ready: it waits for the certificates phase to finish, which the
// Contour namespace appearing signals, then stands up Contour.
func driveStaticCertificateToReady() {
	GinkgoHelper()
	Eventually(func() error {
		return k8sClient.Get(ctx, types.NamespacedName{Name: contourNamespace}, &corev1.Namespace{})
	}, 30*time.Second, 200*time.Millisecond).Should(Succeed())
	markDeploymentAvailable(contourControllerDeployment, contourNamespace)
	markDaemonSetReady()
	Eventually(readyConditionStatus, 30*time.Second, 200*time.Millisecond).
		Should(Equal(metav1.ConditionTrue))
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

// expectChartCRDsApplied waits until every CRD the chart ships in its crds/
// directory, each document of a file holding several included, is on the
// cluster with the chart's schema for each of its versions. The in-memory
// Helm client skips crds/, so only the operator's own apply puts them there.
func expectChartCRDsApplied(chrt *chart.Chart) {
	GinkgoHelper()
	var want []*apiextensionsv1.CustomResourceDefinition
	for _, obj := range chrt.CRDObjects() {
		reader := utilyaml.NewYAMLReader(bufio.NewReader(bytes.NewReader(obj.File.Data)))
		for {
			doc, err := reader.Read()
			if errors.Is(err, io.EOF) {
				break
			}
			Expect(err).NotTo(HaveOccurred(), "read %s", obj.Filename)
			crd := &apiextensionsv1.CustomResourceDefinition{}
			Expect(yaml.Unmarshal(doc, crd)).To(Succeed(), "parse %s", obj.Filename)
			if crd.Name != "" {
				want = append(want, crd)
			}
		}
	}
	Expect(want).NotTo(BeEmpty(), "chart %s ships no CRDs", chrt.Name())

	for _, crd := range want {
		Eventually(func(g Gomega) {
			live := &apiextensionsv1.CustomResourceDefinition{}
			g.Expect(k8sClient.Get(ctx, client.ObjectKey{Name: crd.Name}, live)).To(Succeed())
			g.Expect(live.Spec.Versions).To(HaveLen(len(crd.Spec.Versions)))
			for i := range crd.Spec.Versions {
				g.Expect(live.Spec.Versions[i].Name).To(Equal(crd.Spec.Versions[i].Name))
				g.Expect(live.Spec.Versions[i].Schema).To(Equal(crd.Spec.Versions[i].Schema),
					"schema of %s %s", crd.Name, crd.Spec.Versions[i].Name)
			}
		}, 30*time.Second, 200*time.Millisecond).Should(Succeed())
	}
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
		// kyvernoDiscovery answers the Kyverno presence check; envtest
		// serves no Kyverno, so specs flip it to the case they test.
		kyvernoDiscovery *kyvernotest.Discovery
	)

	BeforeEach(func() {
		ensureNamespace(testOperatorNamespace)
		ensureNamespace(testCASecretsNamespace)
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
		kyvernoDiscovery = &kyvernotest.Discovery{}

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
			KyvernoDiscovery:  kyvernoDiscovery,
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
		_ = k8sClient.DeleteAllOf(ctx, &corev1.Secret{}, client.InNamespace(testCASecretsNamespace))
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
		Expect(ns.Labels).To(HaveKeyWithValue(ownership.ManagedByLabel, ownership.ManagedByValue))
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

	DescribeTable("rejects an unusable CA key pair with a specific reason before installing cert-manager",
		func(data func() map[string][]byte, wantReason, wantMessage string) {
			Expect(k8sClient.Create(ctx, makeSecret(testOperatorNamespace, "custom-ca", data()))).To(Succeed())
			obj := &configv1alpha1.EducatesClusterConfig{
				ObjectMeta: metav1.ObjectMeta{Name: "cluster"},
				Spec:       validManagedSpec(),
			}
			Expect(k8sClient.Create(ctx, obj)).To(Succeed())

			Eventually(certificatesReadyReason, 30*time.Second, 200*time.Millisecond).Should(Equal(wantReason))

			got := &configv1alpha1.EducatesClusterConfig{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "cluster"}, got)).To(Succeed())
			Expect(got.Status.Phase).To(Equal(configv1alpha1.ClusterConfigPhaseDegraded))
			cond := meta.FindStatusCondition(got.Status.Conditions, conditionCertificatesReady)
			Expect(cond.Status).To(Equal(metav1.ConditionFalse))
			Expect(cond.Message).To(ContainSubstring(wantMessage))
			Expect(meta.IsStatusConditionFalse(got.Status.Conditions, conditionReady)).To(BeTrue())

			// cert-manager is never involved, and no CA trust is derived.
			hc, err := helmFac.For(certManagerNamespace)
			Expect(err).NotTo(HaveOccurred())
			_, statusErr := hc.Status(certManagerReleaseName)
			Expect(statusErr).To(MatchError(helm.ErrReleaseNotFound))
			Expect(ingressCAData()).To(BeNil())
		},
		Entry("when tls.key is missing",
			func() map[string][]byte {
				return map[string][]byte{"tls.crt": testpki.NewRootCA().CertPEM}
			},
			pki.ReasonCAKeyPairIncomplete, `"tls.key"`),
		Entry("when tls.crt is missing",
			func() map[string][]byte {
				return map[string][]byte{"tls.key": testpki.NewRootCA().KeyPEM}
			},
			pki.ReasonCAKeyPairIncomplete, `"tls.crt"`),
		Entry("when its certificate is not a CA",
			func() map[string][]byte {
				leaf := testpki.NewRootCA().NewLeaf(testpki.WithDNSNames("*.educates.test"))
				return map[string][]byte{"tls.crt": leaf.ChainPEM(), "tls.key": leaf.KeyPEM}
			},
			pki.ReasonCAKeyPairNotCA, "not a CA certificate"),
	)

	It("rejects a referenced CA trust Secret without ca.crt with a specific reason", func() {
		Expect(k8sClient.Create(ctx, makeCustomCASecret())).To(Succeed())
		Expect(k8sClient.Create(ctx, makeSecret(testOperatorNamespace, "custom-ca-trust", map[string][]byte{
			"tls.crt": testpki.NewRootCA().CertPEM,
		}))).To(Succeed())
		spec := validManagedSpec()
		spec.Ingress.Certificates.BundledCertManager.CustomCA.CACertificateSecretRef = &configv1alpha1.SecretReference{
			Name: "custom-ca-trust",
		}
		Expect(k8sClient.Create(ctx, &configv1alpha1.EducatesClusterConfig{
			ObjectMeta: metav1.ObjectMeta{Name: "cluster"},
			Spec:       spec,
		})).To(Succeed())

		Eventually(certificatesReadyReason, 30*time.Second, 200*time.Millisecond).
			Should(Equal(pki.ReasonCATrustSecretIncomplete))
		Expect(ingressCAData()).To(BeNil())
	})

	DescribeTable("derives educates-ingress-ca holding only ca.crt",
		func(setup func(spec *configv1alpha1.EducatesClusterConfigSpec) (want []byte)) {
			spec := validManagedSpec()
			want := setup(&spec)
			Expect(k8sClient.Create(ctx, &configv1alpha1.EducatesClusterConfig{
				ObjectMeta: metav1.ObjectMeta{Name: "cluster"},
				Spec:       spec,
			})).To(Succeed())

			Eventually(ingressCAData, 30*time.Second, 200*time.Millisecond).
				Should(Equal(map[string][]byte{"ca.crt": want}))

			derived := &corev1.Secret{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Namespace: testOperatorNamespace, Name: ingressCASecretName}, derived)).To(Succeed())
			Expect(derived.Type).To(Equal(corev1.SecretTypeOpaque))
			Expect(derived.Labels).To(HaveKeyWithValue(ownership.ManagedByLabel, ownership.ManagedByValue))
			Expect(derived.OwnerReferences).To(HaveLen(1))
			Expect(derived.OwnerReferences[0].Kind).To(Equal("EducatesClusterConfig"))
			Expect(derived.OwnerReferences[0].Name).To(Equal("cluster"))
			Expect(*derived.OwnerReferences[0].Controller).To(BeTrue())
		},
		Entry("from the root-most certificate of the CA key pair's tls.crt chain",
			func(spec *configv1alpha1.EducatesClusterConfigSpec) []byte {
				root := testpki.NewRootCA()
				inter := root.NewIntermediateCA()
				Expect(k8sClient.Create(ctx, makeSecret(testOperatorNamespace, "custom-ca", map[string][]byte{
					"tls.crt": inter.ChainPEM(),
					"tls.key": inter.KeyPEM,
				}))).To(Succeed())
				return root.CertPEM
			}),
		Entry("from the CA key pair's ca.crt",
			func(spec *configv1alpha1.EducatesClusterConfigSpec) []byte {
				root := testpki.NewRootCA()
				inter := root.NewIntermediateCA()
				// tls.crt alone would yield the intermediate; ca.crt wins.
				Expect(k8sClient.Create(ctx, makeSecret(testCASecretsNamespace, "custom-ca", map[string][]byte{
					"tls.crt": inter.CertPEM,
					"tls.key": inter.KeyPEM,
					"ca.crt":  root.CertPEM,
				}))).To(Succeed())
				spec.Ingress.Certificates.BundledCertManager.CustomCA.CAKeyPairSecretRef.Namespace = testCASecretsNamespace
				return root.CertPEM
			}),
		Entry("from the CA trust Secret, ignoring a key it also holds",
			func(spec *configv1alpha1.EducatesClusterConfigSpec) []byte {
				Expect(k8sClient.Create(ctx, makeCustomCASecret())).To(Succeed())
				trusted := testpki.NewRootCA()
				Expect(k8sClient.Create(ctx, makeSecret(testCASecretsNamespace, "custom-ca-trust", map[string][]byte{
					"ca.crt":  trusted.CertPEM,
					"tls.crt": trusted.CertPEM,
					"tls.key": trusted.KeyPEM,
				}))).To(Succeed())
				spec.Ingress.Certificates.BundledCertManager.CustomCA.CACertificateSecretRef = &configv1alpha1.SecretReference{
					Name:      "custom-ca-trust",
					Namespace: testCASecretsNamespace,
				}
				return trusted.CertPEM
			}),
	)

	It("refreshes educates-ingress-ca when its source Secret changes", func() {
		Expect(k8sClient.Create(ctx, makeCustomCASecret())).To(Succeed())
		first, second := testpki.NewRootCA(), testpki.NewRootCA()
		trust := makeSecret(testCASecretsNamespace, "custom-ca-trust", map[string][]byte{"ca.crt": first.CertPEM})
		Expect(k8sClient.Create(ctx, trust)).To(Succeed())
		spec := validManagedSpec()
		spec.Ingress.Certificates.BundledCertManager.CustomCA.CACertificateSecretRef = &configv1alpha1.SecretReference{
			Name:      "custom-ca-trust",
			Namespace: testCASecretsNamespace,
		}
		Expect(k8sClient.Create(ctx, &configv1alpha1.EducatesClusterConfig{
			ObjectMeta: metav1.ObjectMeta{Name: "cluster"},
			Spec:       spec,
		})).To(Succeed())
		Eventually(ingressCAData, 30*time.Second, 200*time.Millisecond).
			Should(Equal(map[string][]byte{"ca.crt": first.CertPEM}))

		// No spec change: only the Secret watch can carry this through.
		Expect(k8sClient.Get(ctx, types.NamespacedName{Namespace: testCASecretsNamespace, Name: "custom-ca-trust"}, trust)).To(Succeed())
		trust.Data = map[string][]byte{"ca.crt": second.CertPEM}
		Expect(k8sClient.Update(ctx, trust)).To(Succeed())

		Eventually(ingressCAData, 30*time.Second, 200*time.Millisecond).
			Should(Equal(map[string][]byte{"ca.crt": second.CertPEM}))
	})

	It("rejects the ExternalCertManager provider as not yet supported", func() {
		spec := validManagedSpec()
		spec.Ingress.Certificates = configv1alpha1.Certificates{
			Provider: configv1alpha1.CertificatesProviderExternalCertManager,
			ExternalCertManager: &configv1alpha1.ExternalCertManagerConfig{
				ClusterIssuerRef: configv1alpha1.LocalObjectReference{Name: "byo-issuer"},
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
		}, 30*time.Second, 200*time.Millisecond).Should(And(
			ContainSubstring("spec.ingress.certificates.provider"),
			ContainSubstring(`"ExternalCertManager" is not yet supported`)))

		got := &configv1alpha1.EducatesClusterConfig{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "cluster"}, got)).To(Succeed())
		Expect(got.Status.Phase).To(Equal(configv1alpha1.ClusterConfigPhaseDegraded))
		hc, err := helmFac.For(certManagerNamespace)
		Expect(err).NotTo(HaveOccurred())
		_, statusErr := hc.Status(certManagerReleaseName)
		Expect(statusErr).To(MatchError(helm.ErrReleaseNotFound))
	})

	It("serves a StaticCertificate without cert-manager and reaches Ready", func() {
		root := testpki.NewRootCA()
		leaf := root.NewLeaf(testpki.WithDNSNames("*.educates.test"))
		// Extra keys in the source never reach the served copy or the
		// derived CA trust.
		Expect(k8sClient.Create(ctx, makeSecret(testCASecretsNamespace, "wildcard-tls", map[string][]byte{
			"tls.crt": leaf.ChainPEM(),
			"tls.key": leaf.KeyPEM,
			"ca.crt":  root.CertPEM,
		}))).To(Succeed())
		Expect(k8sClient.Create(ctx, makeSecret(testCASecretsNamespace, "wildcard-ca", map[string][]byte{
			"ca.crt":  root.CertPEM,
			"tls.key": root.KeyPEM,
		}))).To(Succeed())

		Expect(k8sClient.Create(ctx, &configv1alpha1.EducatesClusterConfig{
			ObjectMeta: metav1.ObjectMeta{Name: "cluster"},
			Spec:       staticCertificateSpec(),
		})).To(Succeed())

		// Certificates come first; Contour's namespace appearing means
		// that phase is done.
		Eventually(func() error {
			return k8sClient.Get(ctx, types.NamespacedName{Name: contourNamespace}, &corev1.Namespace{})
		}, 30*time.Second, 200*time.Millisecond).Should(Succeed())
		markDeploymentAvailable(contourControllerDeployment, contourNamespace)
		markDaemonSetReady()
		Eventually(readyConditionStatus, 30*time.Second, 200*time.Millisecond).
			Should(Equal(metav1.ConditionTrue))

		got := &configv1alpha1.EducatesClusterConfig{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "cluster"}, got)).To(Succeed())
		certs := meta.FindStatusCondition(got.Status.Conditions, conditionCertificatesReady)
		Expect(certs).NotTo(BeNil())
		Expect(certs.Status).To(Equal(metav1.ConditionTrue))
		Expect(certs.Reason).To(Equal("StaticCertificateValid"))
		Expect(meta.IsStatusConditionTrue(got.Status.Conditions, conditionValidationSucceeded)).To(BeTrue())

		Expect(got.Status.Ingress.Protocol).To(Equal(configv1alpha1.IngressProtocolHTTPS))
		Expect(got.Status.Ingress.WildcardCertificateSecretRef).To(Equal(&configv1alpha1.NamespacedSecretRef{
			Namespace: testOperatorNamespace,
			Name:      "educates-wildcard-tls",
		}))
		Expect(got.Status.Ingress.ClusterIssuerRef).To(BeNil())
		Expect(got.Status.Ingress.CACertificateSecretRef).To(Equal(&configv1alpha1.NamespacedSecretRef{
			Namespace: testOperatorNamespace,
			Name:      "educates-ingress-ca",
		}))
		Expect(ingressCAData()).To(Equal(map[string][]byte{"ca.crt": root.CertPEM}))

		served := &corev1.Secret{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Namespace: testOperatorNamespace, Name: "educates-wildcard-tls"}, served)).To(Succeed())
		Expect(served.Type).To(Equal(corev1.SecretTypeTLS))
		Expect(served.Data).To(Equal(map[string][]byte{"tls.crt": leaf.ChainPEM(), "tls.key": leaf.KeyPEM}))
		Expect(served.Labels).To(HaveKeyWithValue(ownership.ManagedByLabel, ownership.ManagedByValue))
		Expect(served.OwnerReferences).To(HaveLen(1))
		Expect(served.OwnerReferences[0].Kind).To(Equal("EducatesClusterConfig"))

		// No cert-manager, ClusterIssuer or Certificate.
		Expect(got.Status.BundledChartVersions).NotTo(HaveKey("cert-manager"))
		hc, err := helmFac.For(certManagerNamespace)
		Expect(err).NotTo(HaveOccurred())
		_, statusErr := hc.Status(certManagerReleaseName)
		Expect(statusErr).To(MatchError(helm.ErrReleaseNotFound))
		issuers := &cmv1.ClusterIssuerList{}
		Expect(k8sClient.List(ctx, issuers)).To(Succeed())
		Expect(issuers.Items).To(BeEmpty())
		certificates := &cmv1.CertificateList{}
		Expect(k8sClient.List(ctx, certificates)).To(Succeed())
		Expect(certificates.Items).To(BeEmpty())
	})

	It("keeps educates-wildcard-tls following the StaticCertificate source Secret", func() {
		root := testpki.NewRootCA()
		first := root.NewLeaf(testpki.WithDNSNames("*.educates.test"))
		second := root.NewLeaf(testpki.WithDNSNames("*.educates.test"))
		source := makeSecret(testCASecretsNamespace, "wildcard-tls", map[string][]byte{
			"tls.crt": first.CertPEM,
			"tls.key": first.KeyPEM,
		})
		Expect(k8sClient.Create(ctx, source)).To(Succeed())
		Expect(k8sClient.Create(ctx, makeSecret(testCASecretsNamespace, "wildcard-ca", map[string][]byte{
			"ca.crt": root.CertPEM,
		}))).To(Succeed())
		Expect(k8sClient.Create(ctx, &configv1alpha1.EducatesClusterConfig{
			ObjectMeta: metav1.ObjectMeta{Name: "cluster"},
			Spec:       staticCertificateSpec(),
		})).To(Succeed())
		driveStaticCertificateToReady()

		// Ready has no periodic requeue before the certificate expires,
		// so only the Secret watch can carry the change through in time.
		Expect(k8sClient.Get(ctx, types.NamespacedName{Namespace: testCASecretsNamespace, Name: "wildcard-tls"}, source)).To(Succeed())
		source.Data = map[string][]byte{"tls.crt": second.CertPEM, "tls.key": second.KeyPEM}
		Expect(k8sClient.Update(ctx, source)).To(Succeed())

		Eventually(wildcardTLSData, 10*time.Second, 200*time.Millisecond).
			Should(Equal(map[string][]byte{"tls.crt": second.CertPEM, "tls.key": second.KeyPEM}))

		// The CA trust Secret is watched too: educates-ingress-ca follows
		// it.
		bundle := append(append([]byte{}, root.CertPEM...), testpki.NewRootCA().CertPEM...)
		trust := &corev1.Secret{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Namespace: testCASecretsNamespace, Name: "wildcard-ca"}, trust)).To(Succeed())
		trust.Data = map[string][]byte{"ca.crt": bundle}
		Expect(k8sClient.Update(ctx, trust)).To(Succeed())

		Eventually(ingressCAData, 10*time.Second, 200*time.Millisecond).
			Should(Equal(map[string][]byte{"ca.crt": bundle}))
	})

	DescribeTable("rejects an unusable StaticCertificate with a specific reason",
		func(secrets func() []*corev1.Secret, wantReason string) {
			for _, s := range secrets() {
				Expect(k8sClient.Create(ctx, s)).To(Succeed())
			}
			Expect(k8sClient.Create(ctx, &configv1alpha1.EducatesClusterConfig{
				ObjectMeta: metav1.ObjectMeta{Name: "cluster"},
				Spec:       staticCertificateSpec(),
			})).To(Succeed())

			Eventually(certificatesReadyReason, 30*time.Second, 200*time.Millisecond).Should(Equal(wantReason))

			got := &configv1alpha1.EducatesClusterConfig{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "cluster"}, got)).To(Succeed())
			Expect(got.Status.Phase).To(Equal(configv1alpha1.ClusterConfigPhaseDegraded))
			Expect(meta.IsStatusConditionFalse(got.Status.Conditions, conditionCertificatesReady)).To(BeTrue())
			Expect(meta.IsStatusConditionFalse(got.Status.Conditions, conditionReady)).To(BeTrue())

			// Nothing is served or trusted, and cert-manager is never
			// involved.
			Expect(wildcardTLSData()).To(BeNil())
			Expect(ingressCAData()).To(BeNil())
			hc, err := helmFac.For(certManagerNamespace)
			Expect(err).NotTo(HaveOccurred())
			_, statusErr := hc.Status(certManagerReleaseName)
			Expect(statusErr).To(MatchError(helm.ErrReleaseNotFound))
		},
		Entry("when the wildcard Secret is missing",
			func() []*corev1.Secret {
				return []*corev1.Secret{staticCASecret(testpki.NewRootCA())}
			},
			pki.ReasonWildcardCertificateNotFound),
		Entry("when the CA trust Secret is missing",
			func() []*corev1.Secret {
				leaf := testpki.NewRootCA().NewLeaf(testpki.WithDNSNames("*.educates.test"))
				return []*corev1.Secret{staticWildcardSecret(leaf.CertPEM, leaf.KeyPEM)}
			},
			pki.ReasonCATrustSecretNotFound),
		Entry("when the wildcard Secret has no tls.key",
			func() []*corev1.Secret {
				root := testpki.NewRootCA()
				leaf := root.NewLeaf(testpki.WithDNSNames("*.educates.test"))
				return []*corev1.Secret{staticCASecret(root), staticWildcardSecret(leaf.CertPEM, nil)}
			},
			pki.ReasonWildcardCertificateIncomplete),
		Entry("when tls.crt does not parse",
			func() []*corev1.Secret {
				root := testpki.NewRootCA()
				leaf := root.NewLeaf(testpki.WithDNSNames("*.educates.test"))
				return []*corev1.Secret{staticCASecret(root), staticWildcardSecret([]byte("not a certificate"), leaf.KeyPEM)}
			},
			pki.ReasonWildcardCertificateUnparseable),
		Entry("when tls.key belongs to another certificate",
			func() []*corev1.Secret {
				root := testpki.NewRootCA()
				leaf := root.NewLeaf(testpki.WithDNSNames("*.educates.test"))
				other := root.NewLeaf(testpki.WithDNSNames("*.educates.test"))
				return []*corev1.Secret{staticCASecret(root), staticWildcardSecret(leaf.CertPEM, other.KeyPEM)}
			},
			pki.ReasonWildcardCertificateKeyMismatch),
		Entry("when its SANs do not cover the ingress domain",
			func() []*corev1.Secret {
				root := testpki.NewRootCA()
				leaf := root.NewLeaf(testpki.WithDNSNames("*.elsewhere.test"))
				return []*corev1.Secret{staticCASecret(root), staticWildcardSecret(leaf.CertPEM, leaf.KeyPEM)}
			},
			pki.ReasonWildcardCertificateDomainMismatch),
		Entry("when it has expired",
			func() []*corev1.Secret {
				root := testpki.NewRootCA()
				leaf := root.NewLeaf(testpki.WithDNSNames("*.educates.test"),
					testpki.WithValidity(time.Now().Add(-48*time.Hour), time.Now().Add(-24*time.Hour)))
				return []*corev1.Secret{staticCASecret(root), staticWildcardSecret(leaf.CertPEM, leaf.KeyPEM)}
			},
			pki.ReasonWildcardCertificateExpired),
		Entry("when its chain does not verify against the CA",
			func() []*corev1.Secret {
				leaf := testpki.NewRootCA().NewLeaf(testpki.WithDNSNames("*.educates.test"))
				return []*corev1.Secret{staticCASecret(testpki.NewRootCA()), staticWildcardSecret(leaf.ChainPEM(), leaf.KeyPEM)}
			},
			pki.ReasonWildcardCertificateUntrusted),
	)

	It("keeps educates-ingress-ca when the StaticCertificate's CA trust Secret changes to a CA the certificate does not verify against", func() {
		root := testpki.NewRootCA()
		leaf := root.NewLeaf(testpki.WithDNSNames("*.educates.test"))
		Expect(k8sClient.Create(ctx, staticWildcardSecret(leaf.CertPEM, leaf.KeyPEM))).To(Succeed())
		trust := staticCASecret(root)
		Expect(k8sClient.Create(ctx, trust)).To(Succeed())
		Expect(k8sClient.Create(ctx, &configv1alpha1.EducatesClusterConfig{
			ObjectMeta: metav1.ObjectMeta{Name: "cluster"},
			Spec:       staticCertificateSpec(),
		})).To(Succeed())
		driveStaticCertificateToReady()
		Expect(ingressCAData()).To(Equal(map[string][]byte{"ca.crt": root.CertPEM}))

		Expect(k8sClient.Get(ctx, types.NamespacedName{Namespace: testCASecretsNamespace, Name: "wildcard-ca"}, trust)).To(Succeed())
		trust.Data = map[string][]byte{"ca.crt": testpki.NewRootCA().CertPEM}
		Expect(k8sClient.Update(ctx, trust)).To(Succeed())

		Eventually(certificatesReadyReason, 30*time.Second, 200*time.Millisecond).
			Should(Equal(pki.ReasonWildcardCertificateUntrusted))
		// Components keep trusting the CA the served certificate verified
		// against; the CA that does not verify it never reaches them.
		Consistently(ingressCAData, 2*time.Second, 200*time.Millisecond).
			Should(Equal(map[string][]byte{"ca.crt": root.CertPEM}))
	})

	It("returns CertificatesReady and ValidationSucceeded to True once the StaticCertificate is fixed", func() {
		root := testpki.NewRootCA()
		other := root.NewLeaf(testpki.WithDNSNames("*.educates.test"))
		leaf := root.NewLeaf(testpki.WithDNSNames("*.educates.test"))
		source := staticWildcardSecret(leaf.CertPEM, other.KeyPEM)
		Expect(k8sClient.Create(ctx, source)).To(Succeed())
		Expect(k8sClient.Create(ctx, staticCASecret(root))).To(Succeed())
		Expect(k8sClient.Create(ctx, &configv1alpha1.EducatesClusterConfig{
			ObjectMeta: metav1.ObjectMeta{Name: "cluster"},
			Spec:       staticCertificateSpec(),
		})).To(Succeed())
		Eventually(certificatesReadyReason, 30*time.Second, 200*time.Millisecond).
			Should(Equal(pki.ReasonWildcardCertificateKeyMismatch))

		Expect(k8sClient.Get(ctx, types.NamespacedName{Namespace: testCASecretsNamespace, Name: "wildcard-tls"}, source)).To(Succeed())
		source.Data = map[string][]byte{"tls.crt": leaf.CertPEM, "tls.key": leaf.KeyPEM}
		Expect(k8sClient.Update(ctx, source)).To(Succeed())

		Eventually(certificatesReadyReason, 30*time.Second, 200*time.Millisecond).
			Should(Equal("StaticCertificateValid"))
		got := &configv1alpha1.EducatesClusterConfig{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "cluster"}, got)).To(Succeed())
		Expect(meta.IsStatusConditionTrue(got.Status.Conditions, conditionCertificatesReady)).To(BeTrue())
		Expect(meta.IsStatusConditionTrue(got.Status.Conditions, conditionValidationSucceeded)).To(BeTrue())
		Expect(wildcardTLSData()).To(Equal(map[string][]byte{"tls.crt": leaf.CertPEM, "tls.key": leaf.KeyPEM}))
	})

	It("flips CertificatesReady when a StaticCertificate expires, with no spec or Secret change", func() {
		root := testpki.NewRootCA()
		leaf := root.NewLeaf(testpki.WithDNSNames("*.educates.test"),
			testpki.WithValidity(time.Now().Add(-time.Hour), time.Now().Add(15*time.Second)))
		Expect(k8sClient.Create(ctx, staticWildcardSecret(leaf.CertPEM, leaf.KeyPEM))).To(Succeed())
		Expect(k8sClient.Create(ctx, staticCASecret(root))).To(Succeed())
		Expect(k8sClient.Create(ctx, &configv1alpha1.EducatesClusterConfig{
			ObjectMeta: metav1.ObjectMeta{Name: "cluster"},
			Spec:       staticCertificateSpec(),
		})).To(Succeed())
		driveStaticCertificateToReady()
		Expect(certificatesReadyReason()).To(Equal("StaticCertificateValid"))

		// Ready leaves nothing else to requeue reconcile, so only the
		// requeue at the certificate's expiry can report it.
		Eventually(certificatesReadyReason, 30*time.Second, 200*time.Millisecond).
			Should(Equal(pki.ReasonWildcardCertificateExpired))
		Expect(readyConditionStatus()).To(Equal(metav1.ConditionFalse))
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

		// The CA reaches components only as the key-free derived Secret.
		Expect(got.Status.Ingress.CACertificateSecretRef).To(Equal(&configv1alpha1.NamespacedSecretRef{
			Namespace: testOperatorNamespace,
			Name:      "educates-ingress-ca",
		}))
		Expect(ingressCAData()).To(HaveLen(1))
		Expect(ingressCAData()).To(HaveKey("ca.crt"))

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

	// An administrator may run their own cert-manager, Contour,
	// external-dns or Kyverno under the release and namespace names the
	// bundled one uses. Deleting an Educates install whose spec does not
	// bundle that service must leave it alone.
	DescribeTable("leaves a cluster service it did not install in place on delete",
		func(svc clusterServiceRelease, notBundled func(spec *configv1alpha1.EducatesClusterConfigSpec)) {
			spec := validManagedSpec()
			notBundled(&spec)
			hc := deleteClusterConfigOverSeededRelease(helmFac, svc, spec)

			rel, err := hc.Status(svc.releaseName)
			Expect(err).NotTo(HaveOccurred())
			Expect(rel.Info.Status).To(Equal(releasecommon.StatusDeployed))

			ns := &corev1.Namespace{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: svc.namespace}, ns)).To(Succeed())
			Expect(ns.DeletionTimestamp).To(BeNil())
		},
		Entry("cert-manager with certificates provider None", certManagerRelease,
			func(spec *configv1alpha1.EducatesClusterConfigSpec) {
				spec.Ingress.Certificates = configv1alpha1.Certificates{Provider: configv1alpha1.CertificatesProviderNone}
			}),
		Entry("cert-manager with certificates provider StaticCertificate", certManagerRelease,
			func(spec *configv1alpha1.EducatesClusterConfigSpec) {
				spec.Ingress.Certificates = configv1alpha1.Certificates{Provider: configv1alpha1.CertificatesProviderStaticCertificate}
			}),
		Entry("Contour with an external ingress controller", contourRelease,
			func(spec *configv1alpha1.EducatesClusterConfigSpec) {
				spec.Ingress.Controller.Provider = configv1alpha1.IngressControllerProviderExternalIngressController
			}),
		Entry("external-dns with DNS provider Manual", externalDNSRelease,
			func(spec *configv1alpha1.EducatesClusterConfigSpec) {
				spec.DNS = &configv1alpha1.DNS{Provider: configv1alpha1.DNSProviderManual}
			}),
		Entry("Kyverno with an external Kyverno", kyvernoRelease,
			func(spec *configv1alpha1.EducatesClusterConfigSpec) {
				spec.PolicyEnforcement = bundledKyvernoPolicyEnforcement()
				spec.PolicyEnforcement.Kyverno.Provider = configv1alpha1.KyvernoProviderExternal
			}),
	)

	// The gates above must still let teardown remove every service the
	// spec has the operator install, whether or not its install got as
	// far as Ready. Here validation fails for want of the CA Secret, so
	// the operator installs nothing and the seeded release stands in
	// for the one it would have installed.
	DescribeTable("removes a cluster service the spec bundles on delete",
		func(svc clusterServiceRelease, bundled func(spec *configv1alpha1.EducatesClusterConfigSpec)) {
			spec := validManagedSpec()
			bundled(&spec)
			hc := deleteClusterConfigOverSeededRelease(helmFac, svc, spec)

			_, err := hc.Status(svc.releaseName)
			Expect(err).To(MatchError(helm.ErrReleaseNotFound))

			// envtest has no namespace controller, so the namespace
			// stays Terminating rather than disappearing.
			ns := &corev1.Namespace{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: svc.namespace}, ns)).To(Succeed())
			Expect(ns.DeletionTimestamp).NotTo(BeNil())
		},
		Entry("cert-manager", certManagerRelease,
			func(*configv1alpha1.EducatesClusterConfigSpec) {}),
		Entry("Contour", contourRelease,
			func(*configv1alpha1.EducatesClusterConfigSpec) {}),
		Entry("external-dns", externalDNSRelease,
			func(spec *configv1alpha1.EducatesClusterConfigSpec) {
				spec.DNS = bundledRoute53DNS()
			}),
		Entry("Kyverno", kyvernoRelease,
			func(spec *configv1alpha1.EducatesClusterConfigSpec) {
				spec.PolicyEnforcement = bundledKyvernoPolicyEnforcement()
			}),
	)

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

		// The chart ships DNSEndpoint and DNSRecord in crds/, which Helm
		// applies only on a release's first install; the operator applies
		// them itself so a chart bump reaches upgraded clusters.
		chrt, err := vendoredcharts.ExternalDNS()
		Expect(err).NotTo(HaveOccurred())
		expectChartCRDsApplied(chrt)
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

	// Workshop security is enforced by Kyverno, and both engines default
	// to Kyverno, so a Managed spec must say how Kyverno is provided
	// whenever an engine is Kyverno, and must name the engines at all.
	// Either mistake is reported before any cluster service installs.
	DescribeTable("rejects policy enforcement it cannot honour before installing anything",
		func(pe *configv1alpha1.PolicyEnforcement, field, reason string) {
			Expect(k8sClient.Create(ctx, makeCustomCASecret())).To(Succeed())

			spec := validManagedSpec()
			spec.PolicyEnforcement = pe
			obj := &configv1alpha1.EducatesClusterConfig{
				ObjectMeta: metav1.ObjectMeta{Name: "cluster"},
				Spec:       spec,
			}
			Expect(k8sClient.Create(ctx, obj)).To(Succeed())

			Eventually(validationSucceededMessage, 30*time.Second, 200*time.Millisecond).Should(And(
				ContainSubstring(field+":"), ContainSubstring(reason)))

			got := &configv1alpha1.EducatesClusterConfig{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "cluster"}, got)).To(Succeed())
			Expect(got.Status.Phase).To(Equal(configv1alpha1.ClusterConfigPhaseDegraded))
			expectNoReleaseInstalled(helmFac, certManagerRelease)
			expectNoReleaseInstalled(helmFac, kyvernoRelease)
		},
		Entry("no spec.policyEnforcement", nil,
			"spec.policyEnforcement", "Managed mode requires spec.policyEnforcement"),
		Entry("a Kyverno workshop engine without the kyverno block",
			&configv1alpha1.PolicyEnforcement{
				ClusterPolicy:  configv1alpha1.ClusterPolicyConfig{Engine: configv1alpha1.ClusterPolicyEngineNone},
				WorkshopPolicy: configv1alpha1.WorkshopPolicyConfig{Engine: configv1alpha1.WorkshopPolicyEngineKyverno},
			},
			"spec.policyEnforcement.kyverno", "required when clusterPolicy.engine or workshopPolicy.engine is Kyverno"),
		Entry("a Kyverno cluster engine without the kyverno block",
			&configv1alpha1.PolicyEnforcement{
				ClusterPolicy:  configv1alpha1.ClusterPolicyConfig{Engine: configv1alpha1.ClusterPolicyEngineKyverno},
				WorkshopPolicy: configv1alpha1.WorkshopPolicyConfig{Engine: configv1alpha1.WorkshopPolicyEngineNone},
			},
			"spec.policyEnforcement.kyverno", "required when clusterPolicy.engine or workshopPolicy.engine is Kyverno"),
		Entry("the PodSecurityStandards cluster engine",
			&configv1alpha1.PolicyEnforcement{
				ClusterPolicy:  configv1alpha1.ClusterPolicyConfig{Engine: configv1alpha1.ClusterPolicyEnginePodSecurityStandards},
				WorkshopPolicy: configv1alpha1.WorkshopPolicyConfig{Engine: configv1alpha1.WorkshopPolicyEngineNone},
			},
			"spec.policyEnforcement.clusterPolicy.engine", `engine "PodSecurityStandards" is not yet supported`),
	)

	// Components read the engines from status, never the spec, so an
	// engine the spec sets to None must reach status for session-manager
	// to stop enforcing Kyverno policies.
	It("publishes the spec's policy engines in status once Ready", func() {
		root := testpki.NewRootCA()
		leaf := root.NewLeaf(testpki.WithDNSNames("*.educates.test"))
		Expect(k8sClient.Create(ctx, staticWildcardSecret(leaf.ChainPEM(), leaf.KeyPEM))).To(Succeed())
		Expect(k8sClient.Create(ctx, staticCASecret(root))).To(Succeed())

		Expect(k8sClient.Create(ctx, &configv1alpha1.EducatesClusterConfig{
			ObjectMeta: metav1.ObjectMeta{Name: "cluster"},
			Spec:       staticCertificateSpec(),
		})).To(Succeed())
		driveStaticCertificateToReady()

		got := &configv1alpha1.EducatesClusterConfig{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "cluster"}, got)).To(Succeed())
		Expect(got.Status.PolicyEnforcement).To(Equal(&configv1alpha1.StatusPolicyEnforcement{
			ClusterPolicyEngine:  configv1alpha1.ClusterPolicyEngineNone,
			WorkshopPolicyEngine: configv1alpha1.WorkshopPolicyEngineNone,
		}))
		// Reported as in Inline mode, so PolicyEnforcementReady is always
		// present once the policy engines have been dealt with.
		Expect(policyEnforcementReadyCondition()).To(And(
			Not(BeNil()),
			HaveField("Status", metav1.ConditionTrue),
			HaveField("Reason", "NotRequired")))
	})

	It("waits for an External Kyverno to be served before reaching Ready", func() {
		kyvernoDiscovery.SetServed(false)
		root := testpki.NewRootCA()
		leaf := root.NewLeaf(testpki.WithDNSNames("*.educates.test"))
		Expect(k8sClient.Create(ctx, staticWildcardSecret(leaf.ChainPEM(), leaf.KeyPEM))).To(Succeed())
		Expect(k8sClient.Create(ctx, staticCASecret(root))).To(Succeed())

		spec := staticCertificateSpec()
		spec.PolicyEnforcement = bundledKyvernoPolicyEnforcement()
		spec.PolicyEnforcement.Kyverno.Provider = configv1alpha1.KyvernoProviderExternal
		Expect(k8sClient.Create(ctx, &configv1alpha1.EducatesClusterConfig{
			ObjectMeta: metav1.ObjectMeta{Name: "cluster"},
			Spec:       spec,
		})).To(Succeed())

		Eventually(func() error {
			return k8sClient.Get(ctx, types.NamespacedName{Name: contourNamespace}, &corev1.Namespace{})
		}, 30*time.Second, 200*time.Millisecond).Should(Succeed())
		markDeploymentAvailable(contourControllerDeployment, contourNamespace)
		markDaemonSetReady()

		Eventually(policyEnforcementReadyCondition, 30*time.Second, 200*time.Millisecond).Should(And(
			Not(BeNil()),
			HaveField("Status", metav1.ConditionFalse),
			HaveField("Reason", "KyvernoNotFound"),
			HaveField("Message", ContainSubstring("the API server does not serve"))))
		// Ready carries the cause too, since only the administrator can
		// fix it and Ready is what educates admin platform deploy shows.
		got := &configv1alpha1.EducatesClusterConfig{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "cluster"}, got)).To(Succeed())
		Expect(meta.FindStatusCondition(got.Status.Conditions, conditionReady)).To(And(
			Not(BeNil()),
			HaveField("Status", metav1.ConditionFalse),
			HaveField("Reason", "KyvernoNotFound"),
			HaveField("Message", ContainSubstring("the API server does not serve"))))
		Expect(got.Status.Phase).To(Equal(configv1alpha1.ClusterConfigPhaseDegraded))
		expectNoReleaseInstalled(helmFac, kyvernoRelease)

		// The operator polls for it, since nothing it watches changes
		// when the administrator installs Kyverno.
		kyvernoDiscovery.SetServed(true)
		Eventually(readyConditionStatus, 45*time.Second, 200*time.Millisecond).
			Should(Equal(metav1.ConditionTrue))
		Expect(policyEnforcementReadyCondition()).To(And(
			HaveField("Status", metav1.ConditionTrue),
			HaveField("Reason", "ExternalKyvernoReady")))

		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "cluster"}, got)).To(Succeed())
		Expect(got.Status.PolicyEnforcement).To(Equal(&configv1alpha1.StatusPolicyEnforcement{
			ClusterPolicyEngine:  configv1alpha1.ClusterPolicyEngineKyverno,
			WorkshopPolicyEngine: configv1alpha1.WorkshopPolicyEngineKyverno,
		}))
		expectNoReleaseInstalled(helmFac, kyvernoRelease)
	})

	It("installs Kyverno and reaches Ready once its controllers are Available and its policy kinds are served", func() {
		// The controllers can be Available before Kyverno's CRDs are
		// established; start with the policy kinds not yet served.
		kyvernoDiscovery.SetServed(false)
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

		Eventually(policyEnforcementReadyCondition, 30*time.Second, 200*time.Millisecond).Should(And(
			Not(BeNil()),
			HaveField("Status", metav1.ConditionFalse),
			HaveField("Reason", "KyvernoNotFound")))
		Expect(readyConditionStatus()).NotTo(Equal(metav1.ConditionTrue))

		kyvernoDiscovery.SetServed(true)
		Eventually(readyConditionStatus, 45*time.Second, 200*time.Millisecond).
			Should(Equal(metav1.ConditionTrue))

		got := &configv1alpha1.EducatesClusterConfig{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "cluster"}, got)).To(Succeed())
		Expect(got.Status.Phase).To(Equal(configv1alpha1.ClusterConfigPhaseReady))
		Expect(got.Status.BundledChartVersions).To(HaveKeyWithValue("kyverno", vendoredcharts.KyvernoChartVersion))
		Expect(got.Status.PolicyEnforcement).To(Equal(&configv1alpha1.StatusPolicyEnforcement{
			ClusterPolicyEngine:  configv1alpha1.ClusterPolicyEngineKyverno,
			WorkshopPolicyEngine: configv1alpha1.WorkshopPolicyEngineKyverno,
		}))

		Expect(policyEnforcementReadyCondition()).To(And(
			HaveField("Status", metav1.ConditionTrue),
			HaveField("Reason", "BundledKyvernoReady")))
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
