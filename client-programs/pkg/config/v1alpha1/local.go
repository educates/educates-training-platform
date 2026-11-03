package v1alpha1

import "regexp"

// EducatesLocalConfig is the laptop-kind-cluster scenario kind. Empty file
// (apiVersion + kind only) is valid; defaults fill in everything else.
//
// Hard exclusions (escalate to EducatesConfig escape hatch): mode,
// target.provider, dns, ACME, imageRegistry.prefix, cluster-service
// discriminators, analytics, dockerDaemon.*, storage.*, network.blockCIDRs,
// workshops.frameAncestors, debug.
type EducatesLocalConfig struct {
	TypeMeta `yaml:",inline"`

	Cluster           LocalClusterConfig           `yaml:"cluster,omitempty"`
	Resolver          LocalResolverConfig          `yaml:"resolver,omitempty"`
	Ingress           LocalIngressConfig           `yaml:"ingress,omitempty"`
	ClusterAdmin      *bool                        `yaml:"clusterAdmin,omitempty"`
	LookupService     *bool                        `yaml:"lookupService,omitempty"`
	ImagePrePuller    *bool                        `yaml:"imagePrePuller,omitempty"`
	WebsiteStyling    LocalWebsiteStylingConfig    `yaml:"websiteStyling,omitempty"`
	SecretPropagation LocalSecretPropagationConfig `yaml:"secretPropagation,omitempty"`
	ImageVersions     []ImageVersion               `yaml:"imageVersions,omitempty"`
	Operator          LocalOperatorConfig          `yaml:"operator,omitempty"`
}

type LocalClusterConfig struct {
	ListenAddress         string           `yaml:"listenAddress,omitempty"`
	RegistryListenAddress string           `yaml:"registryListenAddress,omitempty"`
	ApiServer             ApiServerConfig  `yaml:"apiServer,omitempty"`
	Networking            NetworkingConfig `yaml:"networking,omitempty"`
	VolumeMounts          []VolumeMount    `yaml:"volumeMounts,omitempty"`
	RegistryMirrors       []RegistryMirror `yaml:"registryMirrors,omitempty"`
	Nodes                 []ClusterNode    `yaml:"nodes,omitempty"`
}

// ClusterNode describes a node of the local kind cluster. Leaving
// cluster.nodes empty keeps the default single-control-plane cluster; when
// set, the list fully declares the cluster's nodes and must include a
// control-plane. Labels become Kubernetes node labels; taints are
// registered on the node at join time (worker nodes).
type ClusterNode struct {
	Role   string             `yaml:"role"`
	Labels map[string]string  `yaml:"labels,omitempty"`
	Taints []ClusterNodeTaint `yaml:"taints,omitempty"`
}

// ClusterNodeTaint is a Kubernetes node taint applied to a worker node when
// it registers. Value is optional; Effect is one of NoSchedule,
// PreferNoSchedule, or NoExecute.
type ClusterNodeTaint struct {
	Key    string `yaml:"key"`
	Value  string `yaml:"value,omitempty"`
	Effect string `yaml:"effect"`
}

type ApiServerConfig struct {
	Address string `yaml:"address,omitempty"`
	Port    int    `yaml:"port,omitempty"`
}

type NetworkingConfig struct {
	ServiceSubnet string `yaml:"serviceSubnet,omitempty"`
	PodSubnet     string `yaml:"podSubnet,omitempty"`
}

type VolumeMount struct {
	HostPath      string `yaml:"hostPath"`
	ContainerPath string `yaml:"containerPath"`
	ReadOnly      *bool  `yaml:"readOnly,omitempty"`
}

// RegistryMirror is the user-declared pull-through cache surface. The
// always-on localhost:5001 mirror is implicit and not represented here.
type RegistryMirror struct {
	Mirror   string `yaml:"mirror"`
	URL      string `yaml:"url,omitempty"`
	Username string `yaml:"username,omitempty"`
	Password string `yaml:"password,omitempty"`
	Port     string `yaml:"port,omitempty"`
	BindIP   string `yaml:"bindIP,omitempty"`
}

type LocalResolverConfig struct {
	TargetAddress string   `yaml:"targetAddress,omitempty"`
	ExtraDomains  []string `yaml:"extraDomains,omitempty"`
}

type LocalIngressConfig struct {
	Domain string `yaml:"domain,omitempty"`

	// Insecure serves the local cluster over plain HTTP with no TLS. No
	// CA or certificate is needed, so the one-time `educates local
	// secrets add ca` step is skipped. Translates to the operator's
	// certificates.provider: None with ingress.protocol: http.
	Insecure bool `yaml:"insecure,omitempty"`
}

// LocalWebsiteStylingConfig is the narrow subset exposed by EducatesLocalConfig.
// Full styling surface (per-page overrides, HTML snippets) is escape-hatch only.
type LocalWebsiteStylingConfig struct {
	DefaultTheme  string         `yaml:"defaultTheme,omitempty"`
	ThemeDataRefs []ThemeDataRef `yaml:"themeDataRefs,omitempty"`
}

type ThemeDataRef struct {
	Namespace string `yaml:"namespace"`
	Name      string `yaml:"name"`
}

type LocalSecretPropagationConfig struct {
	ImagePullSecretNames []string `yaml:"imagePullSecretNames,omitempty"`
}

type ImageVersion struct {
	Name  string `yaml:"name"`
	Image string `yaml:"image"`
}

type LocalOperatorConfig struct {
	Image            OperatorImage `yaml:"image,omitempty"`
	ImagePullSecrets []string      `yaml:"imagePullSecrets,omitempty"`
	LogLevel         string        `yaml:"logLevel,omitempty"`
}

