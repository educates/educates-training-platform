package v1alpha1

const KindEducatesConfig = "EducatesConfig"

// EducatesConfig is the escape-hatch CLI config kind. Its body mirrors the
// four platform CRDs verbatim (layout B1: section keys = camelCase CRD kind,
// body = CR .spec). The CLI wraps apiVersion/kind/metadata.name at translate
// time and applies the result without further transformation.
//
// Per the locked design:
//   - No CLI-inferred defaults (no nip.io domain, no auto-injected TLS).
//   - No invariants. Every CRD field is settable.
//   - Static CRD defaults still apply at apply-time via apiserver defaulting.
//
// The CR-spec sections are passed through as untyped maps; the schema (not
// Go types) is the source of truth for their shape.
type EducatesConfig struct {
	TypeMeta `yaml:",inline"`

	// Target carries CLI-side-effect inputs (kind cluster bootstrap +
	// macOS resolver). Optional; when absent the CLI just applies the
	// declared CRs with no side effects. provider drives which side
	// effects run.
	Target *EducatesConfigTarget `yaml:"target,omitempty"`

	// Operator chart values surface — same fields as on every scenario kind.
	Operator LocalOperatorConfig `yaml:"operator,omitempty"`

	// CR-spec passthrough sections. Untyped on purpose: the JSON schema
	// (generated from the CRDs) is the source of truth for field shape.
	// Omitted LookupService means it is not deployed.
	EducatesClusterConfig map[string]interface{} `yaml:"educatesClusterConfig,omitempty"`
	SecretsManager        map[string]interface{} `yaml:"secretsManager,omitempty"`
	LookupService         map[string]interface{} `yaml:"lookupService,omitempty"`
	SessionManager        map[string]interface{} `yaml:"sessionManager,omitempty"`
}

// EducatesConfigTarget carries CLI-side-effect inputs. cluster/resolver
// reuse the same Go types as EducatesLocalConfig so the kind cluster + macOS
// resolver code paths can accept either kind interchangeably.
type EducatesConfigTarget struct {
	Provider string              `yaml:"provider,omitempty"`
	Cluster  LocalClusterConfig  `yaml:"cluster,omitempty"`
	Resolver LocalResolverConfig `yaml:"resolver,omitempty"`
}

// IngressDomain returns the ingress domain the educatesClusterConfig
// section declares: spec.inline.ingress.domain in Inline mode, and
// spec.ingress.domain otherwise. Empty when the section declares none.
func (c *EducatesConfig) IngressDomain() string {
	path := []string{"ingress", "domain"}
	if mode, _ := c.EducatesClusterConfig["mode"].(string); mode == "Inline" {
		path = []string{"inline", "ingress", "domain"}
	}
	var v interface{} = c.EducatesClusterConfig
	for _, key := range path {
		switch m := v.(type) {
		case map[string]interface{}:
			v = m[key]
		case map[interface{}]interface{}:
			v = m[key]
		default:
			return ""
		}
	}
	domain, _ := v.(string)
	return domain
}
