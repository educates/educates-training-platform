package config

import (
	"fmt"
	"os"
	"path"

	"github.com/pkg/errors"
	"github.com/vmware-tanzu-labs/educates-training-platform/client-programs/pkg/utils"
	"gopkg.in/yaml.v2"
)

type VolumeMountConfig struct {
	HostPath      string `yaml:"hostPath"`
	ContainerPath string `yaml:"containerPath"`
	ReadOnly      bool   `yaml:"readOnly,omitempty"`
}

type LocalKindClusterConfig struct {
	ListenAddress string              `yaml:"listenAddress,omitempty"`
	VolumeMounts  []VolumeMountConfig `yaml:"volumeMounts,omitempty"`
}

type LocalDNSResolverConfig struct {
	TargetAddress string   `yaml:"targetAddress,omitempty"`
	ExtraDomains  []string `yaml:"extraDomains,omitempty"`
}

type AwsClusterInfrastructureIRSARolesConfig struct {
	ExternalDns string `yaml:"external-dns"`
	CertManager string `yaml:"cert-manager"`
}

type AwsClusterInfrastructureConfig struct {
	AwsId       string                                  `yaml:"awsId,omitempty"`
	Region      string                                  `yaml:"region"`
	ClusterName string                                  `yaml:"clusterName,omitempty"`
	IRSARoles   AwsClusterInfrastructureIRSARolesConfig `yaml:"irsaRoles,omitempty"`
}

type ClusterInfrastructureConfig struct {
	// This can be only "kind", "eks", "custom" for now
	Provider       string                         `yaml:"provider"`
	Aws            AwsClusterInfrastructureConfig `yaml:"aws,omitempty"`
	CertificateRef CACertificateRefConfig         `yaml:"caCertificateRef,omitempty"`
}

type PackageConfig struct {
	Enabled  bool                   `yaml:"enabled"`
	Settings map[string]interface{} `yaml:"settings"`
}

type ClusterPackagesConfig struct {
	Contour        PackageConfig `yaml:"contour"`
	CertManager    PackageConfig `yaml:"cert-manager"`
	ExternalDns    PackageConfig `yaml:"external-dns"`
	Certs          PackageConfig `yaml:"certs"`
	Kyverno        PackageConfig `yaml:"kyverno"`
	MetaController PackageConfig `yaml:"metacontroller,omitempty"`
	Educates       PackageConfig `yaml:"educates"`
}

type TLSCertificateConfig struct {
	Certificate string `yaml:"tls.crt"`
	PrivateKey  string `yaml:"tls.key"`
}

type TLSCertificateRefConfig struct {
	Namespace string `yaml:"namespace"`
	Name      string `yaml:"name"`
}

type CACertificateConfig struct {
	Certificate string `yaml:"ca.crt"`
}

type CACertificateRefConfig struct {
	Namespace string `yaml:"namespace"`
	Name      string `yaml:"name"`
}

type CANodeInjectorConfig struct {
	Enabled bool `yaml:"enabled"`
}

type ClusterRuntimeConfig struct {
	Class string `yaml:"class,omitempty"`
}

type ClusterIngressConfig struct {
	Domain            string                  `yaml:"domain"`
	Class             string                  `yaml:"class,omitempty"`
	Protocol          string                  `yaml:"protocol,omitempty"`
	TLSCertificate    TLSCertificateConfig    `yaml:"tlsCertificate,omitempty"`
	TLSCertificateRef TLSCertificateRefConfig `yaml:"tlsCertificateRef,omitempty"`
	CACertificate     CACertificateConfig     `yaml:"caCertificate,omitempty"`
	CACertificateRef  CACertificateRefConfig  `yaml:"caCertificateRef,omitempty"`
	CANodeInjector    CANodeInjectorConfig    `yaml:"caNodeInjector,omitempty"`
}

type SessionCookiesConfig struct {
	Domain string `yaml:"domain,omitempty"`
}

type ClusterStorageConfig struct {
	Class string `yaml:"class,omitempty"`
	User  int    `yaml:"user,omitempty"`
	Group int    `yaml:"group,omitempty"`
}

type ClusterSecurityConfig struct {
	PolicyEngine string `yaml:"policyEngine"`
}

type PullSecretRefConfig struct {
	Namespace string `yaml:"namespace"`
	Name      string `yaml:"name"`
}

type ClusterSecretsConfig struct {
	PullSecretRefs []PullSecretRefConfig `yaml:"pullSecretRefs"`
}

type UserCredentialsConfig struct {
	Username string `yaml:"username"`
	Password string `yaml:"password"`
}

