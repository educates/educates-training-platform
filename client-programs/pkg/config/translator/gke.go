package translator

import (
	"github.com/educates/educates-training-platform/client-programs/pkg/config/v1alpha1"
)

// TranslateGKE converts EducatesGKEConfig into the deployable output.
// ECC.spec is mode: Managed with the full GKE-prod stack: BundledContour
// (LoadBalancer envoy), BundledCertManager with ACME-DNS01-CloudDNS,
// BundledExternalDNS with CloudDNS, BundledKyverno.
//
// opts is accepted for signature uniformity with TranslateLocal; the
// CASecret fields are not consumed (ACME does its own cert lifecycle).
func TranslateGKE(cfg *v1alpha1.EducatesGKEConfig, _ Options) (*Output, error) {
	out := &Output{
		OperatorChartValues:   operatorChartValuesFor(cfg.Operator),
		EducatesClusterConfig: wrapCR(apiVersionConfig, "EducatesClusterConfig", gkeECCSpec(cfg)),
		SecretsManager:        wrapCR(apiVersionPlatform, "SecretsManager", scenarioSecretsManagerSpec(cfg.Operator.LogLevel, cfg.ImageVersions)),
		SessionManager:        wrapCR(apiVersionPlatform, "SessionManager", scenarioSessionManagerSpec(cfg.Operator.LogLevel, cfg.WebsiteStyling, cfg.ImagePrePuller, cfg.ImageVersions)),
	}
	if cfg.LookupService != nil && *cfg.LookupService {
		out.LookupService = wrapCR(apiVersionPlatform, "LookupService", scenarioLookupServiceSpec(cfg.Operator.LogLevel, cfg.ImageVersions))
	}
	return out, nil
}

// gkeECCSpec builds the Managed-mode ECC.spec for GKE.
func gkeECCSpec(cfg *v1alpha1.EducatesGKEConfig) map[string]interface{} {
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
		ingress["protocol"] = "https"
	} else {
		cloudDNS := map[string]interface{}{
			"project":                        cfg.GCP.Project,
			"workloadIdentityServiceAccount": cfg.GCP.CertManagerServiceAccount,
		}
		acme := map[string]interface{}{
			"email": cfg.ACME.Email,
			"solvers": map[string]interface{}{
				"dns01": map[string]interface{}{
					"provider": "CloudDNS",
					"cloudDNS": cloudDNS,
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
				"provider": "CloudDNS",
				"sources":  []interface{}{"service"},
				"cloudDNS": map[string]interface{}{
					"project":                        cfg.GCP.Project,
					"workloadIdentityServiceAccount": cfg.GCP.ExternalDNSServiceAccount,
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

// scenarioSecretsManagerSpec is the shared body used by SecretsManager —
// logLevel plus the routed "secrets-manager" image override; the
// operator derives everything else from chart defaults + ECC.status.
func scenarioSecretsManagerSpec(logLevel string, ivs []v1alpha1.ImageVersion) map[string]interface{} {
	spec := map[string]interface{}{}
	if logLevel != "" {
		spec["logLevel"] = logLevel
	}
	if ref := componentImageRef(ivs, "secrets-manager"); ref != nil {
		spec["image"] = ref
	}
	return spec
}

// scenarioLookupServiceSpec is shared by every scenario kind that
// emits LookupService. An imageVersions entry named "lookup-service"
// routes here as spec.image.
func scenarioLookupServiceSpec(logLevel string, ivs []v1alpha1.ImageVersion) map[string]interface{} {
	spec := map[string]interface{}{
		"ingress": map[string]interface{}{"prefix": "lookup"},
	}
	if logLevel != "" {
		spec["logLevel"] = logLevel
	}
	if ref := componentImageRef(ivs, "lookup-service"); ref != nil {
		spec["image"] = ref
	}
	return spec
}

// scenarioSessionManagerSpec is the cloud-scenario-shaped SessionManager
// builder. Mirrors localSessionManagerSpec minus the laptop-specific
// storage.storageGroup / network.blockedCidrs invariants.
//
// The public-URL scheme is carried by EducatesClusterConfig.spec.ingress.
// protocol (published in status and consumed by every component), not by
// a SessionManager override, so external TLS termination needs nothing
// here.
func scenarioSessionManagerSpec(logLevel string, ws v1alpha1.LocalWebsiteStylingConfig, ipp v1alpha1.ImagePrePullerConfig, imageVersions []v1alpha1.ImageVersion) map[string]interface{} {
	spec := map[string]interface{}{}
	if logLevel != "" {
		spec["logLevel"] = logLevel
	}
	if ws.DefaultTheme != "" {
		spec["defaultTheme"] = ws.DefaultTheme
	}
	if len(ws.ThemeDataRefs) > 0 {
		spec["themes"] = themesFromDataRefs(ws.ThemeDataRefs)
	}
	applyImagePrePuller(spec, ipp)
	applySessionManagerImageOverrides(spec, imageVersions)
	return spec
}
