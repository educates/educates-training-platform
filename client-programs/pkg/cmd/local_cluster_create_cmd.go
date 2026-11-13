package cmd

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/spf13/cobra"

	"github.com/educates/educates-training-platform/client-programs/pkg/cluster"
	"github.com/educates/educates-training-platform/client-programs/pkg/config"
	"github.com/educates/educates-training-platform/client-programs/pkg/config/v1alpha1"
	"github.com/educates/educates-training-platform/client-programs/pkg/constants"
	"github.com/educates/educates-training-platform/client-programs/pkg/deployer"
	"github.com/educates/educates-training-platform/client-programs/pkg/deployer/progress"
	"github.com/educates/educates-training-platform/client-programs/pkg/localcerts"
	"github.com/educates/educates-training-platform/client-programs/pkg/registry"
	"github.com/educates/educates-training-platform/client-programs/pkg/utils"
)

var localClusterCreateExample = `
  # Create a local kind cluster and install Educates using the laptop config:
  educates local cluster create

  # Create using a specific config file:
  educates local cluster create --config config.yaml

  # Create only the kind cluster + registry, skipping the platform install:
  educates local cluster create --cluster-only
`

type LocalClusterCreateOptions struct {
	Config            string
	LocalConfig       bool
	Kubeconfig        string
	Context           string
	ClusterImage      string
	KubernetesVersion string
	ClusterOnly       bool
	RegistryBindIP    string
	Timeout           time.Duration
	Verbose           bool
}

func (p *ProjectInfo) NewLocalClusterCreateCmd() *cobra.Command {
	var o LocalClusterCreateOptions

	c := &cobra.Command{
		Args:  cobra.NoArgs,
		Use:   "create",
		Short: "Create a local kind cluster, bring up the registry, and install Educates",
		Long: `Lays down a full laptop install in one command:

  1. Loads EducatesLocalConfig (or EducatesConfig with target.provider=kind).
  2. Creates the 'educates' kind cluster.
  3. Brings up the always-on localhost:5001 registry.
  4. Sets up the loopback service for 'educates serve-workshop'.
  5. Tail-calls into the platform deploy pipeline
     (helm install operator + apply 4 platform CRs + wait Ready).

An EducatesConfig creates the kind cluster from target.cluster and deploys
its custom resources as declared, with no CLI defaulting: no
<host-IP>.nip.io domain, and no certificates or Secrets taken from the
local secrets cache. Any Secret the custom resources reference has to be
created in the cluster before the deploy can become ready.

--cluster-only stops after step 4 — useful for testing the platform
deploy against a hand-prepared cluster.`,
		Example: localClusterCreateExample,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ip, err := utils.ValidateAndResolveIP(o.RegistryBindIP)
			if err != nil {
				return fmt.Errorf("invalid --registry-bind-ip: %w", err)
			}
			o.RegistryBindIP = ip

			// --kubernetes-version selects the kind node image. An explicit
			// --kind-cluster-image always wins, so power users can still pin
			// an arbitrary image.
			if o.KubernetesVersion != "" {
				image, ok := constants.KubernetesVersionToKindImage[o.KubernetesVersion]
				if !ok {
					return fmt.Errorf("unsupported --kubernetes-version %q; supported versions are: %v", o.KubernetesVersion, constants.SupportedKubernetesVersions())
				}
				if o.ClusterImage == "" {
					o.ClusterImage = image
				}
			}

			return p.runLocalClusterCreate(cmd.Context(), cmd.OutOrStdout(), &o)
		},
	}

	c.Flags().StringVarP(&o.Config, "config", "c", "", "path to a CLI config file (any kind)")
	c.Flags().BoolVar(&o.LocalConfig, "local-config", false, "use <data-home>/config.yaml (default when --config is not given)")
	c.Flags().StringVar(&o.Kubeconfig, "kubeconfig", "", "kubeconfig file (defaults to $KUBECONFIG / ~/.kube/config)")
	c.Flags().StringVar(&o.Context, "context", "", "context name to use within the kubeconfig (for the platform deploy tail-call)")
	c.Flags().StringVar(&o.ClusterImage, "kind-cluster-image", "", "docker image to use when booting the kind cluster (overrides --kubernetes-version)")
	c.Flags().StringVar(&o.KubernetesVersion, "kubernetes-version", constants.DefaultKubernetesVersion, fmt.Sprintf("Kubernetes version for the kind cluster (supported: %v)", constants.SupportedKubernetesVersions()))
	c.Flags().BoolVar(&o.ClusterOnly, "cluster-only", false, "create kind cluster + registry; skip the platform deploy")
	c.Flags().StringVar(&o.RegistryBindIP, "registry-bind-ip", "127.0.0.1", "bind IP for the always-on localhost:5001 registry")
	c.Flags().DurationVar(&o.Timeout, "timeout", deployer.DefaultTimeout, "per-CR Ready=True wait timeout (passed through to deploy)")
	c.Flags().BoolVar(&o.Verbose, "verbose", false, "show helm SDK debug output on stderr")
	c.MarkFlagsMutuallyExclusive("config", "local-config")

	return c
}

