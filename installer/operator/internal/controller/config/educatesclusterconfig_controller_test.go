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
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	configv1alpha1 "github.com/educates/educates-training-platform/installer/operator/api/config/v1alpha1"
)

// Structural CRD validation only. Reconciler logic is not under test
// here.

// ingressWithCertificatesProvider returns a Managed-mode ingress block
// that passes structural validation, with the given certificates
// provider.
func ingressWithCertificatesProvider(provider configv1alpha1.CertificatesProvider) *configv1alpha1.Ingress {
	return &configv1alpha1.Ingress{
		Domain:           "educates.test",
		IngressClassName: "contour",
		Controller: configv1alpha1.IngressController{
			Provider: configv1alpha1.IngressControllerProviderBundledContour,
		},
		Certificates: configv1alpha1.Certificates{
			Provider: provider,
		},
	}
}

// bundledRoute53DNS returns a dns block that passes structural
// validation, with the bundled external-dns publishing to Route53.
func bundledRoute53DNS() *configv1alpha1.DNS {
	return &configv1alpha1.DNS{
		Provider: configv1alpha1.DNSProviderBundledExternalDNS,
		BundledExternalDNS: &configv1alpha1.BundledExternalDNSConfig{
			Provider: configv1alpha1.DNS01ProviderRoute53,
			Route53:  &configv1alpha1.ExternalDNSRoute53Config{HostedZoneID: "Z0000000000000"},
		},
	}
}

// bundledKyvernoPolicyEnforcement returns a policyEnforcement block
// that passes structural validation, with both engines on the bundled
// Kyverno.
func bundledKyvernoPolicyEnforcement() *configv1alpha1.PolicyEnforcement {
	return &configv1alpha1.PolicyEnforcement{
		ClusterPolicy:  configv1alpha1.ClusterPolicyConfig{Engine: configv1alpha1.ClusterPolicyEngineKyverno},
		WorkshopPolicy: configv1alpha1.WorkshopPolicyConfig{Engine: configv1alpha1.WorkshopPolicyEngineKyverno},
		Kyverno:        &configv1alpha1.KyvernoConfig{Provider: configv1alpha1.KyvernoProviderBundled},
	}
}

