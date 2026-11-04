package translator

import (
	"fmt"

	"github.com/educates/educates-training-platform/client-programs/pkg/config/v1alpha1"
)

// TranslateLocal converts EducatesLocalConfig into the deployable output.
//
// Translator invariants applied here:
//   - mode: Managed
//   - ingress.ingressClassName: contour
//   - ingress.controller.provider: BundledContour
//   - policyEnforcement: BundledKyverno (cluster + workshop)
//
// ingress.certificates comes from opts.LocalCertificates, the decision
// the CLI's local certificate resolution step made from the local
// secrets cache: provider None; BundledCertManager with the CustomCA
// issuer, its CA key pair reference and, optionally, the CA trust Secret
// reference; or StaticCertificate with the wildcard certificate
// reference and, optionally, the CA trust Secret reference. Returns an
// error when that decision is missing or incomplete, so an install that
// cannot get its certificates fails at translate time rather than at
// deploy time.
//
// When ingress.insecure is set the cluster serves plain HTTP
// (ingress.protocol http).
//
// Static field defaults (clusterAdmin true, lookupService true,
// imagePrePuller.enabled true, operator.logLevel info,
// cluster.listenAddress 127.0.0.1) have already been applied by
// EducatesLocalConfig.WithDefaults() at load time.
//
// Environment-dependent defaults are NOT applied here:
//   - ingress.domain stays empty unless the caller set it (host-IP nip.io
//     defaulting belongs upstream of the translator).
//   - operator.image.tag stays as-is (CLI-binary-version defaulting
//     belongs in command code that has access to the build info).
func TranslateLocal(cfg *v1alpha1.EducatesLocalConfig, opts Options) (*Output, error) {
	certificates, err := localCertificates(opts.LocalCertificates)
	if err != nil {
		return nil, err
	}
	out := &Output{
		OperatorChartValues:   operatorChartValuesFor(cfg.Operator),
		EducatesClusterConfig: wrapCR(apiVersionConfig, "EducatesClusterConfig", localECCSpec(cfg, certificates)),
		SecretsManager:        wrapCR(apiVersionPlatform, "SecretsManager", localSecretsManagerSpec(cfg)),
		SessionManager:        wrapCR(apiVersionPlatform, "SessionManager", localSessionManagerSpec(cfg)),
	}
	if cfg.LookupService != nil && *cfg.LookupService {
		out.LookupService = wrapCR(apiVersionPlatform, "LookupService", localLookupServiceSpec(cfg))
	}
	return out, nil
}

// localCertificates builds the ingress.certificates block from the
// local certificate decision, rejecting a missing or incomplete one.
func localCertificates(lc LocalCertificates) (map[string]interface{}, error) {
	switch lc.Provider {
	case CertificatesProviderNone:
		// No cert-manager, no issuer, no certificate.
		return map[string]interface{}{"provider": string(CertificatesProviderNone)}, nil
	case CertificatesProviderBundledCertManager:
		if lc.CustomCA == nil || lc.CustomCA.CAKeyPairSecretRef.Name == "" {
			return nil, fmt.Errorf("translator: the BundledCertManager decision for EducatesLocalConfig has no CA key pair Secret name")
		}
		customCA := map[string]interface{}{
			"caKeyPairSecretRef": secretRef(lc.CustomCA.CAKeyPairSecretRef),
		}
		if ref := lc.CustomCA.CACertificateSecretRef; ref != nil {
			customCA["caCertificateSecretRef"] = secretRef(*ref)
		}
		return map[string]interface{}{
			"provider": string(CertificatesProviderBundledCertManager),
			"bundledCertManager": map[string]interface{}{
				"issuerType": "CustomCA",
				"customCA":   customCA,
			},
		}, nil
	case CertificatesProviderStaticCertificate:
		if lc.StaticCertificate == nil || lc.StaticCertificate.TLSSecretRef.Name == "" {
			return nil, fmt.Errorf("translator: the StaticCertificate decision for EducatesLocalConfig has no TLS certificate Secret name")
		}
		static := map[string]interface{}{
			"tlsSecretRef": secretRef(lc.StaticCertificate.TLSSecretRef),
		}
		if ref := lc.StaticCertificate.CACertificateSecretRef; ref != nil {
			static["caCertificateSecretRef"] = secretRef(*ref)
		}
		return map[string]interface{}{
			"provider":          string(CertificatesProviderStaticCertificate),
			"staticCertificate": static,
		}, nil
	case "":
		return nil, fmt.Errorf("translator: EducatesLocalConfig needs a certificates decision; the caller must resolve it from the local secrets cache before translating")
	default:
		return nil, fmt.Errorf("translator: unsupported certificates provider %q for EducatesLocalConfig", lc.Provider)
	}
}

// secretRef builds a SecretReference, leaving out an empty namespace.
func secretRef(ref SecretRef) map[string]interface{} {
	out := map[string]interface{}{"name": ref.Name}
	if ref.Namespace != "" {
		out["namespace"] = ref.Namespace
	}
	return out
}