func (p *ProjectInfo) runLocalClusterCreate(ctx context.Context, w io.Writer, o *LocalClusterCreateOptions) error {
	cfg, configPath, createdConfig, err := loadLocalConfig(o)
	if err != nil {
		return err
	}
	if createdConfig {
		fmt.Fprintf(w, "No local config found — wrote a default EducatesLocalConfig to %s\n", configPath)
		fmt.Fprintf(w, "Continuing with defaults; view or edit it with 'educates local config view' / 'educates local config edit'.\n\n")
	}

	// 0. Preflight: fail fast (before any docker / kind / k8s mutation)
	//    when the cluster already exists, host 80/443 are bound, or a
	//    secure install has no certificate source. The host-port case is
	//    the v3 busybox probe; kind itself fails later with a much less
	//    actionable error if Envoy can't publish. The certificate case is
	//    the one the deploy resolves later: without this check it would
	//    only fail after the kind cluster and registry already exist.
	in, err := p.prepareLocalCreate(cfg)
	if err != nil {
		return err
	}
	id := defaultLocalCluster()
	clusterConfig := cluster.NewKindClusterConfig(o.Kubeconfig, id)
	if exists, err := clusterConfig.ClusterExists(); err != nil {
		return err
	} else if exists {
		return fmt.Errorf("kind cluster '%s' already exists; run 'educates local cluster delete' first or use the existing cluster directly", id.KindName)
	}
	if err := checkHostPortsAvailable(ctx, in.Cluster.ListenAddress, o.Verbose, w); err != nil {
		return err
	}

	// The kind, registry, loopback and mirror phases all report through
	// one progress reporter so the create flow reads as a single sequence
	// of `→ … ✓` steps (the platform deploy tail-call builds its own
	// reporter against the same writer). Verbose turns off in-place
	// morphing so kind's full spinner and the per-step detail lines are
	// all preserved rather than overwritten.
	rep := progress.New(w, 0, isStdoutTTY(w) && !o.Verbose)

	// 1. kind bootstrap. kindBootstrapFromConfig builds the focused
	//    KindBootstrapInput from EducatesLocalConfig.Cluster fields the
	//    template reads. By default kind's phases are forwarded onto this
	//    one morphing step line and its footer chatter is suppressed.
	//    Under --verbose the reporter is non-morphing and kind uses its
	//    own full logger (spinner + detail), so the step's committed
	//    header/footer simply frame kind's output.
	bootstrap := kindBootstrapFromConfig(in.Cluster)
	if err := runStep(rep, fmt.Sprintf("creating kind cluster '%s'", id.KindName), "ready", func(s progress.Step) error {
		onPhase := s.Update
		if o.Verbose {
			onPhase = nil
		}
		return clusterConfig.CreateCluster(bootstrap, o.ClusterImage, onPhase, o.Verbose)
	}); err != nil {
		return err
	}
	client, err := clusterConfig.Config.GetClient()
	if err != nil {
		return err
	}

	// 2. always-on local registry + k8s Service for imgpkg pulls.
	if err := runStep(rep, "bringing up localhost:5001 registry", "ready", func(s progress.Step) error {
		if err := registry.DeployRegistryAndLinkToCluster(o.RegistryBindIP, []string{id.ControlPlaneNode()}, client, s); err != nil {
			return err
		}
		s.Update("registering cluster service")
		return registry.UpdateRegistryK8SService(client)
	}); err != nil {
		return fmt.Errorf("registry: %w", err)
	}

	// 3. loopback service for hugo livereload (educates serve-workshop).
	//    It resolves localhost.<ingress domain>, so a config that declares
	//    no domain gets none.
	if in.Domain == "" {
		rep.Note("no ingress domain declared; skipped the loopback service for 'educates serve-workshop'")
	} else if err := runStep(rep, "creating loopback service", "ready", func(s progress.Step) error {
		return cluster.CreateLoopbackService(client, in.Domain)
	}); err != nil {
		return fmt.Errorf("loopback service: %w", err)
	}

	// 4. registry mirrors declared in config (pull-through caches).
	for _, m := range in.Cluster.RegistryMirrors {
		label := "registry mirror " + m.Mirror
		if m.URL != "" {
			label += " → " + m.URL
		}
		mc := registryMirrorFromConfig(m)
		if err := runStep(rep, label, "ready", func(s progress.Step) error {
			return registry.DeployMirrorAndLinkToCluster(&mc, []string{id.ControlPlaneNode()}, s)
		}); err != nil {
			return fmt.Errorf("mirror %s: %w", m.Mirror, err)
		}
	}

	if o.ClusterOnly {
		rep.Note("cluster + registry ready (--cluster-only; skipped platform deploy)")
		return nil
	}

	// 5. tail-call the v4 deploy. We have the loaded config; rather
	//    than re-loading it inside runDeploy (which would re-do
	//    the host-IP fallback non-deterministically against a freshly
	//    started cluster's IP), translate here and call deployer.Deploy
	//    directly.
	return tailCallDeploy(ctx, w, in.Deploy, in.Plan, o)
}

