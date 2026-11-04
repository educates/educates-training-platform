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

	releasecommon "helm.sh/helm/v4/pkg/release/common"
	release "helm.sh/helm/v4/pkg/release/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/retry"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	crconfig "sigs.k8s.io/controller-runtime/pkg/config"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	configv1alpha1 "github.com/educates/educates-training-platform/installer/operator/api/config/v1alpha1"
	platformv1alpha1 "github.com/educates/educates-training-platform/installer/operator/api/platform/v1alpha1"
	"github.com/educates/educates-training-platform/installer/operator/internal/helm"
	"github.com/educates/educates-training-platform/installer/operator/internal/kyverno/kyvernotest"
	"github.com/educates/educates-training-platform/installer/operator/internal/pki"
	"github.com/educates/educates-training-platform/installer/operator/internal/testpki"
	vendoredcharts "github.com/educates/educates-training-platform/installer/operator/vendored-charts"
)

// testOperatorNamespace is the operator namespace the SessionManager
// reconciler runs with, matching the namespace of the references the
// cluster config fixtures publish.
const testOperatorNamespace = "educates-installer"

// testCAOverrideNamespace holds CA override Secrets, a namespace other
// than the operator namespace.
const testCAOverrideNamespace = "test-ca-override"

// makeReadySecretsManager creates the SecretsManager singleton and
// stamps Ready=True directly via the status subresource, running the
// secrets-manager chart this operator embeds. Used as a fixture for
// SessionManager specs whose gate-2 depends on it.
func makeReadySecretsManager() {
	GinkgoHelper()
	makeReadySecretsManagerAt(vendoredcharts.SecretsManagerChartVersion)
}

// makeReadySecretsManagerAt is makeReadySecretsManager with the chart
// version the SecretsManager reports it runs, the state of a cluster whose
// secrets-manager release has not yet converged after an operator upgrade
// when the version is an older one.
func makeReadySecretsManagerAt(installedVersion string) {
	GinkgoHelper()
	sm := &platformv1alpha1.SecretsManager{
		ObjectMeta: metav1.ObjectMeta{Name: singletonName},
	}
	Expect(k8sClient.Create(ctx, sm)).To(Succeed())
	Expect(k8sClient.Get(ctx, types.NamespacedName{Name: singletonName}, sm)).To(Succeed())
	sm.Status = platformv1alpha1.SecretsManagerStatus{
		Phase:            platformv1alpha1.ComponentPhaseReady,
		InstalledVersion: installedVersion,
		Conditions: []metav1.Condition{{
			Type:               conditionReady,
			Status:             metav1.ConditionTrue,
			Reason:             "Reconciled",
			Message:            "test fixture",
			LastTransitionTime: metav1.Now(),
		}},
	}
	Expect(k8sClient.Status().Update(ctx, sm)).To(Succeed())
}

// setSecretsManagerInstalledVersion moves the SecretsManager singleton to
// report the given chart version, as its reconciler does once the
// secrets-manager release converges.
func setSecretsManagerInstalledVersion(installedVersion string) {
	GinkgoHelper()
	sm := &platformv1alpha1.SecretsManager{}
	Expect(k8sClient.Get(ctx, types.NamespacedName{Name: singletonName}, sm)).To(Succeed())
	sm.Status.InstalledVersion = installedVersion
	Expect(k8sClient.Status().Update(ctx, sm)).To(Succeed())
}

// updateSessionManagerSpec applies mutate to the SessionManager singleton's
// spec, fetching it again and retrying when the update conflicts with a
// status write the running reconciler made after the fetch.
func updateSessionManagerSpec(mutate func(*platformv1alpha1.SessionManagerSpec)) {
	GinkgoHelper()
	Expect(retry.RetryOnConflict(retry.DefaultRetry, func() error {
		smgr := &platformv1alpha1.SessionManager{}
		if err := k8sClient.Get(ctx, types.NamespacedName{Name: singletonName}, smgr); err != nil {
			return err
		}
		mutate(&smgr.Spec)
		return k8sClient.Update(ctx, smgr)
	})).To(Succeed())
}

// publishPolicyEnginesOnClusterConfig replaces the policy engines the
// cluster config fixture publishes.
func publishPolicyEnginesOnClusterConfig(cluster configv1alpha1.ClusterPolicyEngine, workshop configv1alpha1.WorkshopPolicyEngine) {
	GinkgoHelper()
	cc := &configv1alpha1.EducatesClusterConfig{}
	Expect(k8sClient.Get(ctx, types.NamespacedName{Name: configSingletonName}, cc)).To(Succeed())
	cc.Status.PolicyEnforcement = &configv1alpha1.StatusPolicyEnforcement{
		ClusterPolicyEngine:  cluster,
		WorkshopPolicyEngine: workshop,
	}
	Expect(k8sClient.Status().Update(ctx, cc)).To(Succeed())
}

func smgrReadyStatus() metav1.ConditionStatus {
	name := singletonName
	got := &platformv1alpha1.SessionManager{}
	if err := k8sClient.Get(ctx, types.NamespacedName{Name: name}, got); err != nil {
		return metav1.ConditionUnknown
	}
	c := meta.FindStatusCondition(got.Status.Conditions, conditionReady)
	if c == nil {
		return metav1.ConditionUnknown
	}
	return c.Status
}

func smgrConditionReason(condType string) string {
	name := singletonName
	got := &platformv1alpha1.SessionManager{}
	if err := k8sClient.Get(ctx, types.NamespacedName{Name: name}, got); err != nil {
		return ""
	}
	c := meta.FindStatusCondition(got.Status.Conditions, condType)
	if c == nil {
		return ""
	}
	return c.Reason
}

