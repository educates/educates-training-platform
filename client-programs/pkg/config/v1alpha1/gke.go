package v1alpha1

const KindEducatesGKEConfig = "EducatesGKEConfig"

// EducatesGKEConfig is the GKE production scenario kind. All cluster
// services are operator-installed and authenticated via Workload
// Identity — no static credentials anywhere.
//
// Locked invariants applied by TranslateGKE:
//   - mode: Managed
//   - ingress.ingressClassName: contour
//   - ingress.controller.provider: BundledContour
//     bundledContour.envoyServiceType: LoadBalancer
//   - ingress.certificates.provider: BundledCertManager
//   - ingress.certificates.bundledCertManager.issuerType: ACME
//     acme.solvers.dns01.provider: CloudDNS
//   - dns.provider: BundledExternalDNS
//     bundledExternalDNS.provider: CloudDNS
//   - policyEnforcement: BundledKyverno (cluster + workshop)
//
// User-provided fields are narrow on purpose. Power users who need
// non-WI auth, alternate Contour envoyServiceType, or different policy
// engines drop to the EducatesConfig escape hatch.
type EducatesGKEConfig struct {
	TypeMeta `yaml:",inline"`

	// GCP carries the project + service-account configuration. project
	// is required; both WI service-account emails default from project
	// when empty.
	GCP GCPConfig `yaml:"gcp"`

	// Domain is the wildcard ingress subdomain. Required.
	Domain string `yaml:"domain"`

	// ACME carries the cert-manager ACME config. email is required;
	// server defaults to Let's Encrypt production at CRD level.
	ACME ACMEConfig `yaml:"acme"`

	// ExternalTLSTermination asserts that TLS for the ingress domain is
	// terminated outside the cluster (cloud load balancer or proxy
	// forwarding plain HTTP inward). Generated portal and workshop URLs
	// use https regardless of in-cluster certificate presence. Maps to
	// SessionManager.spec.ingressOverrides.protocol: https.
	ExternalTLSTermination bool `yaml:"externalTLSTermination,omitempty"`

	// Top-level toggles shared with EducatesLocalConfig. Defaults per
	// the locked design: clusterAdmin=false, lookupService=true,
	// imagePrePuller.enabled=true.
	ClusterAdmin      *bool                        `yaml:"clusterAdmin,omitempty"`
	LookupService     *bool                        `yaml:"lookupService,omitempty"`
	ImagePrePuller    ImagePrePullerConfig         `yaml:"imagePrePuller,omitempty"`
	WebsiteStyling    LocalWebsiteStylingConfig    `yaml:"websiteStyling,omitempty"`
	SecretPropagation LocalSecretPropagationConfig `yaml:"secretPropagation,omitempty"`
	ImageVersions     []ImageVersion               `yaml:"imageVersions,omitempty"`
	Operator          LocalOperatorConfig          `yaml:"operator,omitempty"`
}

// GCPConfig is the GCP envelope for EducatesGKEConfig.
type GCPConfig struct {
	// Project is the GCP project that owns the CloudDNS zone and the
	// Workload Identity service accounts. Required.
	Project string `yaml:"project"`

	// CertManagerServiceAccount is the GCP service-account email bound
	// to the cert-manager K8s ServiceAccount via Workload Identity.
	// Empty defaults to cert-manager@<project>.iam.gserviceaccount.com.
	CertManagerServiceAccount string `yaml:"certManagerServiceAccount,omitempty"`

	// ExternalDNSServiceAccount is the GCP service-account email bound
	// to the external-dns K8s ServiceAccount via Workload Identity.
	// Empty defaults to external-dns@<project>.iam.gserviceaccount.com.
	ExternalDNSServiceAccount string `yaml:"externalDNSServiceAccount,omitempty"`
}

// ACMEConfig is the user-controllable ACME surface — email + optional
// server override. The solver provider (CloudDNS for GKE, Route53 for
// EKS) is an invariant of the kind, not user-controlled here.
type ACMEConfig struct {
	// Email is the contact address registered with the ACME server.
	// Required.
	Email string `yaml:"email"`

	// Server is the ACME directory URL. Empty defers to the CRD
	// default (Let's Encrypt production).
	Server string `yaml:"server,omitempty"`
}

// WithDefaults applies static + project-derived defaults.
func (c *EducatesGKEConfig) WithDefaults() *EducatesGKEConfig {
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
	if c.GCP.Project != "" {
		if c.GCP.CertManagerServiceAccount == "" {
			c.GCP.CertManagerServiceAccount = "cert-manager@" + c.GCP.Project + ".iam.gserviceaccount.com"
		}
		if c.GCP.ExternalDNSServiceAccount == "" {
			c.GCP.ExternalDNSServiceAccount = "external-dns@" + c.GCP.Project + ".iam.gserviceaccount.com"
		}
	}
	return c
}

// ApplyCLIDefaults mirrors EducatesLocalConfig's CLI-binary defaulting.
func (c *EducatesGKEConfig) ApplyCLIDefaults(projectVersion, imageRepository string) *EducatesGKEConfig {
	if c.Operator.Image.Repository == "" && imageRepository != "" {
		c.Operator.Image.Repository = imageRepository + "/educates-operator"
	}
	if c.Operator.Image.Tag == "" && projectVersion != "" {
		c.Operator.Image.Tag = projectVersion
	}
	return c
}
