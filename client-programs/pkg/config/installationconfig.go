package config

import (
	"os"
	"path"

	"github.com/educates/educates-training-platform/client-programs/pkg/secrets"
	"github.com/educates/educates-training-platform/client-programs/pkg/utils"
	"github.com/pkg/errors"
	"gopkg.in/yaml.v2"
)

type VolumeMountConfig struct {
	HostPath      string `yaml:"hostPath"`
	ContainerPath string `yaml:"containerPath"`
	ReadOnly      *bool  `yaml:"readOnly,omitempty"`
}

type LocalKindClusterConfig struct {
	ListenAddress   string                 `yaml:"listenAddress,omitempty"`
	ApiServer       KindApiServerConfig    `yaml:"apiServer,omitempty"`
	VolumeMounts    []VolumeMountConfig    `yaml:"volumeMounts,omitempty"`
	RegistryMirrors []RegistryMirrorConfig `yaml:"registryMirrors,omitempty"`
}

type RegistryMirrorConfig struct {
	Mirror   string `yaml:"mirror"`
	URL      string `yaml:"url,omitempty"`
	Username string `yaml:"username,omitempty"`
	Password string `yaml:"password,omitempty"`
	Port     string `yaml:"port,omitempty"`
	BindIP   string `yaml:"bindIP,omitempty"`
}

type KindApiServerConfig struct {
	Address string `yaml:"address,omitempty"`
	Port    int    `yaml:"port,omitempty"`
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
	Route53Zone Route53ZoneConfig                       `yaml:"route53,omitempty"`
	ClusterName string                                  `yaml:"clusterName,omitempty"`
	IRSARoles   AwsClusterInfrastructureIRSARolesConfig `yaml:"irsaRoles,omitempty"`
}

type Route53ZoneConfig struct {
	HostedZoneId string `yaml:"hostedZone"`
}

type GcpClusterInfrastructureWorkloadIdentitiesConfig struct {
	ExternalDns string `yaml:"external-dns"`
	CertManager string `yaml:"cert-manager"`
}

type CloudDNSConfig struct {
	Zone string `yaml:"zone,omitempty"`
}

type GcpClusterInfrastructureConfig struct {
	Project   string                                           `yaml:"project,omitempty"`
	CloudDNS  CloudDNSConfig                                   `yaml:"cloudDNS,omitempty"`
	IRSARoles GcpClusterInfrastructureWorkloadIdentitiesConfig `yaml:"workloadIdentity,omitempty"`
}

type ClusterInfrastructureConfig struct {
	// This can be only "kind", "eks", "gke" "custom" for now
	Provider       string                         `yaml:"provider"`
	AWS            AwsClusterInfrastructureConfig `yaml:"aws,omitempty"`
	GCP            GcpClusterInfrastructureConfig `yaml:"gcp,omitempty"`
	CertificateRef CACertificateRefConfig         `yaml:"caCertificateRef,omitempty"`
}

type PackageConfig struct {
	Enabled  *bool                  `yaml:"enabled,omitempty"`
	Settings map[string]interface{} `yaml:"settings"`
}

