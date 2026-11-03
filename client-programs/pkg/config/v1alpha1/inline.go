package v1alpha1

const KindEducatesInlineConfig = "EducatesInlineConfig"

// EducatesInlineConfig is the BYO scenario kind. The user asserts that
// cert-manager (or a wildcard cert), an ingress controller, and a policy
// engine already exist on the cluster, and Educates uses them via
// EducatesClusterConfig.spec.inline references.
//
// Locked invariants applied by TranslateInline:
//   - EducatesClusterConfig.spec.mode: Inline
//   - All values flow under spec.inline; spec.{ingress,dns,
//     policyEnforcement,imageRegistry} stay unset (forbidden by CEL
//     on the CRD).
//
// No target.provider: Inline mode is provider-agnostic by design.
// EducatesInlineConfig is accepted by render and deploy but not by
// 'local cluster create' (which is kind-only).
type EducatesInlineConfig struct {
	TypeMeta `yaml:",inline"`

	// Domain is the wildcard ingress subdomain. Required.
	Domain string `yaml:"domain"`

	// IngressClassName names the IngressClass routing to the BYO
	// controller (e.g. "contour", "openshift-default"). Required.
	IngressClassName string `yaml:"ingressClassName"`

	// WildcardCertificateSecret names a kubernetes.io/tls Secret in the
	// operator namespace with keys tls.crt + tls.key, valid for
	// *.<Domain>. Required unless externalTLSTermination is set, in which
	// case TLS lives outside the cluster and no in-cluster certificate is
	// referenced.
	WildcardCertificateSecret string `yaml:"wildcardCertificateSecret,omitempty"`

	// CACertificateSecret optionally names a Secret with the ca.crt
	// key for the CA chain that issued the wildcard. Workshops mount it
	// when they need to trust outbound calls to private endpoints.
	CACertificateSecret string `yaml:"caCertificateSecret,omitempty"`

	// ClusterIssuerName is informational — when a cert-manager
	// ClusterIssuer signed the wildcard, this name surfaces in status.
	// Optional.
	ClusterIssuerName string `yaml:"clusterIssuerName,omitempty"`

	// ImageRegistry optionally rewrites workshop image refs to live
	// behind an in-cluster mirror and supplies pull credentials.
	ImageRegistry InlineImageRegistry `yaml:"imageRegistry,omitempty"`

	// PolicyEnforcement names the engines the cluster already enforces.
	// Defaults: clusterEngine=Kyverno, workshopEngine=Kyverno.
	PolicyEnforcement InlinePolicyEnforcement `yaml:"policyEnforcement,omitempty"`

	// ExternalTLSTermination asserts that TLS for the ingress domain is
	// terminated outside the cluster (corporate load balancer or proxy
	// forwarding plain HTTP inward). No in-cluster wildcard certificate
	// is referenced, and generated portal and workshop URLs use https.
	// Maps to EducatesClusterConfig.spec.inline.ingress.protocol: https
	// with no wildcardCertificateSecretRef.
	ExternalTLSTermination bool `yaml:"externalTLSTermination,omitempty"`

	// Top-level toggles shared with EducatesLocalConfig.
	ClusterAdmin      *bool                        `yaml:"clusterAdmin,omitempty"`
	LookupService     *bool                        `yaml:"lookupService,omitempty"`
	ImagePrePuller    ImagePrePullerConfig         `yaml:"imagePrePuller,omitempty"`
	WebsiteStyling    LocalWebsiteStylingConfig    `yaml:"websiteStyling,omitempty"`
	SecretPropagation LocalSecretPropagationConfig `yaml:"secretPropagation,omitempty"`
	ImageVersions     []ImageVersion               `yaml:"imageVersions,omitempty"`
	Operator          LocalOperatorConfig          `yaml:"operator,omitempty"`
}

type InlineImageRegistry struct {
	Prefix      string   `yaml:"prefix,omitempty"`
	PullSecrets []string `yaml:"pullSecrets,omitempty"`
}

type InlinePolicyEnforcement struct {
	// ClusterEngine enum: Kyverno | PodSecurityStandards | OpenShiftSCC | None.
	ClusterEngine string `yaml:"clusterEngine,omitempty"`
	// WorkshopEngine enum: Kyverno | None.
	WorkshopEngine string `yaml:"workshopEngine,omitempty"`
}

// WithDefaults applies static defaults that are independent of host
// environment. Operator.logLevel mirrors EducatesLocalConfig. Policy
// engines default to Kyverno (matches CRD kubebuilder defaults).
func (c *EducatesInlineConfig) WithDefaults() *EducatesInlineConfig {
	if c.ClusterAdmin == nil {
		f := false
		c.ClusterAdmin = &f
	}
	if c.LookupService == nil {
		t := true
		c.LookupService = &t
	}
	c.ImagePrePuller.WithDefaults()
	if c.Operator.LogLevel == "" {
		c.Operator.LogLevel = "info"
	}
	if c.PolicyEnforcement.ClusterEngine == "" {
		c.PolicyEnforcement.ClusterEngine = "Kyverno"
	}
	if c.PolicyEnforcement.WorkshopEngine == "" {
		c.PolicyEnforcement.WorkshopEngine = "Kyverno"
	}
	return c
}

// ApplyCLIDefaults mirrors EducatesLocalConfig's CLI-binary defaulting
// for operator.image.
func (c *EducatesInlineConfig) ApplyCLIDefaults(projectVersion, imageRepository string) *EducatesInlineConfig {
	if c.Operator.Image.Repository == "" && imageRepository != "" {
		c.Operator.Image.Repository = imageRepository + "/educates-operator"
	}
	if c.Operator.Image.Tag == "" && projectVersion != "" {
		c.Operator.Image.Tag = projectVersion
	}
	return c
}
