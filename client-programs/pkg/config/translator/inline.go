package translator

import (
	"fmt"
	"sort"

	"github.com/educates/educates-training-platform/client-programs/pkg/config/v1alpha1"
)

// TranslateInline converts EducatesInlineConfig into the deployable
// output. ECC.spec is mode: Inline; everything funnels under spec.inline.
// No cluster services are installed by the operator.
//
// opts.LocalCertificates is ignored: Inline mode brings its own CA reference
// via the optional caCertificateSecret field. The signature stays uniform
// with TranslateLocal so the dispatcher in Translate() doesn't have to
// special-case.
func TranslateInline(cfg *v1alpha1.EducatesInlineConfig, _ Options) (*Output, error) {
	out := &Output{
		OperatorChartValues:   inlineOperatorChartValues(cfg),
		EducatesClusterConfig: wrapCR(apiVersionConfig, "EducatesClusterConfig", inlineECCSpec(cfg)),
		SecretsManager:        wrapCR(apiVersionPlatform, "SecretsManager", inlineSecretsManagerSpec(cfg)),
		SessionManager:        wrapCR(apiVersionPlatform, "SessionManager", inlineSessionManagerSpec(cfg)),
	}
	if cfg.LookupService != nil && *cfg.LookupService {
		out.LookupService = wrapCR(apiVersionPlatform, "LookupService", inlineLookupServiceSpec(cfg))
	}
	return out, nil
}

func inlineOperatorChartValues(cfg *v1alpha1.EducatesInlineConfig) map[string]interface{} {
	return operatorChartValuesFor(cfg.Operator)
}

// inlineECCSpec builds the mode: Inline ECC.spec. The CRD's CEL rule
// forbids any of the Managed-mode top-level fields when mode is Inline,
// so the spec is strictly {mode, inline}.
func inlineECCSpec(cfg *v1alpha1.EducatesInlineConfig) map[string]interface{} {
	ingress := map[string]interface{}{
		"domain":           cfg.Domain,
		"ingressClassName": cfg.IngressClassName,
	}
	if cfg.ExternalTLSTermination {
		// TLS terminates outside the cluster: no in-cluster wildcard
		// certificate, but the public URLs are still https.
		ingress["protocol"] = string(IngressProtocolHTTPS)
	} else {
		ingress["wildcardCertificateSecretRef"] = map[string]interface{}{
			"name": cfg.WildcardCertificateSecret,
		}
	}
	if cfg.CACertificateSecret != "" {
		ingress["caCertificateSecretRef"] = map[string]interface{}{"name": cfg.CACertificateSecret}
	}
	if cfg.ClusterIssuerName != "" {
		ingress["clusterIssuerRef"] = map[string]interface{}{"name": cfg.ClusterIssuerName}
	}

	inline := map[string]interface{}{
		"ingress": ingress,
		"policyEnforcement": map[string]interface{}{
			"clusterPolicyEngine":  cfg.PolicyEnforcement.ClusterEngine,
			"workshopPolicyEngine": cfg.PolicyEnforcement.WorkshopEngine,
		},
	}
	if cfg.ImageRegistry.Prefix != "" || len(cfg.ImageRegistry.PullSecrets) > 0 {
		ir := map[string]interface{}{}
		if cfg.ImageRegistry.Prefix != "" {
			ir["prefix"] = cfg.ImageRegistry.Prefix
		}
		if len(cfg.ImageRegistry.PullSecrets) > 0 {
			refs := make([]interface{}, len(cfg.ImageRegistry.PullSecrets))
			for i, n := range cfg.ImageRegistry.PullSecrets {
				refs[i] = map[string]interface{}{"name": n}
			}
			ir["pullSecrets"] = refs
		}
		inline["imageRegistry"] = ir
	}

	return map[string]interface{}{
		"mode":   "Inline",
		"inline": inline,
	}
}

func inlineSecretsManagerSpec(cfg *v1alpha1.EducatesInlineConfig) map[string]interface{} {
	spec := map[string]interface{}{}
	if cfg.Operator.LogLevel != "" {
		spec["logLevel"] = cfg.Operator.LogLevel
	}
	if ref := componentImageRef(cfg.ImageVersions, v1alpha1.SecretsManagerImageName); ref != nil {
		spec["image"] = ref
	}
	return spec
}