// loadLocalConfig returns the loaded v4 config + the path it came from
// (used by error messages) + whether it just wrote a default config in
// place of a missing one (so the caller can tell the user). Accepts
// EducatesLocalConfig directly or EducatesConfig with target.provider=kind;
// everything else errors.
func loadLocalConfig(o *LocalClusterCreateOptions) (v1alpha1.Config, string, bool, error) {
	var path string
	var created bool
	// --local-config is the default for laptop create — matches v3
	// behaviour where running the command with no flags pointed at
	// <data-home>/config.yaml. --config still wins when set.
	if o.Config != "" {
		path = o.Config
	} else {
		path = config.LocalConfigPath(utils.GetEducatesHomeDir())
		// Unlike render/deploy, a first-time laptop create shouldn't dead-end
		// on "run config init first"; write a minimal default in place and
		// carry on. Migration of an existing v3 install still takes priority.
		didInit, err := config.EnsureOrInitLocalConfigFile(utils.GetEducatesHomeDir())
		if err != nil {
			return nil, "", false, err
		}
		created = didInit
	}
	cfg, err := config.Load(path)
	if err != nil {
		return nil, path, created, err
	}
	switch c := cfg.(type) {
	case *v1alpha1.EducatesLocalConfig:
		return c, path, created, nil
	case *v1alpha1.EducatesConfig:
		if c.Target == nil || c.Target.Provider != "kind" {
			return nil, path, created, fmt.Errorf("%s: EducatesConfig is accepted only with target.provider=kind for laptop create", path)
		}
		return c, path, created, nil
	default:
		return nil, path, created, fmt.Errorf("%s: unsupported kind %q for local cluster create", path, cfg.GetKind())
	}
}

// localCreateInputs is what local cluster create takes from the loaded
// config, whichever of the two accepted kinds it is.
type localCreateInputs struct {
	// Deploy is the config the platform deploy tail-call translates.
	Deploy v1alpha1.Config

	// Cluster declares the kind cluster and its registry mirrors.
	Cluster v1alpha1.LocalClusterConfig

	// Domain is the ingress domain the loopback service resolves under.
	// Empty when the config declares none.
	Domain string

	// Plan is the local certificate plan of an EducatesLocalConfig, and
	// nil for an EducatesConfig.
	Plan *localcerts.Plan
}

// prepareLocalCreate defaults the loaded config and takes from it what
// the create flow needs, checking it before anything is created.
//
// An EducatesLocalConfig gets the CLI defaults, the <host-IP>.nip.io
// fallback and the local certificate plan. An EducatesConfig gets none of
// them: its custom resources are deployed as declared and translated by
// TranslateEscape, so the CLI neither decides its certificates nor syncs
// cached Secrets for it. Only its target.cluster, which is CLI input and
// not a custom resource, gets the local kind's cluster defaults.
func (p *ProjectInfo) prepareLocalCreate(cfg v1alpha1.Config) (*localCreateInputs, error) {
	switch c := cfg.(type) {
	case *v1alpha1.EducatesLocalConfig:
		if err := applyLocalDefaults(c, p); err != nil {
			return nil, err
		}
		if err := validateClusterNodes(c.Cluster.Nodes); err != nil {
			return nil, err
		}
		// A secure install (the default; ingress.insecure not set)
		// serves HTTPS from a cached wildcard TLS certificate or a cached
		// root CA. Defaults have already run, so an empty domain has
		// become a <host-IP>.nip.io insecure install that needs neither;
		// only a domain with insecure left false requires one. Resolving
		// the local certificates is a pure local-cache read, so a missing
		// certificate source is reported, naming the generated root and
		// the mkcert paths, without touching Docker or the cluster at
		// all. The deploy at the end uses this same plan.
		plan, err := localcerts.Resolve(c)
		if err != nil {
			return nil, err
		}
		return &localCreateInputs{
			Deploy:  c,
			Cluster: c.Cluster,
			Domain:  c.Ingress.Domain,
			Plan:    plan,
		}, nil
	case *v1alpha1.EducatesConfig:
		clusterCfg := c.Target.Cluster
		clusterCfg.WithDefaults()
		if err := validateClusterNodes(clusterCfg.Nodes); err != nil {
			return nil, err
		}
		return &localCreateInputs{
			Deploy:  c,
			Cluster: clusterCfg,
			Domain:  c.IngressDomain(),
		}, nil
	default:
		return nil, fmt.Errorf("unsupported kind %q for local cluster create", cfg.GetKind())
	}
}