// localECCSpec builds the EducatesClusterConfig.spec for Local mode.
// Always Managed and BundledContour; certificates is the block
// localCertificates built from the caller's decision.
func localECCSpec(cfg *v1alpha1.EducatesLocalConfig, certificates map[string]interface{}) map[string]interface{} {
	ingress := map[string]interface{}{
		"ingressClassName": "contour",
		"controller": map[string]interface{}{
			"provider": "BundledContour",
			// kind has no in-cluster LoadBalancer. Envoy is exposed as a
			// ClusterIP Service, and the operator pairs ClusterIP with
			// host ports (80/443) so the kind node's mapped 80/443 reach
			// Envoy directly — the v3 kind topology.
			"bundledContour": map[string]interface{}{
				"envoyServiceType": "ClusterIP",
			},
		},
		"certificates": certificates,
	}
	if cfg.Ingress.Insecure {
		ingress["protocol"] = "http"
	}
	if cfg.Ingress.Domain != "" {
		ingress["domain"] = cfg.Ingress.Domain
	}

	spec := map[string]interface{}{
		"mode":    "Managed",
		"ingress": ingress,
		// BundledKyverno invariant. clusterPolicy.engine and
		// workshopPolicy.engine both default to Kyverno via kubebuilder
		// markers, and kyverno.provider defaults to Bundled, but the
		// PolicyEnforcement.{Cluster,Workshop}Policy fields are +required
		// so the block must be present explicitly.
		"policyEnforcement": map[string]interface{}{
			"clusterPolicy":  map[string]interface{}{"engine": "Kyverno"},
			"workshopPolicy": map[string]interface{}{"engine": "Kyverno"},
			"kyverno":        map[string]interface{}{"provider": "Bundled"},
		},
	}
	return spec
}

// localSecretsManagerSpec — near-empty spec; the operator derives
// image/resources from chart defaults + ECC status. An imageVersions
// entry named "secrets-manager" routes here as spec.image (the
// component has its own CR; it is not part of SessionManager's image
// inventory).
func localSecretsManagerSpec(cfg *v1alpha1.EducatesLocalConfig) map[string]interface{} {
	spec := map[string]interface{}{}
	if cfg.Operator.LogLevel != "" {
		spec["logLevel"] = cfg.Operator.LogLevel
	}
	if ref := componentImageRef(cfg.ImageVersions, v1alpha1.SecretsManagerImageName); ref != nil {
		spec["image"] = ref
	}
	return spec
}

// localLookupServiceSpec — minimal; ingress.prefix=lookup is the conventional
// hostname segment. An imageVersions entry named "lookup-service" routes
// here as spec.image.
func localLookupServiceSpec(cfg *v1alpha1.EducatesLocalConfig) map[string]interface{} {
	spec := map[string]interface{}{
		"ingress": map[string]interface{}{
			"prefix": "lookup",
		},
	}
	if cfg.Operator.LogLevel != "" {
		spec["logLevel"] = cfg.Operator.LogLevel
	}
	if ref := componentImageRef(cfg.ImageVersions, v1alpha1.LookupServiceImageName); ref != nil {
		spec["image"] = ref
	}
	return spec
}

// localSessionManagerSpec carries the session-manager runtime knobs the
// CLI surfaces in the narrow EducatesLocalConfig shape.
//
// Locked invariants applied here:
//   - storage.storageGroup: 1
//   - network.blockedCidrs: cloud metadata endpoints
//     (169.254.169.254/32 covers AWS/GCP/Azure IMDS;
//     fd00:ec2::254/128 covers AWS IMDS over IPv6).
//
// TODO: clusterAdmin and secretPropagation have no landing field in the
// current SessionManager CRD. They are dropped here pending the CRD
// additions. The operator will need spec.clusterAdmin (bool) and
// spec.secretPropagation (imagePullSecretNames list) before this
// translator can wire them up.
func localSessionManagerSpec(cfg *v1alpha1.EducatesLocalConfig) map[string]interface{} {
	spec := map[string]interface{}{
		"storage": map[string]interface{}{
			"storageGroup": 1,
		},
		"network": map[string]interface{}{
			"blockedCidrs": []interface{}{
				"169.254.169.254/32",
				"fd00:ec2::254/128",
			},
		},
	}
	if cfg.Operator.LogLevel != "" {
		spec["logLevel"] = cfg.Operator.LogLevel
	}
	if cfg.WebsiteStyling.DefaultTheme != "" {
		spec["defaultTheme"] = cfg.WebsiteStyling.DefaultTheme
	}
	if len(cfg.WebsiteStyling.ThemeDataRefs) > 0 {
		spec["themes"] = themesFromDataRefs(cfg.WebsiteStyling.ThemeDataRefs)
	}
	applyImagePrePuller(spec, cfg.ImagePrePuller)
	applySessionManagerImageOverrides(spec, cfg.ImageVersions)
	return spec
}