type TrainingPortalCredentialsConfig struct {
	Admin UserCredentialsConfig `yaml:"admin,omitempty"`
	Robot UserCredentialsConfig `yaml:"robot,omitempty"`
}

type TrainingPortalConfig struct {
	Credentials TrainingPortalCredentialsConfig `yaml:"credentials,omitempty"`
}

type WorkshopSecurityConfig struct {
	RulesEngine string `yaml:"rulesEngine"`
}

type ImageRegistryConfig struct {
	Host      string `yaml:"host"`
	Namespace string `yaml:"namespace"`
}

type ImageVersionConfig struct {
	Name  string `yaml:"name"`
	Image string `yaml:"image"`
}

type ProxyCacheConfig struct {
	RemoteURL string `yaml:"remoteURL"`
	Username  string `yaml:"username,omitempty"`
	Password  string `yaml:"password,omitempty"`
}
type DockerDaemonConfig struct {
	NetworkMTU int              `yaml:"networkMTU,omitempty"`
	Rootless   bool             `yaml:"rootless,omitempty"`
	Privileged bool             `yaml:"privileged,omitempty"`
	ProxyCache ProxyCacheConfig `yaml:"proxyCache,omitempty"`
}

type ClusterNetworkConfig struct {
	BlockCIDRS []string `yaml:"blockCIDRS"`
}

type GoogleAnayticsConfig struct {
	TrackingId string `yaml:"trackingId"`
}

type ClarityAnayticsConfig struct {
	TrackingId string `yaml:"trackingId"`
}

type AmplitudeAnayticsConfig struct {
	TrackingId string `yaml:"trackingId"`
}

type WebhookAnalyticsConfig struct {
	URL string `yaml:"url"`
}

type WorkshopAnalyticsConfig struct {
	Google    GoogleAnayticsConfig    `yaml:"google,omitempty"`
	Clarity   ClarityAnayticsConfig   `yaml:"clarity,omitempty"`
	Amplitude AmplitudeAnayticsConfig `yaml:"amplitude,omitempty"`
	Webhook   WebhookAnalyticsConfig  `yaml:"webhook,omitempty"`
}

type WebsiteStyleOverridesConfig struct {
	Html   string `yaml:"html"`
	Script string `yaml:"script"`
	Style  string `yaml:"style"`
}

type WebsiteHTMLSnippetConfig struct {
	HTML string `yaml:"html"`
}

type ThemeDataRefConfig struct {
	Namespace string `yaml:"namespace"`
	Name      string `yaml:"name"`
}

type WebsiteStylingConfig struct {
	WorkshopDashboard    WebsiteStyleOverridesConfig `yaml:"workshopDashboard,omitempty"`
	WorkshopInstructions WebsiteStyleOverridesConfig `yaml:"workshopInstructions,omitempty"`
	TrainingPortal       WebsiteStyleOverridesConfig `yaml:"trainingPortal,omitempty"`
	WorkshopStarted      WebsiteHTMLSnippetConfig    `yaml:"workshopStarted,omitempty"`
	WorkshopFinished     WebsiteHTMLSnippetConfig    `yaml:"workshopFinished,omitempty"`
	DefaultTheme         string                      `yaml:"defaultTheme,omitempty"`
	ThemeDataRefs        []ThemeDataRefConfig        `yaml:"themeDataRefs,omitempty"`
	FrameAncestors       []string                    `yaml:"frameAncestors,omitempty"`
}

type ClusterEssentialsConfig struct {
	ClusterInfrastructure ClusterInfrastructureConfig `yaml:"clusterInfrastructure,omitempty"`
	ClusterPackages       ClusterPackagesConfig       `yaml:"clusterPackages,omitempty"`
	ClusterSecurity       ClusterSecurityConfig       `yaml:"clusterSecurity,omitempty"`
}

type TrainingPlatformConfig struct {
	ClusterSecurity   ClusterSecurityConfig   `yaml:"clusterSecurity,omitempty"`
	ClusterRuntime    ClusterRuntimeConfig    `yaml:"clusterRuntime,omitempty"`
	ClusterIngress    ClusterIngressConfig    `yaml:"clusterIngress,omitempty"`
	SessionCookies    SessionCookiesConfig    `yaml:"sessionCookies,omitempty"`
	ClusterStorage    ClusterStorageConfig    `yaml:"clusterStorage,omitempty"`
	ClusterSecrets    ClusterSecretsConfig    `yaml:"clusterSecrets,omitempty"`
	TrainingPortal    TrainingPortalConfig    `yaml:"trainingPortal,omitempty"`
	WorkshopSecurity  WorkshopSecurityConfig  `yaml:"workshopSecurity,omitempty"`
	ImageRegistry     ImageRegistryConfig     `yaml:"imageRegistry,omitempty"`
	ImageVersions     []ImageVersionConfig    `yaml:"imageVersions,omitempty"`
	DockerDaemon      DockerDaemonConfig      `yaml:"dockerDaemon,omitempty"`
	ClusterNetwork    ClusterNetworkConfig    `yaml:"clusterNetwork,omitempty"`
	WorkshopAnalytics WorkshopAnalyticsConfig `yaml:"workshopAnalytics,omitempty"`
	WebsiteStyling    WebsiteStylingConfig    `yaml:"websiteStyling,omitempty"`
}

