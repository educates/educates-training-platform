package v1alpha1

const KindEducatesEKSConfig = "EducatesEKSConfig"

// EducatesEKSConfig is the EKS production scenario kind. All cluster
// services are operator-installed and authenticated via IRSA (IAM Roles
// for Service Accounts) — no static credentials anywhere.
//
// Locked invariants applied by TranslateEKS:
//   - mode: Managed
//   - ingress.ingressClassName: contour
//   - ingress.controller.provider: BundledContour
//     bundledContour.envoyServiceType: LoadBalancer
//   - ingress.certificates.provider: BundledCertManager
//   - ingress.certificates.bundledCertManager.issuerType: ACME
//     acme.solvers.dns01.provider: Route53
//   - dns.provider: BundledExternalDNS
//     bundledExternalDNS.provider: Route53
//   - policyEnforcement: BundledKyverno (cluster + workshop)
type EducatesEKSConfig struct {
	TypeMeta `yaml:",inline"`

	// AWS carries the account + Route53 + IAM role configuration.
	// accountId, region, and route53HostedZoneId are required; both IRSA
	// role ARNs default from accountId when empty.
	AWS AWSConfig `yaml:"aws"`

	// Domain is the wildcard ingress subdomain. Required.
	Domain string `yaml:"domain"`

	// ACME carries the cert-manager ACME config. Required: email.
	// (Shared shape with EducatesGKEConfig.)
	ACME ACMEConfig `yaml:"acme"`

	// ExternalTLSTermination asserts that TLS for the ingress domain is
	// terminated outside the cluster (cloud load balancer or proxy
	// forwarding plain HTTP inward). Generated portal and workshop URLs
	// use https regardless of in-cluster certificate presence. Maps to
	// SessionManager.spec.ingressOverrides.protocol: https.
	ExternalTLSTermination bool `yaml:"externalTLSTermination,omitempty"`

	// Top-level toggles shared with EducatesLocalConfig. Defaults:
	// clusterAdmin=false, lookupService=true, imagePrePuller.enabled=true.
	ClusterAdmin      *bool                        `yaml:"clusterAdmin,omitempty"`
	LookupService     *bool                        `yaml:"lookupService,omitempty"`
	ImagePrePuller    ImagePrePullerConfig         `yaml:"imagePrePuller,omitempty"`
	WebsiteStyling    LocalWebsiteStylingConfig    `yaml:"websiteStyling,omitempty"`
	SecretPropagation LocalSecretPropagationConfig `yaml:"secretPropagation,omitempty"`
	ImageVersions     []ImageVersion               `yaml:"imageVersions,omitempty"`
	Operator          LocalOperatorConfig          `yaml:"operator,omitempty"`
}

// AWSConfig is the AWS envelope for EducatesEKSConfig.
type AWSConfig struct {
	// AccountId is the 12-digit AWS account that owns the Route53 zone
	// and the IRSA IAM roles. Required.
	AccountId string `yaml:"accountId"`

	// Region is the AWS region. Required (Route53 hosted-zone API
	// calls and ACME-DNS01 challenges go through this region).
	Region string `yaml:"region"`

	// Route53HostedZoneId names the Route53 hosted zone for the
	// wildcard domain. Required.
	Route53HostedZoneId string `yaml:"route53HostedZoneId"`

	// CertManagerRoleARN is the IAM role assumed by the cert-manager
	// K8s ServiceAccount via IRSA. Empty defaults to
	// arn:aws:iam::<accountId>:role/educates-cert-manager.
	CertManagerRoleARN string `yaml:"certManagerRoleARN,omitempty"`

	// ExternalDNSRoleARN is the IAM role assumed by the external-dns
	// K8s ServiceAccount via IRSA. Empty defaults to
	// arn:aws:iam::<accountId>:role/educates-external-dns.
	ExternalDNSRoleARN string `yaml:"externalDNSRoleARN,omitempty"`
}

func (c *EducatesEKSConfig) WithDefaults() *EducatesEKSConfig {
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
	if c.AWS.AccountId != "" {
		if c.AWS.CertManagerRoleARN == "" {
			c.AWS.CertManagerRoleARN = "arn:aws:iam::" + c.AWS.AccountId + ":role/educates-cert-manager"
		}
		if c.AWS.ExternalDNSRoleARN == "" {
			c.AWS.ExternalDNSRoleARN = "arn:aws:iam::" + c.AWS.AccountId + ":role/educates-external-dns"
		}
	}
	return c
}

func (c *EducatesEKSConfig) ApplyCLIDefaults(projectVersion, imageRepository string) *EducatesEKSConfig {
	if c.Operator.Image.Repository == "" && imageRepository != "" {
		c.Operator.Image.Repository = imageRepository + "/educates-operator"
	}
	if c.Operator.Image.Tag == "" && projectVersion != "" {
		c.Operator.Image.Tag = projectVersion
	}
	return c
}
