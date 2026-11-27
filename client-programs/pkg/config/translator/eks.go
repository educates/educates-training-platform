package translator

import (
	"github.com/educates/educates-training-platform/client-programs/pkg/config/v1alpha1"
)

// TranslateEKS converts EducatesEKSConfig into the deployable output.
// ECC.spec is mode: Managed with the EKS-prod stack: BundledContour
// (LoadBalancer envoy), BundledCertManager with ACME-DNS01-Route53,
// BundledExternalDNS with Route53, BundledKyverno.
func TranslateEKS(cfg *v1alpha1.EducatesEKSConfig, _ Options) (*Output, error) {
	out := &Output{
		OperatorChartValues:   operatorChartValuesFor(cfg.Operator),
		EducatesClusterConfig: wrapCR(apiVersionConfig, "EducatesClusterConfig", eksECCSpec(cfg)),
		SecretsManager:        wrapCR(apiVersionPlatform, "SecretsManager", scenarioSecretsManagerSpec(cfg.Operator.LogLevel, cfg.ImageVersions)),
		SessionManager:        wrapCR(apiVersionPlatform, "SessionManager", scenarioSessionManagerSpec(cfg.Operator.LogLevel, cfg.ClusterAdmin, cfg.WebsiteStyling, cfg.ImagePrePuller, cfg.ImageVersions)),
	}
	if cfg.LookupService != nil && *cfg.LookupService {
		out.LookupService = wrapCR(apiVersionPlatform, "LookupService", scenarioLookupServiceSpec(cfg.Operator.LogLevel, cfg.ImageVersions))
	}
	return out, nil
}

// eksECCSpec builds the Managed-mode ECC.spec for EKS. Mirrors the GKE
// shape but with Route53 in place of CloudDNS, and IRSA roles in place
// of WI service-account emails.
func eksECCSpec(cfg *v1alpha1.EducatesEKSConfig) map[string]interface{} {
	ingress := map[string]interface{}{
		"domain":           cfg.Domain,
		"ingressClassName": "contour",
		"controller": map[string]interface{}{
			"provider": "BundledContour",
			"bundledContour": map[string]interface{}{
				"envoyServiceType": "LoadBalancer",
			},
		},
	}
	if cfg.ExternalTLSTermination {
		// TLS terminates at the cloud load balancer: Educates issues no
		// certificate, but the public URLs are still https.
		ingress["certificates"] = map[string]interface{}{"provider": string(CertificatesProviderNone)}
		ingress["protocol"] = string(IngressProtocolHTTPS)
	} else {
		route53 := map[string]interface{}{
			"hostedZoneID": cfg.AWS.Route53HostedZoneId,
			"region":       cfg.AWS.Region,
			"iamRoleARN":   cfg.AWS.CertManagerRoleARN,
		}
		acme := map[string]interface{}{
			"email": cfg.ACME.Email,
			"solvers": map[string]interface{}{
				"dns01": map[string]interface{}{
					"provider": "Route53",
					"route53":  route53,
				},
			},
		}
		if cfg.ACME.Server != "" {
			acme["server"] = cfg.ACME.Server
		}
		ingress["certificates"] = map[string]interface{}{
			"provider": string(CertificatesProviderBundledCertManager),
			"bundledCertManager": map[string]interface{}{
				"issuerType": "ACME",
				"acme":       acme,
			},
		}
	}

	return map[string]interface{}{
		"mode":    "Managed",
		"ingress": ingress,
		"dns": map[string]interface{}{
			"provider": "BundledExternalDNS",
			"bundledExternalDNS": map[string]interface{}{
				"provider": "Route53",
				"sources":  []interface{}{"service"},
				"route53": map[string]interface{}{
					"hostedZoneID": cfg.AWS.Route53HostedZoneId,
					"region":       cfg.AWS.Region,
					"iamRoleARN":   cfg.AWS.ExternalDNSRoleARN,
				},
			},
		},
		"policyEnforcement": map[string]interface{}{
			"clusterPolicy":  map[string]interface{}{"engine": "Kyverno"},
			"workshopPolicy": map[string]interface{}{"engine": "Kyverno"},
			"kyverno":        map[string]interface{}{"provider": "Bundled"},
		},
	}
}