type InstallationConfig struct {
	Debug                 bool                        `yaml:"debug,omitempty"`
	LocalKindCluster      LocalKindClusterConfig      `yaml:"localKindCluster,omitempty"`
	LocalDNSResolver      LocalDNSResolverConfig      `yaml:"localDNSResolver,omitempty"`
	ClusterInfrastructure ClusterInfrastructureConfig `yaml:"clusterInfrastructure,omitempty"`
	ClusterPackages       ClusterPackagesConfig       `yaml:"clusterPackages,omitempty"`
	ClusterSecurity       ClusterSecurityConfig       `yaml:"clusterSecurity,omitempty"`
	ClusterRuntime        ClusterRuntimeConfig        `yaml:"clusterRuntime,omitempty"`
	ClusterIngress        ClusterIngressConfig        `yaml:"clusterIngress,omitempty"`
	SessionCookies        SessionCookiesConfig        `yaml:"sessionCookies,omitempty"`
	ClusterStorage        ClusterStorageConfig        `yaml:"clusterStorage,omitempty"`
	ClusterSecrets        ClusterSecretsConfig        `yaml:"clusterSecrets,omitempty"`
	TrainingPortal        TrainingPortalConfig        `yaml:"trainingPortal,omitempty"`
	WorkshopSecurity      WorkshopSecurityConfig      `yaml:"workshopSecurity,omitempty"`
	ImageRegistry         ImageRegistryConfig         `yaml:"imageRegistry,omitempty"`
	ImageVersions         []ImageVersionConfig        `yaml:"imageVersions,omitempty"`
	DockerDaemon          DockerDaemonConfig          `yaml:"dockerDaemon,omitempty"`
	ClusterNetwork        ClusterNetworkConfig        `yaml:"clusterNetwork,omitempty"`
	WorkshopAnalytics     WorkshopAnalyticsConfig     `yaml:"workshopAnalytics,omitempty"`
	WebsiteStyling        WebsiteStylingConfig        `yaml:"websiteStyling,omitempty"`
}

func NewDefaultInstallationConfig() *InstallationConfig {
	localIPAddress, err := HostIP()

	if err != nil {
		localIPAddress = "127.0.0.1"
	}

	return &InstallationConfig{
		ClusterInfrastructure: ClusterInfrastructureConfig{
			Provider: "kind",
		},
		ClusterPackages: ClusterPackagesConfig{
			Contour: PackageConfig{
				Enabled: true,
			},
			Kyverno: PackageConfig{
				Enabled: true,
			},
			Educates: PackageConfig{
				Enabled: true,
			},
		},
		ClusterSecurity: ClusterSecurityConfig{
			PolicyEngine: "kyverno",
		},
		ClusterIngress: ClusterIngressConfig{
			Domain: fmt.Sprintf("%s.nip.io", localIPAddress),
		},
		WorkshopSecurity: WorkshopSecurityConfig{
			RulesEngine: "kyverno",
		},
	}
}

func NewInstallationConfigFromFile(configFile string) (*InstallationConfig, error) {
	config := NewDefaultInstallationConfig()

	if configFile != "" {
		data, err := os.ReadFile(configFile)

		if err != nil {
			return nil, errors.Wrapf(err, "failed to read installation config file %s", configFile)
		}

		if err := yaml.UnmarshalStrict(data, &config); err != nil {
			return nil, errors.Wrapf(err, "unable to parse installation config file %s", configFile)
		}
	} else {
		valuesFile := path.Join(utils.GetEducatesHomeDir(), "values.yaml")

		data, err := os.ReadFile(valuesFile)

		if err == nil && len(data) != 0 {
			if err := yaml.UnmarshalStrict(data, &config); err != nil {
				return nil, errors.Wrapf(err, "unable to parse default config file %s", valuesFile)
			}
		}
	}

	return config, nil
}