var _ = Describe("EducatesClusterConfig CRD validation", func() {
	AfterEach(func() {
		// Clean up the singleton if a prior test created it; ignore not-found.
		obj := &configv1alpha1.EducatesClusterConfig{}
		if err := k8sClient.Get(ctx, types.NamespacedName{Name: "cluster"}, obj); err == nil {
			Expect(k8sClient.Delete(ctx, obj)).To(Succeed())
		}
	})

	It("accepts a Managed-mode resource named 'cluster'", func() {
		obj := &configv1alpha1.EducatesClusterConfig{
			ObjectMeta: metav1.ObjectMeta{Name: "cluster"},
			Spec: configv1alpha1.EducatesClusterConfigSpec{
				Mode: configv1alpha1.ClusterConfigModeManaged,
			},
		}
		Expect(k8sClient.Create(ctx, obj)).To(Succeed())
	})

	It("rejects a resource with a name other than 'cluster' (singleton CEL)", func() {
		obj := &configv1alpha1.EducatesClusterConfig{
			ObjectMeta: metav1.ObjectMeta{Name: "not-cluster"},
			Spec: configv1alpha1.EducatesClusterConfigSpec{
				Mode: configv1alpha1.ClusterConfigModeManaged,
			},
		}
		err := k8sClient.Create(ctx, obj)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("singleton"))
	})

	It("rejects a spec.mode change on update (mode immutability CEL)", func() {
		obj := &configv1alpha1.EducatesClusterConfig{
			ObjectMeta: metav1.ObjectMeta{Name: "cluster"},
			Spec: configv1alpha1.EducatesClusterConfigSpec{
				Mode: configv1alpha1.ClusterConfigModeManaged,
			},
		}
		Expect(k8sClient.Create(ctx, obj)).To(Succeed())

		obj.Spec.Mode = configv1alpha1.ClusterConfigModeInline
		err := k8sClient.Update(ctx, obj)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("immutable"))
	})

	It("rejects a spec.ingress.certificates.provider change on update (provider immutability CEL)", func() {
		obj := &configv1alpha1.EducatesClusterConfig{
			ObjectMeta: metav1.ObjectMeta{Name: "cluster"},
			Spec: configv1alpha1.EducatesClusterConfigSpec{
				Mode:    configv1alpha1.ClusterConfigModeManaged,
				Ingress: ingressWithCertificatesProvider(configv1alpha1.CertificatesProviderNone),
			},
		}
		Expect(k8sClient.Create(ctx, obj)).To(Succeed())

		obj.Spec.Ingress.Certificates.Provider = configv1alpha1.CertificatesProviderBundledCertManager
		err := k8sClient.Update(ctx, obj)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("spec.ingress.certificates.provider is immutable"))
	})

	It("rejects removing spec.ingress once a certificates provider is set (provider immutability CEL)", func() {
		obj := &configv1alpha1.EducatesClusterConfig{
			ObjectMeta: metav1.ObjectMeta{Name: "cluster"},
			Spec: configv1alpha1.EducatesClusterConfigSpec{
				Mode:    configv1alpha1.ClusterConfigModeManaged,
				Ingress: ingressWithCertificatesProvider(configv1alpha1.CertificatesProviderBundledCertManager),
			},
		}
		Expect(k8sClient.Create(ctx, obj)).To(Succeed())

		obj.Spec.Ingress = nil
		err := k8sClient.Update(ctx, obj)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("spec.ingress.certificates.provider is immutable"))
	})

	It("accepts an update that keeps the certificates provider", func() {
		obj := &configv1alpha1.EducatesClusterConfig{
			ObjectMeta: metav1.ObjectMeta{Name: "cluster"},
			Spec: configv1alpha1.EducatesClusterConfigSpec{
				Mode:    configv1alpha1.ClusterConfigModeManaged,
				Ingress: ingressWithCertificatesProvider(configv1alpha1.CertificatesProviderNone),
			},
		}
		Expect(k8sClient.Create(ctx, obj)).To(Succeed())

		obj.Spec.Ingress.Domain = "other.educates.test"
		obj.Spec.Ingress.Protocol = configv1alpha1.IngressProtocolHTTP
		Expect(k8sClient.Update(ctx, obj)).To(Succeed())
	})

	It("rejects a spec.ingress.controller.provider change on update (provider immutability CEL)", func() {
		obj := &configv1alpha1.EducatesClusterConfig{
			ObjectMeta: metav1.ObjectMeta{Name: "cluster"},
			Spec: configv1alpha1.EducatesClusterConfigSpec{
				Mode:    configv1alpha1.ClusterConfigModeManaged,
				Ingress: ingressWithCertificatesProvider(configv1alpha1.CertificatesProviderNone),
			},
		}
		Expect(k8sClient.Create(ctx, obj)).To(Succeed())

		obj.Spec.Ingress.Controller.Provider = configv1alpha1.IngressControllerProviderExternalIngressController
		err := k8sClient.Update(ctx, obj)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("spec.ingress.controller.provider is immutable"))
	})

	It("accepts an update to the bundled Contour settings that keeps the ingress controller provider", func() {
		obj := &configv1alpha1.EducatesClusterConfig{
			ObjectMeta: metav1.ObjectMeta{Name: "cluster"},
			Spec: configv1alpha1.EducatesClusterConfigSpec{
				Mode:    configv1alpha1.ClusterConfigModeManaged,
				Ingress: ingressWithCertificatesProvider(configv1alpha1.CertificatesProviderNone),
			},
		}
		Expect(k8sClient.Create(ctx, obj)).To(Succeed())

		obj.Spec.Ingress.Controller.BundledContour = &configv1alpha1.BundledContourConfig{
			EnvoyServiceType: configv1alpha1.EnvoyServiceTypeNodePort,
		}
		Expect(k8sClient.Update(ctx, obj)).To(Succeed())
	})

	It("rejects a spec.dns.provider change on update (provider immutability CEL)", func() {
		obj := &configv1alpha1.EducatesClusterConfig{
			ObjectMeta: metav1.ObjectMeta{Name: "cluster"},
			Spec: configv1alpha1.EducatesClusterConfigSpec{
				Mode: configv1alpha1.ClusterConfigModeManaged,
				DNS:  &configv1alpha1.DNS{Provider: configv1alpha1.DNSProviderManual},
			},
		}
		Expect(k8sClient.Create(ctx, obj)).To(Succeed())

		obj.Spec.DNS = bundledRoute53DNS()
		err := k8sClient.Update(ctx, obj)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("spec.dns.provider is immutable"))
	})

	It("rejects removing a spec.dns whose provider is not None (provider immutability CEL)", func() {
		obj := &configv1alpha1.EducatesClusterConfig{
			ObjectMeta: metav1.ObjectMeta{Name: "cluster"},
			Spec: configv1alpha1.EducatesClusterConfigSpec{
				Mode: configv1alpha1.ClusterConfigModeManaged,
				DNS:  bundledRoute53DNS(),
			},
		}
		Expect(k8sClient.Create(ctx, obj)).To(Succeed())

		obj.Spec.DNS = nil
		err := k8sClient.Update(ctx, obj)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("spec.dns.provider is immutable"))
	})

	It("rejects adding a spec.dns whose provider is not None (provider immutability CEL)", func() {
		obj := &configv1alpha1.EducatesClusterConfig{
			ObjectMeta: metav1.ObjectMeta{Name: "cluster"},
			Spec: configv1alpha1.EducatesClusterConfigSpec{
				Mode: configv1alpha1.ClusterConfigModeManaged,
			},
		}
		Expect(k8sClient.Create(ctx, obj)).To(Succeed())

		obj.Spec.DNS = bundledRoute53DNS()
		err := k8sClient.Update(ctx, obj)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("spec.dns.provider is immutable"))
	})

	It("accepts an update to the bundled external-dns settings that keeps the DNS provider", func() {
		obj := &configv1alpha1.EducatesClusterConfig{
			ObjectMeta: metav1.ObjectMeta{Name: "cluster"},
			Spec: configv1alpha1.EducatesClusterConfigSpec{
				Mode: configv1alpha1.ClusterConfigModeManaged,
				DNS:  bundledRoute53DNS(),
			},
		}
		Expect(k8sClient.Create(ctx, obj)).To(Succeed())

		obj.Spec.DNS.BundledExternalDNS.Route53.Region = "eu-west-1"
		obj.Spec.DNS.BundledExternalDNS.Sources = []string{"service", "ingress"}
		Expect(k8sClient.Update(ctx, obj)).To(Succeed())
	})

	It("accepts adding a spec.dns with provider None to a resource created without one", func() {
		obj := &configv1alpha1.EducatesClusterConfig{
			ObjectMeta: metav1.ObjectMeta{Name: "cluster"},
			Spec: configv1alpha1.EducatesClusterConfigSpec{
				Mode: configv1alpha1.ClusterConfigModeManaged,
			},
		}
		Expect(k8sClient.Create(ctx, obj)).To(Succeed())

		obj.Spec.DNS = &configv1alpha1.DNS{Provider: configv1alpha1.DNSProviderNone}
		Expect(k8sClient.Update(ctx, obj)).To(Succeed())
	})

	DescribeTable("rejects a change to how policy enforcement is provided on update (policy immutability CEL)",
		func(change func(spec *configv1alpha1.EducatesClusterConfigSpec)) {
			obj := &configv1alpha1.EducatesClusterConfig{
				ObjectMeta: metav1.ObjectMeta{Name: "cluster"},
				Spec: configv1alpha1.EducatesClusterConfigSpec{
					Mode:              configv1alpha1.ClusterConfigModeManaged,
					PolicyEnforcement: bundledKyvernoPolicyEnforcement(),
				},
			}
			Expect(k8sClient.Create(ctx, obj)).To(Succeed())

			change(&obj.Spec)
			err := k8sClient.Update(ctx, obj)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("spec.policyEnforcement engines and kyverno provider are immutable"))
		},
		Entry("cluster policy engine", func(spec *configv1alpha1.EducatesClusterConfigSpec) {
			spec.PolicyEnforcement.ClusterPolicy.Engine = configv1alpha1.ClusterPolicyEnginePodSecurityStandards
		}),
		Entry("workshop policy engine", func(spec *configv1alpha1.EducatesClusterConfigSpec) {
			spec.PolicyEnforcement.WorkshopPolicy.Engine = configv1alpha1.WorkshopPolicyEngineNone
		}),
		Entry("kyverno provider", func(spec *configv1alpha1.EducatesClusterConfigSpec) {
			spec.PolicyEnforcement.Kyverno.Provider = configv1alpha1.KyvernoProviderExternal
		}),
		Entry("removing spec.policyEnforcement.kyverno", func(spec *configv1alpha1.EducatesClusterConfigSpec) {
			spec.PolicyEnforcement.Kyverno = nil
		}),
		Entry("removing spec.policyEnforcement", func(spec *configv1alpha1.EducatesClusterConfigSpec) {
			spec.PolicyEnforcement = nil
		}),
	)

	// A Managed spec without spec.policyEnforcement, or a Kyverno engine
	// without the kyverno block, fails validation and installs nothing, so
	// it is fixed by adding the missing block in place, as spec.ingress is
	// added. Once set, neither block can be removed.
	It("accepts adding spec.policyEnforcement to a resource created without one", func() {
		obj := &configv1alpha1.EducatesClusterConfig{
			ObjectMeta: metav1.ObjectMeta{Name: "cluster"},
			Spec: configv1alpha1.EducatesClusterConfigSpec{
				Mode: configv1alpha1.ClusterConfigModeManaged,
			},
		}
		Expect(k8sClient.Create(ctx, obj)).To(Succeed())

		obj.Spec.PolicyEnforcement = bundledKyvernoPolicyEnforcement()
		Expect(k8sClient.Update(ctx, obj)).To(Succeed())
	})

	It("accepts adding spec.policyEnforcement.kyverno to a policyEnforcement created without one", func() {
		pe := bundledKyvernoPolicyEnforcement()
		pe.Kyverno = nil
		obj := &configv1alpha1.EducatesClusterConfig{
			ObjectMeta: metav1.ObjectMeta{Name: "cluster"},
			Spec: configv1alpha1.EducatesClusterConfigSpec{
				Mode:              configv1alpha1.ClusterConfigModeManaged,
				PolicyEnforcement: pe,
			},
		}
		Expect(k8sClient.Create(ctx, obj)).To(Succeed())

		obj.Spec.PolicyEnforcement.Kyverno = &configv1alpha1.KyvernoConfig{Provider: configv1alpha1.KyvernoProviderBundled}
		Expect(k8sClient.Update(ctx, obj)).To(Succeed())
	})

	It("accepts an update that keeps how policy enforcement is provided", func() {
		obj := &configv1alpha1.EducatesClusterConfig{
			ObjectMeta: metav1.ObjectMeta{Name: "cluster"},
			Spec: configv1alpha1.EducatesClusterConfigSpec{
				Mode:              configv1alpha1.ClusterConfigModeManaged,
				Ingress:           ingressWithCertificatesProvider(configv1alpha1.CertificatesProviderNone),
				PolicyEnforcement: bundledKyvernoPolicyEnforcement(),
			},
		}
		Expect(k8sClient.Create(ctx, obj)).To(Succeed())

		obj.Spec.Ingress.Domain = "other.educates.test"
		obj.Spec.PolicyEnforcement = bundledKyvernoPolicyEnforcement()
		Expect(k8sClient.Update(ctx, obj)).To(Succeed())
	})

	It("accepts adding spec.ingress to a resource created without one", func() {
		obj := &configv1alpha1.EducatesClusterConfig{
			ObjectMeta: metav1.ObjectMeta{Name: "cluster"},
			Spec: configv1alpha1.EducatesClusterConfigSpec{
				Mode: configv1alpha1.ClusterConfigModeManaged,
			},
		}
		Expect(k8sClient.Create(ctx, obj)).To(Succeed())

		obj.Spec.Ingress = ingressWithCertificatesProvider(configv1alpha1.CertificatesProviderBundledCertManager)
		Expect(k8sClient.Update(ctx, obj)).To(Succeed())
	})

	It("accepts a Managed-mode resource with a None certificates provider and http protocol", func() {
		obj := &configv1alpha1.EducatesClusterConfig{
			ObjectMeta: metav1.ObjectMeta{Name: "cluster"},
			Spec: configv1alpha1.EducatesClusterConfigSpec{
				Mode: configv1alpha1.ClusterConfigModeManaged,
				Ingress: &configv1alpha1.Ingress{
					Domain:           "educates.test",
					IngressClassName: "contour",
					Protocol:         configv1alpha1.IngressProtocolHTTP,
					Controller: configv1alpha1.IngressController{
						Provider: configv1alpha1.IngressControllerProviderBundledContour,
					},
					Certificates: configv1alpha1.Certificates{
						Provider: configv1alpha1.CertificatesProviderNone,
					},
				},
			},
		}
		Expect(k8sClient.Create(ctx, obj)).To(Succeed())
	})

	It("rejects ingress.protocol http with a non-None certificates provider (ingress CEL)", func() {
		obj := &configv1alpha1.EducatesClusterConfig{
			ObjectMeta: metav1.ObjectMeta{Name: "cluster"},
			Spec: configv1alpha1.EducatesClusterConfigSpec{
				Mode: configv1alpha1.ClusterConfigModeManaged,
				Ingress: &configv1alpha1.Ingress{
					Domain:           "educates.test",
					IngressClassName: "contour",
					Protocol:         configv1alpha1.IngressProtocolHTTP,
					Controller: configv1alpha1.IngressController{
						Provider: configv1alpha1.IngressControllerProviderBundledContour,
					},
					Certificates: configv1alpha1.Certificates{
						Provider: configv1alpha1.CertificatesProviderBundledCertManager,
					},
				},
			},
		}
		err := k8sClient.Create(ctx, obj)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("ingress.protocol http is only valid"))
	})

	It("rejects Managed-mode fields when mode is Inline (exclusivity CEL)", func() {
		obj := &configv1alpha1.EducatesClusterConfig{
			ObjectMeta: metav1.ObjectMeta{Name: "cluster"},
			Spec: configv1alpha1.EducatesClusterConfigSpec{
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
				DNS: &configv1alpha1.DNS{
					Provider: configv1alpha1.DNSProviderNone,
				},
			},
		}
		err := k8sClient.Create(ctx, obj)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("forbidden when mode is Inline"))
	})

	It("rejects spec.inline when mode is Managed (exclusivity CEL)", func() {
		obj := &configv1alpha1.EducatesClusterConfig{
			ObjectMeta: metav1.ObjectMeta{Name: "cluster"},
			Spec: configv1alpha1.EducatesClusterConfigSpec{
				Mode: configv1alpha1.ClusterConfigModeManaged,
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
			},
		}
		err := k8sClient.Create(ctx, obj)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("spec.inline is forbidden"))
	})
})