// applyLocalDefaults mirrors what render/deploy do before translation:
// CLI-binary defaults for operator.image, host-IP nip.io for ingress.domain
// when empty.
func applyLocalDefaults(cfg *v1alpha1.EducatesLocalConfig, p *ProjectInfo) error {
	cfg.ApplyCLIDefaults(p.Version, p.ImageRepository)
	// Fills <host-IP>.nip.io and defaults ingress.insecure when the
	// domain was left empty; no-op when the user set one.
	if _, err := maybeApplyHostDomain(cfg); err != nil {
		return err
	}
	return nil
}

// validateClusterNodes checks a user-declared cluster.nodes list: when set,
// it must contain at least one control-plane node (kind cannot bootstrap
// without one) and use only the roles kind understands. An empty list keeps
// the default single control-plane cluster and needs no validation.
func validateClusterNodes(nodes []v1alpha1.ClusterNode) error {
	if len(nodes) == 0 {
		return nil
	}

	controlPlanes := 0
	for _, n := range nodes {
		switch n.Role {
		case "control-plane":
			controlPlanes++
		case "worker":
		default:
			return fmt.Errorf("cluster.nodes: invalid node role %q (must be \"control-plane\" or \"worker\")", n.Role)
		}
	}

	if controlPlanes == 0 {
		return fmt.Errorf("cluster.nodes: at least one node must have role \"control-plane\"")
	}

	return nil
}

// kindBootstrapFromConfig pulls the kind-template inputs from the cluster
// declaration of an EducatesLocalConfig or EducatesConfig.
func kindBootstrapFromConfig(clusterCfg v1alpha1.LocalClusterConfig) *cluster.KindBootstrapInput {
	mounts := make([]cluster.KindVolumeMount, len(clusterCfg.VolumeMounts))
	for i, m := range clusterCfg.VolumeMounts {
		mounts[i] = cluster.KindVolumeMount{
			HostPath:      m.HostPath,
			ContainerPath: m.ContainerPath,
		}
		// The v4 source-of-truth is *bool (so "unset" round-trips
		// through YAML); collapse to the template-friendly pair here.
		if m.ReadOnly != nil {
			mounts[i].HasReadOnly = true
			mounts[i].ReadOnly = *m.ReadOnly
		}
	}

	nodes := make([]cluster.KindNode, len(clusterCfg.Nodes))
	for i, n := range clusterCfg.Nodes {
		taints := make([]cluster.KindNodeTaint, len(n.Taints))
		for j, t := range n.Taints {
			taints[j] = cluster.KindNodeTaint{Key: t.Key, Value: t.Value, Effect: t.Effect}
		}
		nodes[i] = cluster.KindNode{
			Role:   n.Role,
			Labels: n.Labels,
			Taints: taints,
		}
	}

	return &cluster.KindBootstrapInput{
		ListenAddress: clusterCfg.ListenAddress,
		ApiServer: cluster.KindApiServer{
			Address: clusterCfg.ApiServer.Address,
			Port:    clusterCfg.ApiServer.Port,
		},
		Networking: cluster.KindNetworking{
			ServiceSubnet: clusterCfg.Networking.ServiceSubnet,
			PodSubnet:     clusterCfg.Networking.PodSubnet,
		},
		VolumeMounts: mounts,
		Nodes:        nodes,
	}
}

func registryMirrorFromConfig(m v1alpha1.RegistryMirror) registry.MirrorConfig {
	return registry.MirrorConfig{
		Mirror:   m.Mirror,
		URL:      m.URL,
		Username: m.Username,
		Password: m.Password,
		Port:     m.Port,
		BindIP:   m.BindIP,
	}
}

// tailCallDeploy translates the already-loaded+defaulted config and
// runs the install with the local certificate plan the preflight in
// runLocalClusterCreate resolved, which is nil for an EducatesConfig.
// Shares the translate → deploy plumbing with runDeploy via
// translateAndDeploy.
func tailCallDeploy(ctx context.Context, w io.Writer, cfg v1alpha1.Config, plan *localcerts.Plan, o *LocalClusterCreateOptions) error {
	return translateAndDeploy(ctx, w, cfg, plan, deployPipelineFlags{
		Kubeconfig: o.Kubeconfig,
		Context:    o.Context,
		Timeout:    o.Timeout,
		Verbose:    o.Verbose,
	})
}