var _ = Describe("SessionManager reconciler", func() {
	var (
		mgrCancel context.CancelFunc
		mgrDone   chan error
		helmFac   *memoryHelmFactory
		// kyvernoDiscovery answers the Kyverno presence check; envtest
		// serves no Kyverno, so specs flip it to the case they test.
		kyvernoDiscovery *kyvernotest.Discovery
	)

	BeforeEach(func() {
		ensureNamespace(platformNamespace)
		ensureNamespace(testOperatorNamespace)
		ensureNamespace(testCAOverrideNamespace)
		helmFac = newMemoryHelmFactory()
		kyvernoDiscovery = &kyvernotest.Discovery{}

		var mgrCtx context.Context
		mgrCtx, mgrCancel = context.WithCancel(ctx)

		mgr, err := ctrl.NewManager(cfg, ctrl.Options{
			Scheme:     k8sClient.Scheme(),
			Metrics:    metricsserver.Options{BindAddress: "0"},
			Controller: crconfig.Controller{SkipNameValidation: new(true)},
		})
		Expect(err).NotTo(HaveOccurred())

		Expect((&SessionManagerReconciler{
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
		smgr := &platformv1alpha1.SessionManager{}
		if err := k8sClient.Get(ctx, types.NamespacedName{Name: singletonName}, smgr); err == nil {
			smgr.Finalizers = nil
			_ = k8sClient.Update(ctx, smgr)
			_ = k8sClient.Delete(ctx, smgr)
		}
		_ = k8sClient.DeleteAllOf(ctx, &platformv1alpha1.SecretsManager{})
		// LookupService specs create the singleton as part of the
		// remote-access Auto-presence-signal coverage; drain it so
		// the next spec starts with a clean cluster.
		ls := &platformv1alpha1.LookupService{}
		if err := k8sClient.Get(ctx, types.NamespacedName{Name: singletonName}, ls); err == nil {
			ls.Finalizers = nil
			_ = k8sClient.Update(ctx, ls)
			_ = k8sClient.Delete(ctx, ls)
		}
		_ = k8sClient.DeleteAllOf(ctx, &configv1alpha1.EducatesClusterConfig{})
		_ = k8sClient.DeleteAllOf(ctx, &appsv1.Deployment{}, client.InNamespace(platformNamespace))
		_ = k8sClient.DeleteAllOf(ctx, &corev1.Secret{}, client.InNamespace(testOperatorNamespace))
		_ = k8sClient.DeleteAllOf(ctx, &corev1.Secret{}, client.InNamespace(testCAOverrideNamespace))
	})

	// caCertificateRefValue returns a poller for the
	// clusterIngress.caCertificateRef value of the named release, which
	// yields nil while there is no release.
	caCertificateRefValue := func(releaseName string) func() any {
		return func() any {
			hc, err := helmFac.For(platformNamespace)
			if err != nil {
				return nil
			}
			rel, err := hc.Status(releaseName)
			if err != nil {
				return nil
			}
			ci, _ := rel.Config["clusterIngress"].(map[string]any)
			return ci["caCertificateRef"]
		}
	}
	smCACertificateRefValue := caCertificateRefValue(sessionManagerReleaseName)

	// overrideCAData returns the data of the key-free copy of the CA
	// override, or nil while it does not exist.
	overrideCAData := func() map[string][]byte {
		s := &corev1.Secret{}
		key := types.NamespacedName{Namespace: testOperatorNamespace, Name: sessionManagerIngressCASecretName}
		if err := k8sClient.Get(ctx, key, s); err != nil {
			return nil
		}
		return s.Data
	}

	withCAOverride := func(name string) *platformv1alpha1.SessionManager {
		return &platformv1alpha1.SessionManager{
			ObjectMeta: metav1.ObjectMeta{Name: singletonName},
			Spec: platformv1alpha1.SessionManagerSpec{
				IngressOverrides: &platformv1alpha1.IngressOverrides{
					CACertificateSecretRef: &platformv1alpha1.SecretReference{
						Name:      name,
						Namespace: testCAOverrideNamespace,
					},
				},
			},
		}
	}

	// sessionManagerReleaseInstalled reports whether the session-manager
	// release exists.
	sessionManagerReleaseInstalled := func() bool {
		hc, err := helmFac.For(platformNamespace)
		Expect(err).NotTo(HaveOccurred())
		_, err = hc.Status(sessionManagerReleaseName)
		return err == nil
	}

	// Workshop security is enforced by Kyverno: the chart installs Kyverno
	// policies and the runtime creates them per workshop, so whenever an
	// effective engine is Kyverno, whether the cluster's or this
	// SessionManager's override, session-manager waits for Kyverno.
	DescribeTable("refuses to install while an effective Kyverno engine has no Kyverno",
		func(clusterWorkshopEngine configv1alpha1.WorkshopPolicyEngine, override *platformv1alpha1.WorkshopPolicyOverride) {
			kyvernoDiscovery.SetServed(false)
			makeReadyClusterConfig()
			publishPolicyEnginesOnClusterConfig(configv1alpha1.ClusterPolicyEngineNone, clusterWorkshopEngine)
			makeReadySecretsManager()
			Expect(k8sClient.Create(ctx, &platformv1alpha1.SessionManager{
				ObjectMeta: metav1.ObjectMeta{Name: singletonName},
				Spec:       platformv1alpha1.SessionManagerSpec{WorkshopPolicyOverride: override},
			})).To(Succeed())

			Eventually(func() string {
				return smgrConditionReason(conditionKyvernoAvailable)
			}, 30*time.Second, 200*time.Millisecond).Should(Equal("KyvernoNotFound"))
			Expect(smgrConditionReason(conditionReady)).To(Equal("WaitingForKyverno"))
			Expect(smgrReadyStatus()).To(Equal(metav1.ConditionFalse))
			Expect(sessionManagerReleaseInstalled()).To(BeFalse())

			// Nothing the reconciler watches changes when Kyverno is
			// installed, so it polls.
			kyvernoDiscovery.SetServed(true)
			Eventually(sessionManagerReleaseInstalled, 45*time.Second, 200*time.Millisecond).Should(BeTrue())
			// The release is installed before the reconcile writes status.
			Eventually(func() string {
				return smgrConditionReason(conditionKyvernoAvailable)
			}, 10*time.Second, 200*time.Millisecond).Should(Equal("KyvernoServed"))
		},
		Entry("the cluster's workshop engine", configv1alpha1.WorkshopPolicyEngineKyverno, nil),
		Entry("an override to Kyverno on a cluster without workshop security", configv1alpha1.WorkshopPolicyEngineNone,
			&platformv1alpha1.WorkshopPolicyOverride{Engine: platformv1alpha1.WorkshopPolicyEngineKyverno}),
	)

	It("installs without looking for Kyverno when every effective engine is None", func() {
		kyvernoDiscovery.SetServed(false)
		makeReadyClusterConfig()
		publishPolicyEnginesOnClusterConfig(configv1alpha1.ClusterPolicyEngineNone, configv1alpha1.WorkshopPolicyEngineKyverno)
		makeReadySecretsManager()
		Expect(k8sClient.Create(ctx, &platformv1alpha1.SessionManager{
			ObjectMeta: metav1.ObjectMeta{Name: singletonName},
			Spec: platformv1alpha1.SessionManagerSpec{
				WorkshopPolicyOverride: &platformv1alpha1.WorkshopPolicyOverride{Engine: platformv1alpha1.WorkshopPolicyEngineNone},
			},
		})).To(Succeed())

		Eventually(sessionManagerReleaseInstalled, 30*time.Second, 200*time.Millisecond).Should(BeTrue())
		// The release is installed before the reconcile writes status.
		Eventually(func() string {
			return smgrConditionReason(conditionKyvernoAvailable)
		}, 10*time.Second, 200*time.Millisecond).Should(Equal("NotRequired"))
	})

	It("derives a key-free copy of a CA override in another namespace and points the chart values at it", func() {
		makeReadyClusterConfig()
		makeReadySecretsManager()
		publishIngressCAOnClusterConfig()
		ca := testpki.NewRootCA()
		Expect(k8sClient.Create(ctx, &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: "override-ca", Namespace: testCAOverrideNamespace},
			Data: map[string][]byte{
				"ca.crt":  ca.CertPEM,
				"tls.crt": ca.CertPEM,
				"tls.key": ca.KeyPEM,
			},
		})).To(Succeed())

		Expect(k8sClient.Create(ctx, withCAOverride("override-ca"))).To(Succeed())

		Eventually(smCACertificateRefValue, 30*time.Second, 200*time.Millisecond).Should(Equal(map[string]any{
			"name":      sessionManagerIngressCASecretName,
			"namespace": testOperatorNamespace,
		}))
		Expect(overrideCAData()).To(Equal(map[string][]byte{"ca.crt": ca.CertPEM}))

		derived := &corev1.Secret{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Namespace: testOperatorNamespace, Name: sessionManagerIngressCASecretName}, derived)).To(Succeed())
		Expect(derived.Type).To(Equal(corev1.SecretTypeOpaque))
		Expect(derived.OwnerReferences).To(HaveLen(1))
		Expect(derived.OwnerReferences[0].Kind).To(Equal("SessionManager"))
		Expect(derived.OwnerReferences[0].Name).To(Equal(singletonName))
	})

	It("refreshes the key-free copy when the CA override's Secret changes", func() {
		makeReadyClusterConfig()
		makeReadySecretsManager()
		first, second := testpki.NewRootCA(), testpki.NewRootCA()
		source := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: "override-ca", Namespace: testCAOverrideNamespace},
			Data:       map[string][]byte{"ca.crt": first.CertPEM},
		}
		Expect(k8sClient.Create(ctx, source)).To(Succeed())
		Expect(k8sClient.Create(ctx, withCAOverride("override-ca"))).To(Succeed())
		Eventually(overrideCAData, 30*time.Second, 200*time.Millisecond).
			Should(Equal(map[string][]byte{"ca.crt": first.CertPEM}))

		// No spec change and less than the reconciler's own requeue
		// delay: only the Secret watch can carry this through.
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(source), source)).To(Succeed())
		source.Data = map[string][]byte{"ca.crt": second.CertPEM}
		Expect(k8sClient.Update(ctx, source)).To(Succeed())

		Eventually(overrideCAData, 10*time.Second, 200*time.Millisecond).
			Should(Equal(map[string][]byte{"ca.crt": second.CertPEM}))
	})

	It("refuses a CA override whose Secret is missing, with a specific reason", func() {
		makeReadyClusterConfig()
		makeReadySecretsManager()

		Expect(k8sClient.Create(ctx, withCAOverride("missing-ca"))).To(Succeed())

		Eventually(func() string {
			return smgrConditionReason(conditionReady)
		}, 30*time.Second, 200*time.Millisecond).Should(Equal(pki.ReasonCATrustSecretNotFound))
		Expect(overrideCAData()).To(BeNil())

		hc, err := helmFac.For(platformNamespace)
		Expect(err).NotTo(HaveOccurred())
		_, statusErr := hc.Status(sessionManagerReleaseName)
		Expect(statusErr).To(MatchError(helm.ErrReleaseNotFound))
	})

	It("drops the key-free copy and returns to the cluster CA when the override is removed", func() {
		makeReadyClusterConfig()
		makeReadySecretsManager()
		publishIngressCAOnClusterConfig()
		ca := testpki.NewRootCA()
		Expect(k8sClient.Create(ctx, &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: "override-ca", Namespace: testCAOverrideNamespace},
			Data:       map[string][]byte{"ca.crt": ca.CertPEM},
		})).To(Succeed())
		Expect(k8sClient.Create(ctx, withCAOverride("override-ca"))).To(Succeed())
		Eventually(overrideCAData, 30*time.Second, 200*time.Millisecond).ShouldNot(BeNil())

		updateSessionManagerSpec(func(spec *platformv1alpha1.SessionManagerSpec) {
			spec.IngressOverrides = nil
		})

		Eventually(overrideCAData, 30*time.Second, 200*time.Millisecond).Should(BeNil())
		Eventually(smCACertificateRefValue, 30*time.Second, 200*time.Millisecond).
			Should(Equal(ingressCAChartRef()))
	})

	It("refuses when EducatesClusterConfig is missing", func() {
		smgr := &platformv1alpha1.SessionManager{
			ObjectMeta: metav1.ObjectMeta{Name: singletonName},
		}
		Expect(k8sClient.Create(ctx, smgr)).To(Succeed())

		Eventually(func() string {
			return smgrConditionReason(conditionClusterConfigAvailable)
		}, 30*time.Second, 200*time.Millisecond).Should(Equal("ClusterConfigNotReady"))

		Expect(smgrReadyStatus()).To(Equal(metav1.ConditionFalse))
	})

	It("refuses when SecretsManager is not Ready (ECC Ready, SM missing)", func() {
		makeReadyClusterConfig()

		smgr := &platformv1alpha1.SessionManager{
			ObjectMeta: metav1.ObjectMeta{Name: singletonName},
		}
		Expect(k8sClient.Create(ctx, smgr)).To(Succeed())

		// ClusterConfigAvailable should flip True; SecretsManagerAvailable
		// stays False because no SecretsManager CR exists yet.
		Eventually(func() string {
			return smgrConditionReason(conditionClusterConfigAvailable)
		}, 30*time.Second, 200*time.Millisecond).Should(Equal("ClusterConfigReady"))

		Eventually(func() string {
			return smgrConditionReason(conditionSecretsManagerAvailable)
		}, 30*time.Second, 200*time.Millisecond).Should(Equal("SecretsManagerNotReady"))

		Expect(smgrReadyStatus()).To(Equal(metav1.ConditionFalse))
	})

	It("waits for SecretsManager to run the embedded secrets-manager chart before installing", func() {
		// After an operator upgrade SecretsManager stays Ready from the
		// previous release until its own release converges, and only then
		// carries the secrets.educates.dev CRD schemas session-manager's
		// SecretCopier and SecretInjector resources are written against.
		makeReadyClusterConfig()
		makeReadySecretsManagerAt("0.0.0-previous")

		smgr := &platformv1alpha1.SessionManager{
			ObjectMeta: metav1.ObjectMeta{Name: singletonName},
		}
		Expect(k8sClient.Create(ctx, smgr)).To(Succeed())

		Eventually(func() string {
			return smgrConditionReason(conditionSecretsManagerAvailable)
		}, 30*time.Second, 200*time.Millisecond).Should(Equal("SecretsManagerNotUpgraded"))
		Expect(smgrReadyStatus()).To(Equal(metav1.ConditionFalse))
		hc, err := helmFac.For(platformNamespace)
		Expect(err).NotTo(HaveOccurred())
		_, err = hc.Status(sessionManagerReleaseName)
		Expect(err).To(MatchError(helm.ErrReleaseNotFound))

		setSecretsManagerInstalledVersion(vendoredcharts.SecretsManagerChartVersion)

		Eventually(func() error {
			_, err := hc.Status(sessionManagerReleaseName)
			return err
		}, 30*time.Second, 200*time.Millisecond).Should(Succeed())
		// The release is installed before the reconcile writes status.
		Eventually(func() string {
			return smgrConditionReason(conditionSecretsManagerAvailable)
		}, 10*time.Second, 200*time.Millisecond).Should(Equal("SecretsManagerReady"))
	})

	It("installs the chart and reaches Ready when both gates pass + Deployment Available", func() {
		makeReadyClusterConfig()
		makeReadySecretsManager()

		smgr := &platformv1alpha1.SessionManager{
			ObjectMeta: metav1.ObjectMeta{Name: singletonName},
			Spec:       platformv1alpha1.SessionManagerSpec{},
		}
		Expect(k8sClient.Create(ctx, smgr)).To(Succeed())

		Eventually(func() error {
			hc, err := helmFac.For(platformNamespace)
			if err != nil {
				return err
			}
			_, err = hc.Status(sessionManagerReleaseName)
			return err
		}, 30*time.Second, 200*time.Millisecond).Should(Succeed())

		markDeploymentAvailable(sessionManagerDeploymentName)

		Eventually(func() metav1.ConditionStatus {
			return smgrReadyStatus()
		}, 30*time.Second, 200*time.Millisecond).Should(Equal(metav1.ConditionTrue))

		got := &platformv1alpha1.SessionManager{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: singletonName}, got)).To(Succeed())
		Expect(got.Status.Phase).To(Equal(platformv1alpha1.ComponentPhaseReady))
		Expect(got.Status.InstalledVersion).To(Equal(vendoredcharts.SessionManagerChartVersion))
		Expect(got.Status.DeploymentRef).NotTo(BeNil())
		Expect(got.Status.DeploymentRef.Namespace).To(Equal(platformNamespace))
		Expect(got.Status.DeploymentRef.Name).To(Equal(sessionManagerDeploymentName))
	})

	It("upgrades the subchart's stale CRDs to the chart's schemas", func() {
		chrt, err := vendoredcharts.SessionManager()
		Expect(err).NotTo(HaveOccurred())
		writeStaleCRDs(chrt)

		makeReadyClusterConfig()
		makeReadySecretsManager()
		smgr := &platformv1alpha1.SessionManager{
			ObjectMeta: metav1.ObjectMeta{Name: singletonName},
			Spec:       platformv1alpha1.SessionManagerSpec{},
		}
		Expect(k8sClient.Create(ctx, smgr)).To(Succeed())

		expectChartCRDsApplied(chrt)
	})

	It("renders Secret-sourced themes and the imagePrePuller toggle into chart values", func() {
		makeReadyClusterConfig()
		makeReadySecretsManager()

		smgr := &platformv1alpha1.SessionManager{
			ObjectMeta: metav1.ObjectMeta{Name: singletonName},
			Spec: platformv1alpha1.SessionManagerSpec{
				Themes: []platformv1alpha1.Theme{
					{
						Name: "corporate",
						Source: platformv1alpha1.ThemeSource{
							Type: platformv1alpha1.ThemeSourceTypeSecret,
							SecretRef: &platformv1alpha1.NamespacedObjectReference{
								Name:      "corporate-theme",
								Namespace: "themes-source",
							},
						},
					},
				},
				DefaultTheme:   "corporate",
				ImagePrePuller: &platformv1alpha1.ImagePrePuller{Enabled: true},
			},
		}
		Expect(k8sClient.Create(ctx, smgr)).To(Succeed())

		var rel *release.Release
		Eventually(func() error {
			hc, err := helmFac.For(platformNamespace)
			if err != nil {
				return err
			}
			rel, err = hc.Status(sessionManagerReleaseName)
			return err
		}, 30*time.Second, 200*time.Millisecond).Should(Succeed())

		styling, ok := rel.Config["websiteStyling"].(map[string]any)
		Expect(ok).To(BeTrue(), "websiteStyling missing from rendered values")
		// The chart keys themes by Secret name; the CR-level theme name
		// translates to its backing Secret.
		Expect(styling["defaultTheme"]).To(Equal("corporate-theme"))
		refs, ok := styling["themeDataRefs"].([]any)
		Expect(ok).To(BeTrue())
		Expect(refs).To(HaveLen(1))
		Expect(refs[0]).To(Equal(map[string]any{
			"name":      "corporate-theme",
			"namespace": "themes-source",
		}))

		prePuller, ok := rel.Config["imagePrePuller"].(map[string]any)
		Expect(ok).To(BeTrue(), "imagePrePuller missing from rendered values")
		Expect(prePuller["enabled"]).To(BeTrue())
	})

	It("reports an image pre-puller name the chart's inventory lacks as InstallFailed", func() {
		makeReadyClusterConfig()
		makeReadySecretsManager()

		smgr := &platformv1alpha1.SessionManager{
			ObjectMeta: metav1.ObjectMeta{Name: singletonName},
			Spec: platformv1alpha1.SessionManagerSpec{
				ImagePrePuller: &platformv1alpha1.ImagePrePuller{
					Enabled: true,
					Images: []platformv1alpha1.PrePulledImage{
						{Name: "jdk22-environment"},
					},
				},
			},
		}
		Expect(k8sClient.Create(ctx, smgr)).To(Succeed())

		Eventually(func() string {
			return smgrConditionReason(conditionReady)
		}, 30*time.Second, 200*time.Millisecond).Should(Equal("InstallFailed"))

		got := &platformv1alpha1.SessionManager{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: singletonName}, got)).To(Succeed())
		cond := meta.FindStatusCondition(got.Status.Conditions, conditionReady)
		Expect(cond.Status).To(Equal(metav1.ConditionFalse))
		Expect(cond.Message).To(ContainSubstring("imagePrePuller.images[0]"))
		Expect(cond.Message).To(ContainSubstring(`"jdk22-environment"`))

		// The failed render stores no release.
		hc, err := helmFac.For(platformNamespace)
		Expect(err).NotTo(HaveOccurred())
		_, statusErr := hc.Status(sessionManagerReleaseName)
		Expect(statusErr).To(MatchError(helm.ErrReleaseNotFound))
	})

	It("threads the ingressOverrides protocol assertion into chart values", func() {
		makeReadyClusterConfig()
		makeReadySecretsManager()

		smgr := &platformv1alpha1.SessionManager{
			ObjectMeta: metav1.ObjectMeta{Name: singletonName},
			Spec: platformv1alpha1.SessionManagerSpec{
				IngressOverrides: &platformv1alpha1.IngressOverrides{Protocol: "https"},
			},
		}
		Expect(k8sClient.Create(ctx, smgr)).To(Succeed())

		var rel *release.Release
		Eventually(func() error {
			hc, err := helmFac.For(platformNamespace)
			if err != nil {
				return err
			}
			rel, err = hc.Status(sessionManagerReleaseName)
			return err
		}, 30*time.Second, 200*time.Millisecond).Should(Succeed())

		ci, ok := rel.Config["clusterIngress"].(map[string]any)
		Expect(ok).To(BeTrue(), "clusterIngress missing from rendered values")
		Expect(ci["protocol"]).To(Equal("https"))
	})

	It("rejects reserved-but-unsupported spec surface with field-specific validation errors", func() {
		makeReadyClusterConfig()
		makeReadySecretsManager()

		smgr := &platformv1alpha1.SessionManager{
			ObjectMeta: metav1.ObjectMeta{Name: singletonName},
			Spec: platformv1alpha1.SessionManagerSpec{
				DefaultAccessCredentials: &platformv1alpha1.DefaultAccessCredentials{
					Username: "educates",
				},
			},
		}
		Expect(k8sClient.Create(ctx, smgr)).To(Succeed())

		Eventually(func() string {
			return smgrConditionReason(conditionReady)
		}, 30*time.Second, 200*time.Millisecond).Should(Equal("ValidationFailed"))

		got := &platformv1alpha1.SessionManager{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: singletonName}, got)).To(Succeed())
		Expect(got.Status.Phase).To(Equal(platformv1alpha1.ComponentPhaseDegraded))
		cond := meta.FindStatusCondition(got.Status.Conditions, conditionReady)
		Expect(cond.Message).To(ContainSubstring("spec.defaultAccessCredentials"))
		Expect(cond.Message).To(ContainSubstring("not yet supported"))

		// Validation refusals must not install the chart.
		hc, err := helmFac.For(platformNamespace)
		Expect(err).NotTo(HaveOccurred())
		_, statusErr := hc.Status(sessionManagerReleaseName)
		Expect(statusErr).To(MatchError(helm.ErrReleaseNotFound))

		// A ConfigMap-sourced theme is refused the same way after the
		// spec is corrected to drop the credentials block.
		got.Spec.DefaultAccessCredentials = nil
		got.Spec.Themes = []platformv1alpha1.Theme{
			{
				Name: "corporate",
				Source: platformv1alpha1.ThemeSource{
					Type: platformv1alpha1.ThemeSourceTypeConfigMap,
					ConfigMapRef: &platformv1alpha1.NamespacedObjectReference{
						Name:      "corporate-theme",
						Namespace: "themes-source",
					},
				},
			},
		}
		Expect(k8sClient.Update(ctx, got)).To(Succeed())

		Eventually(func() string {
			latest := &platformv1alpha1.SessionManager{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: singletonName}, latest); err != nil {
				return ""
			}
			cond := meta.FindStatusCondition(latest.Status.Conditions, conditionReady)
			if cond == nil {
				return ""
			}
			return cond.Message
		}, 30*time.Second, 200*time.Millisecond).Should(ContainSubstring("spec.themes[0].source.type"))
	})

	It("uninstalls the chart on delete", func() {
		makeReadyClusterConfig()
		makeReadySecretsManager()
		smgr := &platformv1alpha1.SessionManager{
			ObjectMeta: metav1.ObjectMeta{Name: singletonName},
		}
		Expect(k8sClient.Create(ctx, smgr)).To(Succeed())

		Eventually(func() error {
			hc, err := helmFac.For(platformNamespace)
			if err != nil {
				return err
			}
			_, err = hc.Status(sessionManagerReleaseName)
			return err
		}, 30*time.Second, 200*time.Millisecond).Should(Succeed())
		markDeploymentAvailable(sessionManagerDeploymentName)
		Eventually(func() metav1.ConditionStatus {
			return smgrReadyStatus()
		}, 30*time.Second, 200*time.Millisecond).Should(Equal(metav1.ConditionTrue))

		Expect(k8sClient.Delete(ctx, smgr)).To(Succeed())

		Eventually(func() error {
			hc, err := helmFac.For(platformNamespace)
			if err != nil {
				return err
			}
			_, statusErr := hc.Status(sessionManagerReleaseName)
			return statusErr
		}, 30*time.Second, 200*time.Millisecond).Should(MatchError(helm.ErrReleaseNotFound))

		Eventually(func() bool {
			err := k8sClient.Get(ctx, types.NamespacedName{Name: singletonName}, &platformv1alpha1.SessionManager{})
			return apierrors.IsNotFound(err)
		}, 30*time.Second, 200*time.Millisecond).Should(BeTrue())
	})

	// --- Optional extras: nodeCATrust + remoteAccess --------------

	// withCAOnClusterConfig augments the shared fixture with the CA
	// trust Secret a custom CA cluster publishes, so
	// resolveNodeCATrust(Auto/Enabled) treats the prerequisite as
	// satisfied.
	withCAOnClusterConfig := publishIngressCAOnClusterConfig

	driveSessionManagerReady := func(smgr *platformv1alpha1.SessionManager) {
		GinkgoHelper()
		Expect(k8sClient.Create(ctx, smgr)).To(Succeed())
		Eventually(func() error {
			hc, err := helmFac.For(platformNamespace)
			if err != nil {
				return err
			}
			_, err = hc.Status(sessionManagerReleaseName)
			return err
		}, 30*time.Second, 200*time.Millisecond).Should(Succeed())
		markDeploymentAvailable(sessionManagerDeploymentName)
	}

	extraConditionReason := func(condType string) string {
		got := &platformv1alpha1.SessionManager{}
		if err := k8sClient.Get(ctx, types.NamespacedName{Name: singletonName}, got); err != nil {
			return ""
		}
		c := meta.FindStatusCondition(got.Status.Conditions, condType)
		if c == nil {
			return ""
		}
		return c.Reason
	}

	It("nodeCATrust Auto installs when ECC publishes a CA cert ref", func() {
		makeReadyClusterConfig()
		makeReadySecretsManager()
		withCAOnClusterConfig()

		driveSessionManagerReady(&platformv1alpha1.SessionManager{
			ObjectMeta: metav1.ObjectMeta{Name: singletonName},
		})

		var rel *release.Release
		Eventually(func() error {
			hc, err := helmFac.For(platformNamespace)
			if err != nil {
				return err
			}
			rel, err = hc.Status(nodeCAInjectorReleaseName)
			return err
		}, 30*time.Second, 200*time.Millisecond).Should(Succeed())
		Eventually(func() string {
			return extraConditionReason(conditionNodeCATrustDeployed)
		}, 30*time.Second, 200*time.Millisecond).Should(Equal("Installed"))

		// The injector reads the key-free CA trust Secret, with its namespace.
		ci, ok := rel.Config["clusterIngress"].(map[string]any)
		Expect(ok).To(BeTrue(), "clusterIngress missing from node-ca-injector values")
		Expect(ci["caCertificateRef"]).To(Equal(ingressCAChartRef()))
	})

	nodeCAInjectorCACertificateRefValue := caCertificateRefValue(nodeCAInjectorReleaseName)

	// createOverrideCASecret creates the CA trust Secret "override-ca"
	// that withCAOverride("override-ca") names.
	createOverrideCASecret := func() {
		GinkgoHelper()
		Expect(k8sClient.Create(ctx, &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: "override-ca", Namespace: testCAOverrideNamespace},
			Data:       map[string][]byte{"ca.crt": testpki.NewRootCA().CertPEM},
		})).To(Succeed())
	}

	overrideCAChartRef := map[string]any{
		"name":      sessionManagerIngressCASecretName,
		"namespace": testOperatorNamespace,
	}

	It("nodeCATrust Auto installs with the CA override's key-free copy when the cluster has no CA", func() {
		makeReadyClusterConfig() // no CA on the fixture
		makeReadySecretsManager()
		createOverrideCASecret()

		driveSessionManagerReady(withCAOverride("override-ca"))

		Eventually(nodeCAInjectorCACertificateRefValue, 30*time.Second, 200*time.Millisecond).
			Should(Equal(overrideCAChartRef))
		Eventually(func() string {
			return extraConditionReason(conditionNodeCATrustDeployed)
		}, 30*time.Second, 200*time.Millisecond).Should(Equal("Installed"))
	})

	It("nodeCATrust trusts only the CA override when the cluster also has a CA", func() {
		makeReadyClusterConfig()
		makeReadySecretsManager()
		withCAOnClusterConfig()
		createOverrideCASecret()

		driveSessionManagerReady(withCAOverride("override-ca"))

		Eventually(nodeCAInjectorCACertificateRefValue, 30*time.Second, 200*time.Millisecond).
			Should(Equal(overrideCAChartRef))
	})

	It("nodeCATrust returns to the cluster CA when the CA override is removed", func() {
		makeReadyClusterConfig()
		makeReadySecretsManager()
		withCAOnClusterConfig()
		createOverrideCASecret()
		driveSessionManagerReady(withCAOverride("override-ca"))
		Eventually(nodeCAInjectorCACertificateRefValue, 30*time.Second, 200*time.Millisecond).
			Should(Equal(overrideCAChartRef))

		updateSessionManagerSpec(func(spec *platformv1alpha1.SessionManagerSpec) {
			spec.IngressOverrides = nil
		})

		Eventually(nodeCAInjectorCACertificateRefValue, 30*time.Second, 200*time.Millisecond).
			Should(Equal(ingressCAChartRef()))
	})

	It("nodeCATrust Enabled installs with only a CA override", func() {
		makeReadyClusterConfig() // no CA on the fixture
		makeReadySecretsManager()
		createOverrideCASecret()
		smgr := withCAOverride("override-ca")
		smgr.Spec.NodeCATrust = &platformv1alpha1.NodeCATrust{Mode: platformv1alpha1.ComponentModeEnabled}

		driveSessionManagerReady(smgr)

		Eventually(func() string {
			return extraConditionReason(conditionNodeCATrustDeployed)
		}, 30*time.Second, 200*time.Millisecond).Should(Equal("Installed"))
		Eventually(nodeCAInjectorCACertificateRefValue, 30*time.Second, 200*time.Millisecond).
			Should(Equal(overrideCAChartRef))
	})

	It("nodeCATrust Disabled installs nothing even with a CA override", func() {
		makeReadyClusterConfig()
		makeReadySecretsManager()
		createOverrideCASecret()
		smgr := withCAOverride("override-ca")
		smgr.Spec.NodeCATrust = &platformv1alpha1.NodeCATrust{Mode: platformv1alpha1.ComponentModeDisabled}

		driveSessionManagerReady(smgr)

		Eventually(func() string {
			return extraConditionReason(conditionNodeCATrustDeployed)
		}, 30*time.Second, 200*time.Millisecond).Should(Equal("ModeDisabled"))
		hc, err := helmFac.For(platformNamespace)
		Expect(err).NotTo(HaveOccurred())
		_, statusErr := hc.Status(nodeCAInjectorReleaseName)
		Expect(statusErr).To(MatchError(helm.ErrReleaseNotFound))
	})

	It("carries the cluster's CA trust Secret, with its namespace, into chart values", func() {
		makeReadyClusterConfig()
		makeReadySecretsManager()
		withCAOnClusterConfig()

		Expect(k8sClient.Create(ctx, &platformv1alpha1.SessionManager{
			ObjectMeta: metav1.ObjectMeta{Name: singletonName},
		})).To(Succeed())

		var rel *release.Release
		Eventually(func() error {
			hc, err := helmFac.For(platformNamespace)
			if err != nil {
				return err
			}
			rel, err = hc.Status(sessionManagerReleaseName)
			return err
		}, 30*time.Second, 200*time.Millisecond).Should(Succeed())

		ci, ok := rel.Config["clusterIngress"].(map[string]any)
		Expect(ok).To(BeTrue(), "clusterIngress missing from rendered values")
		Expect(ci["caCertificateRef"]).To(Equal(ingressCAChartRef()))
	})

	It("nodeCATrust Auto skips when ECC has no CA cert ref", func() {
		makeReadyClusterConfig() // no CA on the fixture
		makeReadySecretsManager()

		driveSessionManagerReady(&platformv1alpha1.SessionManager{
			ObjectMeta: metav1.ObjectMeta{Name: singletonName},
		})

		Eventually(func() string {
			return extraConditionReason(conditionNodeCATrustDeployed)
		}, 30*time.Second, 200*time.Millisecond).Should(Equal("ModeAutoNoCA"))

		hc, err := helmFac.For(platformNamespace)
		Expect(err).NotTo(HaveOccurred())
		_, statusErr := hc.Status(nodeCAInjectorReleaseName)
		Expect(statusErr).To(MatchError(helm.ErrReleaseNotFound))
	})

	It("nodeCATrust Enabled refuses when ECC has no CA cert ref", func() {
		makeReadyClusterConfig()
		makeReadySecretsManager()

		driveSessionManagerReady(&platformv1alpha1.SessionManager{
			ObjectMeta: metav1.ObjectMeta{Name: singletonName},
			Spec: platformv1alpha1.SessionManagerSpec{
				NodeCATrust: &platformv1alpha1.NodeCATrust{
					Mode: platformv1alpha1.ComponentModeEnabled,
				},
			},
		})

		Eventually(func() string {
			return extraConditionReason(conditionNodeCATrustDeployed)
		}, 30*time.Second, 200*time.Millisecond).Should(Equal("NodeCATrustMissingCA"))

		// Aggregate Ready should be False with reason ExtraRefused.
		Eventually(func() metav1.ConditionStatus {
			return smgrReadyStatus()
		}, 30*time.Second, 200*time.Millisecond).Should(Equal(metav1.ConditionFalse))
		Expect(smgrConditionReason(conditionReady)).To(Equal("ExtraRefused"))
	})

	It("surfaces a held-failed extra release as not-ready instead of installed", func() {
		makeReadyClusterConfig()
		makeReadySecretsManager()
		withCAOnClusterConfig() // nodeCATrust Auto → intentInstall

		// Stage a failed node-ca-injector release whose inputs match what
		// the reconciler will render, so EnsureRelease holds it rather than
		// reinstalling. Seed before the SessionManager exists so the extra
		// is already failed when the reconciler reaches the extras step.
		cc := &configv1alpha1.EducatesClusterConfig{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: configSingletonName}, cc)).To(Succeed())
		smObj := &platformv1alpha1.SessionManager{
			ObjectMeta: metav1.ObjectMeta{Name: singletonName},
		}
		chrt, err := vendoredcharts.NodeCAInjector()
		Expect(err).NotTo(HaveOccurred())
		hc, err := helmFac.For(platformNamespace)
		Expect(err).NotTo(HaveOccurred())
		Expect(hc.SeedRelease(
			nodeCAInjectorReleaseName, 1, releasecommon.StatusFailed,
			chrt, renderNodeCAInjectorValues(smObj, cc, testOperatorNamespace),
			"node-ca-injector DaemonSet is invalid: spec.template.spec.containers: Required value",
		)).To(Succeed())

		driveSessionManagerReady(smObj)

		// Per-extra condition surfaces the Helm failure.
		Eventually(func() string {
			return extraConditionReason(conditionNodeCATrustDeployed)
		}, 30*time.Second, 200*time.Millisecond).Should(Equal("ReleaseFailed"))

		// Aggregate Ready demoted to False/ExtraNotReady (not Installed).
		Eventually(func() metav1.ConditionStatus {
			return smgrReadyStatus()
		}, 30*time.Second, 200*time.Millisecond).Should(Equal(metav1.ConditionFalse))
		Expect(smgrConditionReason(conditionReady)).To(Equal("ExtraNotReady"))

		// The release is held, not churned: still the single failed revision.
		live, err := hc.Status(nodeCAInjectorReleaseName)
		Expect(err).NotTo(HaveOccurred())
		Expect(live.Version).To(Equal(1))
		Expect(live.Info.Status).To(Equal(releasecommon.StatusFailed))
	})

	It("remoteAccess Auto installs when a LookupService CR exists", func() {
		makeReadyClusterConfig()
		makeReadySecretsManager()
		// Create the LookupService CR (presence is the Auto signal —
		// readiness isn't part of the signal).
		Expect(k8sClient.Create(ctx, &platformv1alpha1.LookupService{
			ObjectMeta: metav1.ObjectMeta{Name: singletonName},
			Spec: platformv1alpha1.LookupServiceSpec{
				Ingress: platformv1alpha1.LookupServiceIngress{Prefix: "lookup"},
			},
		})).To(Succeed())

		driveSessionManagerReady(&platformv1alpha1.SessionManager{
			ObjectMeta: metav1.ObjectMeta{Name: singletonName},
		})

		Eventually(func() error {
			hc, err := helmFac.For(platformNamespace)
			if err != nil {
				return err
			}
			_, err = hc.Status(remoteAccessReleaseName)
			return err
		}, 30*time.Second, 200*time.Millisecond).Should(Succeed())
		Eventually(func() string {
			return extraConditionReason(conditionRemoteAccessDeployed)
		}, 30*time.Second, 200*time.Millisecond).Should(Equal("Installed"))
	})

	It("remoteAccess Auto skips when no LookupService CR exists", func() {
		makeReadyClusterConfig()
		makeReadySecretsManager()

		driveSessionManagerReady(&platformv1alpha1.SessionManager{
			ObjectMeta: metav1.ObjectMeta{Name: singletonName},
		})

		Eventually(func() string {
			return extraConditionReason(conditionRemoteAccessDeployed)
		}, 30*time.Second, 200*time.Millisecond).Should(Equal("ModeAutoNoLookupService"))
	})

	It("Disabled drains a previously-installed extra", func() {
		makeReadyClusterConfig()
		makeReadySecretsManager()
		withCAOnClusterConfig()

		smgr := &platformv1alpha1.SessionManager{
			ObjectMeta: metav1.ObjectMeta{Name: singletonName},
		}
		driveSessionManagerReady(smgr)

		// Auto + CA → installed.
		Eventually(func() error {
			hc, err := helmFac.For(platformNamespace)
			if err != nil {
				return err
			}
			_, err = hc.Status(nodeCAInjectorReleaseName)
			return err
		}, 30*time.Second, 200*time.Millisecond).Should(Succeed())

		// Flip to Disabled — reconciler should drain.
		updateSessionManagerSpec(func(spec *platformv1alpha1.SessionManagerSpec) {
			spec.NodeCATrust = &platformv1alpha1.NodeCATrust{
				Mode: platformv1alpha1.ComponentModeDisabled,
			}
		})

		Eventually(func() error {
			hc, err := helmFac.For(platformNamespace)
			if err != nil {
				return err
			}
			_, statusErr := hc.Status(nodeCAInjectorReleaseName)
			return statusErr
		}, 30*time.Second, 200*time.Millisecond).Should(MatchError(helm.ErrReleaseNotFound))
		Eventually(func() string {
			return extraConditionReason(conditionNodeCATrustDeployed)
		}, 30*time.Second, 200*time.Millisecond).Should(Equal("ModeDisabled"))
	})
})