func inlineLookupServiceSpec(cfg *v1alpha1.EducatesInlineConfig) map[string]interface{} {
	spec := map[string]interface{}{
		"ingress": map[string]interface{}{"prefix": "lookup"},
	}
	if cfg.Operator.LogLevel != "" {
		spec["logLevel"] = cfg.Operator.LogLevel
	}
	if ref := componentImageRef(cfg.ImageVersions, v1alpha1.LookupServiceImageName); ref != nil {
		spec["image"] = ref
	}
	return spec
}

// inlineSessionManagerSpec mirrors localSessionManagerSpec but drops the
// storage/blockedCidrs invariants — Inline mode runs on user clusters
// where storage classes and network rules are the cluster operator's
// concern, not Educates'. The cloud-metadata blockedCidrs are still
// relevant on cloud installs, but for laptop-derived defaults that's a
// local-scenario assumption we don't carry into BYO.
func inlineSessionManagerSpec(cfg *v1alpha1.EducatesInlineConfig) map[string]interface{} {
	spec := map[string]interface{}{}
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

// operatorChartValuesFor is the shared operator chart values builder.
// Inline + Local both call it; GKE/EKS will too in 11b/11c.
func operatorChartValuesFor(op v1alpha1.LocalOperatorConfig) map[string]interface{} {
	values := map[string]interface{}{}
	if op.Image.Repository != "" || op.Image.Tag != "" || op.Image.PullPolicy != "" {
		image := map[string]interface{}{}
		if op.Image.Repository != "" {
			image["repository"] = op.Image.Repository
		}
		if op.Image.Tag != "" {
			image["tag"] = op.Image.Tag
		}
		if op.Image.PullPolicy != "" {
			image["pullPolicy"] = op.Image.PullPolicy
		}
		values["image"] = image
	}
	if len(op.ImagePullSecrets) > 0 {
		secrets := make([]interface{}, len(op.ImagePullSecrets))
		for i, name := range op.ImagePullSecrets {
			secrets[i] = map[string]interface{}{"name": name}
		}
		values["imagePullSecrets"] = secrets
	}
	if op.LogLevel != "" {
		values["logLevel"] = op.LogLevel
	}
	return values
}

// OperatorConfigFromChartValues is the inverse of operatorChartValuesFor,
// through which every config kind sets the operator chart values: it
// returns the operator block of a CLI config whose chart values are
// values. It also returns, by dotted path and sorted, the values that
// block cannot express, such as ones a helm install of the chart set
// directly, which a config deployed from the block would leave unset.
func OperatorConfigFromChartValues(values map[string]interface{}) (v1alpha1.LocalOperatorConfig, []string) {
	var op v1alpha1.LocalOperatorConfig
	var unmapped []string

	for key, value := range values {
		switch key {
		case "image":
			image, ok := value.(map[string]interface{})
			if !ok {
				unmapped = append(unmapped, key)
				continue
			}
			for field, v := range image {
				s, ok := v.(string)
				switch {
				case ok && field == "repository":
					op.Image.Repository = s
				case ok && field == "tag":
					op.Image.Tag = s
				case ok && field == "pullPolicy":
					op.Image.PullPolicy = s
				default:
					unmapped = append(unmapped, key+"."+field)
				}
			}
		case "imagePullSecrets":
			secrets, ok := value.([]interface{})
			if !ok {
				unmapped = append(unmapped, key)
				continue
			}
			for i, entry := range secrets {
				secret, ok := entry.(map[string]interface{})
				name, named := secret["name"].(string)
				if !ok || !named || len(secret) != 1 {
					unmapped = append(unmapped, fmt.Sprintf("%s[%d]", key, i))
					continue
				}
				op.ImagePullSecrets = append(op.ImagePullSecrets, name)
			}
		case "logLevel":
			if s, ok := value.(string); ok {
				op.LogLevel = s
			} else {
				unmapped = append(unmapped, key)
			}
		default:
			unmapped = append(unmapped, key)
		}
	}

	sort.Strings(unmapped)
	return op, unmapped
}