type ClusterPackagesConfig struct {
	Contour        PackageConfig `yaml:"contour,omitempty"`
	CertManager    PackageConfig `yaml:"cert-manager,omitempty"`
	ExternalDns    PackageConfig `yaml:"external-dns,omitempty"`
	Certs          PackageConfig `yaml:"certs,omitempty"`
	Kyverno        PackageConfig `yaml:"kyverno,omitempty"`
	KappController PackageConfig `yaml:"kapp-controller,omitempty"`
	Educates       PackageConfig `yaml:"educates,omitempty"`
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
	Enabled *bool `yaml:"enabled"`
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

type UserClientConfig struct {
	Id     string `yaml:"id"`
	Secret string `yaml:"secret"`
}

type TrainingPortalClientsConfig struct {
	Robot UserClientConfig `yaml:"robot,omitempty"`
}

type TrainingPortalConfig struct {
	Credentials TrainingPortalCredentialsConfig `yaml:"credentials,omitempty"`
	Clients     TrainingPortalClientsConfig     `yaml:"clients,omitempty"`
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
	Rootless   *bool            `yaml:"rootless,omitempty"`
	Privileged *bool            `yaml:"privileged,omitempty"`
	ProxyCache ProxyCacheConfig `yaml:"proxyCache,omitempty"`
}

type ClusterNetworkConfig struct {
	BlockCIDRs []string `yaml:"blockCIDRs"`
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

type ImagePullerConfig struct {
	Enabled       *bool    `yaml:"enabled"`
	PrePullImages []string `yaml:"prePullImages,omitempty"`
}

type LookupServiceConfig struct {
	Enabled       *bool  `yaml:"enabled"`
	IngressPrefix string `yaml:"ingressPrefix,omitempty"`
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
	Version           string                  `yaml:"version,omitempty"`
	ImageVersions     []ImageVersionConfig    `yaml:"imageVersions,omitempty"`
	DockerDaemon      DockerDaemonConfig      `yaml:"dockerDaemon,omitempty"`
	ClusterNetwork    ClusterNetworkConfig    `yaml:"clusterNetwork,omitempty"`
	WorkshopAnalytics WorkshopAnalyticsConfig `yaml:"workshopAnalytics,omitempty"`
	WebsiteStyling    WebsiteStylingConfig    `yaml:"websiteStyling,omitempty"`
	ImagePuller       ImagePullerConfig       `yaml:"imagePuller,omitempty"`
	LookupService     LookupServiceConfig     `yaml:"lookupService,omitempty"`
}

type InstallationConfig struct {
	Debug                 *bool                       `yaml:"debug,omitempty"`
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
	Version               string                      `yaml:"version,omitempty"`
	ImageVersions         []ImageVersionConfig        `yaml:"imageVersions,omitempty"`
	DockerDaemon          DockerDaemonConfig          `yaml:"dockerDaemon,omitempty"`
	ClusterNetwork        ClusterNetworkConfig        `yaml:"clusterNetwork,omitempty"`
	WorkshopAnalytics     WorkshopAnalyticsConfig     `yaml:"workshopAnalytics,omitempty"`
	WebsiteStyling        WebsiteStylingConfig        `yaml:"websiteStyling,omitempty"`
	ImagePuller           ImagePullerConfig           `yaml:"imagePuller,omitempty"`
	LookupService         LookupServiceConfig         `yaml:"lookupService,omitempty"`
}

type EducatesDomainStruct struct {
	ClusterIngress ClusterIngressConfig `yaml:"clusterIngress,omitempty"`
}

const NULL_CONFIG_FILE = "NULL"

func NewDefaultInstallationConfig() *InstallationConfig {
	return &InstallationConfig{
		ClusterInfrastructure: ClusterInfrastructureConfig{
			Provider: "",
		},
		ClusterPackages: ClusterPackagesConfig{
			Contour: PackageConfig{
				Enabled: utils.BoolPointer(true),
			},
			Kyverno: PackageConfig{
				Enabled: utils.BoolPointer(true),
			},
			Educates: PackageConfig{
				Enabled: utils.BoolPointer(true),
			},
		},
		ClusterSecurity: ClusterSecurityConfig{
			PolicyEngine: "kyverno",
		},
		ClusterIngress: ClusterIngressConfig{
			Domain: GetHostIpAsDns(),
		},
		WorkshopSecurity: WorkshopSecurityConfig{
			RulesEngine: "kyverno",
		},
	}
}

func NewInstallationConfigFromUserFile() (*InstallationConfig, error) {
	config := &InstallationConfig{}

	valuesFile := path.Join(utils.GetEducatesHomeDir(), "values.yaml")

	data, err := os.ReadFile(valuesFile)

	if err == nil && len(data) != 0 {
		if err := yaml.UnmarshalStrict(data, &config); err != nil {
			return nil, errors.Wrapf(err, "unable to parse default config file %s", valuesFile)
		}
	} else {
		config = NewDefaultInstallationConfig()
	}

	return config, nil
}

func NewInstallationConfigFromFile(configFile string) (*InstallationConfig, error) {
	config := &InstallationConfig{}

	data, err := os.ReadFile(configFile)

	if err != nil {
		return nil, errors.Wrapf(err, "failed to read installation config file %s", configFile)
	}

	if err := yaml.UnmarshalStrict(data, &config); err != nil {
		return nil, errors.Wrapf(err, "unable to parse installation config file %s", configFile)
	}

	return config, nil
}

func ConfigForLocalClusters(configFile string, domain string, local bool) (fullConfig *InstallationConfig, err error) {
	if configFile == NULL_CONFIG_FILE {
		fullConfig = NewDefaultInstallationConfig()
	} else if configFile != "" {
		fullConfig, err = NewInstallationConfigFromFile(configFile)
	} else {
		fullConfig, err = NewInstallationConfigFromUserFile()
	}

	if err != nil {
		return nil, err
	}

	if local {
		if fullConfig.ClusterInfrastructure.Provider != "" &&
			fullConfig.ClusterInfrastructure.Provider != "kind" &&
			fullConfig.ClusterInfrastructure.Provider != "custom" {
			return nil, errors.New("Only kind or custom providers are supported for local clusters. If not provided, will default to kind")
		}

		if fullConfig.ClusterInfrastructure.Provider == "" {
			fullConfig.ClusterInfrastructure.Provider = "kind"
		}
	}

	if domain != "" {
		fullConfig.ClusterIngress.Domain = domain
	}

	// We do resolve domain configuration precedence here
	fullConfig.ClusterIngress.Domain = EducatesDomain(fullConfig)

	if local {
		// This augments the installation config with the secrets that are cached locally
		if secretName := secrets.LocalCachedSecretForIngressDomain(fullConfig.ClusterIngress.Domain); secretName != "" {
			fullConfig.ClusterIngress.TLSCertificateRef.Namespace = "educates-secrets"
			fullConfig.ClusterIngress.TLSCertificateRef.Name = secretName
		}

		if secretName := secrets.LocalCachedSecretForCertificateAuthority(fullConfig.ClusterIngress.Domain); secretName != "" {
			fullConfig.ClusterIngress.CACertificateRef.Namespace = "educates-secrets"
			fullConfig.ClusterIngress.CACertificateRef.Name = secretName
		}
	}

	if err := ValidateProvider(fullConfig.ClusterInfrastructure.Provider); err != nil {
		return nil, err
	}

	return fullConfig, nil
}

/**
 * This function will return the configured educates Domain in the following order:
 * 1. If the domain is set in the installation config, it will return that
 * 2. If the domain is set in the Educates Package, it will return that
 * 4. If none of the above are set, it will return the host IP as a DNS
 */
func EducatesDomain(config *InstallationConfig) string {
	if config.ClusterIngress.Domain != "" {
		return config.ClusterIngress.Domain
	}
	// Access config.ClusterPackages.Educates.Settings["ClusterConfig"] and see if there's a value
	if educatesDomain, ok := config.ClusterPackages.Educates.Settings["clusterIngress"]; ok {
		// Access educatesDomain.(map[string]interface{})["domain"] and return that
		p := map[string]interface{}{}
		if educatesDomainBytes, err := yaml.Marshal(educatesDomain); err == nil {
			yaml.Unmarshal(educatesDomainBytes, &p)
			if domain, ok := p["domain"].(string); ok {
				return domain
			}
		}
	}
	return GetHostIpAsDns()
}

func PrintConfigToStdout(config *InstallationConfig) error {
	data, err := yaml.Marshal(config)

	if err != nil {
		return errors.Wrap(err, "failed to marshal installation config")
	}

	// fmt.Println("Configuration to be applied:")
	// fmt.Println("-------------------------------")
	// fmt.Println(string(data))
	os.Stdout.Write(data)
	// fmt.Println("###############################")

	return nil
}

func ValidateProvider(provider string) error {
	switch provider {
	case "eks", "kind", "gke", "custom", "vcluster", "generic", "minikube", "openshift":
		return nil
	default:
		return errors.New("Invalid ClusterInsfrastructure Provider. Valid values are (eks, gke, kind, custom, vcluster, generic, minikube, openshift)")
	}
}