type OperatorImage struct {
	Repository string `yaml:"repository,omitempty"`
	Tag        string `yaml:"tag,omitempty"`
	// PullPolicy maps to the chart's image.pullPolicy. Empty lets the
	// chart auto-derive it (Always for floating tags like develop,
	// IfNotPresent otherwise). Set to "Always" for local-registry
	// development where the tag (e.g. :dev) is rebuilt under the same
	// name on each push.
	PullPolicy string `yaml:"pullPolicy,omitempty"`
}

// Static defaults — independent of host environment. Applied after YAML
// unmarshal, before validation.
//
// Two further layers of defaulting are applied by callers (typically the
// command code, not the loader):
//
//   - ApplyCLIDefaults uses the CLI binary's compiled-in version/registry
//     to fill operator.image.{repository,tag} when empty. Deterministic
//     per CLI binary, so safe for GitOps.
//   - ApplyHostDefaults uses the laptop's host IP to fill ingress.domain
//     with a nip.io fallback. Host-specific, NOT safe for GitOps; only
//     applied when the user opted into laptop-convenience mode
//     (`--local-config`).
func (c *EducatesLocalConfig) WithDefaults() *EducatesLocalConfig {
	c.Cluster.WithDefaults()
	if c.ClusterAdmin == nil {
		t := true
		c.ClusterAdmin = &t
	}
	if c.LookupService == nil {
		t := true
		c.LookupService = &t
	}
	if c.ImagePrePuller == nil {
		t := true
		c.ImagePrePuller = &t
	}
	if c.Operator.LogLevel == "" {
		c.Operator.LogLevel = "info"
	}
	return c
}

// WithDefaults fills the static defaults of a kind cluster declaration.
// Shared by EducatesLocalConfig.cluster and EducatesConfig.target.cluster,
// so both kinds bring up the same cluster from the same fields.
func (c *LocalClusterConfig) WithDefaults() *LocalClusterConfig {
	if c.ListenAddress == "" {
		c.ListenAddress = "127.0.0.1"
	}
	return c
}

// LocalDevImageNames is the set of platform images a dev-built CLI
// defaults to its compiled-in registry — exactly the images the root
// Makefile's `build-core-images` produces. Workshop language images
// (jdk*, conda) are deliberately excluded: their chart defaults point
// at published images that exist, so optional-workshop flows keep
// working in a dev cluster; a developer who builds them locally adds
// explicit imageVersions entries, which always win.
var LocalDevImageNames = []string{
	"session-manager",
	"training-portal",
	"base-environment",
	"docker-registry",
	"image-puller",
	"secrets-manager",
	"tunnel-manager",
	"image-cache",
	"assets-server",
	"lookup-service",
	"node-ca-injector",
}

// semverRe matches release versions as stamped by the release
// workflow: X.Y.Z with optional pre-release/build suffix, optional
// leading v. Anything else ("latest", "develop", "dev", ad-hoc
// PACKAGE_VERSION values) identifies a developer-built CLI.
var semverRe = regexp.MustCompile(`^v?\d+\.\d+\.\d+(-[0-9A-Za-z.-]+)?(\+[0-9A-Za-z.-]+)?$`)

// isDevVersion reports whether the CLI binary's compiled-in version
// identifies a developer build. Release binaries are always stamped
// with a semver tag (including pre-releases like 4.0.0-alpha.1), so
// "not semver" is exactly "not built by the release pipeline".
func isDevVersion(v string) bool {
	return !semverRe.MatchString(v)
}

// ApplyCLIDefaults fills in image defaults from the CLI binary's
// compiled-in version/registry. Deterministic per CLI binary; the
// output is reproducible as long as the same CLI version is used.
//
// All binaries: operator.image.{repository,tag} default to
// `<imageRepository>/educates-operator` : projectVersion (matching
// `installer/charts/educates-installer/values.yaml`).
//
// Developer binaries only (non-semver version, e.g. `latest` from
// `make`): every LocalDevImageNames entry the user didn't override is
// defaulted to `<imageRepository>/educates-<name>:<projectVersion>`,
// so a locally built image set deploys with zero manual config.
// Release binaries (semver-stamped) skip this entirely and behave as
// before. User-supplied imageVersions entries always win.
func (c *EducatesLocalConfig) ApplyCLIDefaults(projectVersion, imageRepository string) *EducatesLocalConfig {
	if c.Operator.Image.Repository == "" && imageRepository != "" {
		c.Operator.Image.Repository = imageRepository + "/educates-operator"
	}
	if c.Operator.Image.Tag == "" && projectVersion != "" {
		c.Operator.Image.Tag = projectVersion
	}
	if projectVersion != "" && imageRepository != "" && isDevVersion(projectVersion) {
		if c.Operator.Image.PullPolicy == "" {
			// Dev tags are rebuilt under the same name on every push;
			// the chart only auto-derives Always for well-known
			// floating tags (latest/develop/...), so pin it here.
			c.Operator.Image.PullPolicy = "Always"
		}
		present := make(map[string]bool, len(c.ImageVersions))
		for _, iv := range c.ImageVersions {
			present[iv.Name] = true
		}
		for _, name := range LocalDevImageNames {
			if !present[name] {
				c.ImageVersions = append(c.ImageVersions, ImageVersion{
					Name:  name,
					Image: imageRepository + "/educates-" + name + ":" + projectVersion,
				})
			}
		}
	}
	return c
}

// Host-derived defaulting (e.g. ingress.domain ← <host-IP>.nip.io) is
// done at the caller, not on the type — the host probe is an external
// effect that doesn't belong on a value type. See pkg/config/hostinfo.
