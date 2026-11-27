package cmd

import (
	"context"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/educates/educates-training-platform/client-programs/pkg/cluster"
	"github.com/educates/educates-training-platform/client-programs/pkg/config"
	"github.com/educates/educates-training-platform/client-programs/pkg/config/v1alpha1"
	"github.com/educates/educates-training-platform/client-programs/pkg/constants"
	"github.com/educates/educates-training-platform/client-programs/pkg/deployer"
	"github.com/educates/educates-training-platform/client-programs/pkg/deployer/progress"
	"github.com/educates/educates-training-platform/client-programs/pkg/localcerts"
	"github.com/educates/educates-training-platform/client-programs/pkg/localclusters"
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

  # Create the named local cluster test2 beside the default one, writing
  # its config when it has none, served at test2.<router address>.nip.io:
  educates local cluster create --local-cluster test2

  # Create it on a domain of its own, under the default cluster's:
  educates local cluster create --local-cluster test2 --domain test2.educates.test
`

type LocalClusterCreateOptions struct {
	Config            string
	LocalConfig       bool
	Kubeconfig        string
	ClusterImage      string
	KubernetesVersion string
	ClusterOnly       bool
	RegistryBindIP    string
	Timeout           time.Duration
	Verbose           bool

	// Domain writes the local cluster's missing config.
	Domain string
}

func (p *ProjectInfo) NewLocalClusterCreateCmd() *cobra.Command {
	var o LocalClusterCreateOptions

	c := &cobra.Command{
		Args:  cobra.NoArgs,
		Use:   "create",
		Short: "Create a local kind cluster, bring up the registry, and install Educates",
		Long: `Lays down a full laptop install in one command:

  1. Loads EducatesLocalConfig (or EducatesConfig with target.provider=kind).
  2. Creates the local cluster's kind cluster: 'educates' for the default
     local cluster, 'educates-<name>' for a named one.
  3. Brings up the always-on localhost:5001 registry.
  4. Sets up the loopback service for 'educates serve-workshop'.
  5. Tail-calls into the platform deploy pipeline
     (helm install operator + apply 4 platform CRs + wait Ready), always
     against the cluster just created, through its own kube context.

An EducatesConfig creates the kind cluster from target.cluster and deploys
its custom resources as declared, with no CLI defaulting: no nip.io
domain, and no certificates or Secrets taken from the local secrets
cache. Any Secret the custom resources reference has to be created in the
cluster before the deploy can become ready.

Every local cluster is served through the router, the container
educates-router, which publishes ports 80 and 443 on the router address,
the one host address every local cluster shares, and sends each request
to the cluster that owns its name. It also publishes port 6443 there, and
sends each connection to the API server of the cluster whose control-plane
node its TLS server name names. Kind nodes publish no ports but their API
server's, on a loopback port that kind's kube context uses, and a cluster
has no host address of its own. Before the config loads, create
writes <data-home>/router.yaml when it is missing, with the host's IP
address as the router address. An existing router.yaml is left as it is.
With no ingress.domain the default local cluster takes the nip.io name of
the router address, such as 10-10-10-1.nip.io, and a named one its name
under it, such as test2.10-10-10-1.nip.io, and is served over plain HTTP.

When the local cluster's config is missing, create writes it, declaring
the optional --domain and nothing else.

Before kind is touched, create refuses a domain of the cluster, its
ingress domain or one of its resolver.extraDomains, that another local
cluster, running or stopped, claims, naming that cluster. A subdomain of
another cluster's domain is allowed: the most specific domain wins. It
also refuses a cluster.apiServer.port of 6443 on the router address or on
every host address, which would take the router's port.

Before kind is touched, create creates the router when it is missing, and
re-creates it when it runs another image than the one this CLI pins,
publishes on another address than router.yaml's, does not publish port
6443, or has stopped, which drops every connection through it. The
router's start is the check that ports 80, 443 and 6443 are free on the
router address: it fails naming any
local cluster created by an earlier release, which publishes them from
its node, to delete and create again. On Docker Desktop 4.97 or later,
creating the router refuses to run while "Allow privileged port
forwarding to external machines" is off, in Settings > Advanced, since
ports 80 and 443 on a non-loopback address need it. Once the cluster
exists, the router's configuration is rendered again and reloaded, which
keeps every other cluster's open connections.

--local-cluster <name>, or EDUCATES_LOCAL_CLUSTER, creates a named local
cluster beside the default one. Its config lives in
<data-home>/clusters/<name>/config.yaml. --config with --local-cluster
takes the config from the file and the cluster's name from the flag, and
makes the cluster's folder <data-home>/clusters/<name>/ without copying
the file into it.

Every registry mirror container that exists is linked into each node of
the new cluster, whichever command created it. A mirror the registry
mirrors file, mirrors.yaml in the data home, declares is created first
when it is missing. Create refuses to run when that file does not load,
or when a declared mirror's container exists with another URL or
username.

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

			id, err := selectedLocalCluster(cmd)
			if err != nil {
				return err
			}
			return p.runLocalClusterCreate(cmd.Context(), cmd.OutOrStdout(), &o, id)
		},
	}

	c.Flags().StringVarP(&o.Config, "config", "c", "", "path to a CLI config file (any kind)")
	c.Flags().BoolVar(&o.LocalConfig, "local-config", false, "use the local cluster's config in the data home (default when --config is not given)")
	addLocalClusterFlag(c)
	c.Flags().StringVar(&o.Domain, localclusters.DomainFlagName, "", "ingress domain written to the local cluster's config when it has none (default: the nip.io name of the router address for the default local cluster, and the cluster's name under it for a named one)")
	c.Flags().StringVar(&o.Kubeconfig, "kubeconfig", "", "kubeconfig file kind writes the cluster's context into, and the platform deploy uses (defaults to ~/.kube/config)")
	c.Flags().StringVar(&o.ClusterImage, "kind-cluster-image", "", "docker image to use when booting the kind cluster (overrides --kubernetes-version)")
	c.Flags().StringVar(&o.KubernetesVersion, "kubernetes-version", constants.DefaultKubernetesVersion, fmt.Sprintf("Kubernetes version for the kind cluster (supported: %v)", constants.SupportedKubernetesVersions()))
	c.Flags().BoolVar(&o.ClusterOnly, "cluster-only", false, "create kind cluster + registry; skip the platform deploy")
	c.Flags().StringVar(&o.RegistryBindIP, "registry-bind-ip", "127.0.0.1", "bind IP for the always-on localhost:5001 registry")
	c.Flags().DurationVar(&o.Timeout, "timeout", deployer.DefaultTimeout, "per-CR Ready=True wait timeout (passed through to deploy)")
	c.Flags().BoolVar(&o.Verbose, "verbose", false, "show helm SDK debug output on stderr")
	c.MarkFlagsMutuallyExclusive("config", "local-config")

	return c
}

func (p *ProjectInfo) runLocalClusterCreate(ctx context.Context, w io.Writer, o *LocalClusterCreateOptions, id localclusters.Identity) error {
	cfg, configPath, createdConfig, err := loadLocalConfig(w, o, id)
	if err != nil {
		return err
	}
	switch {
	case createdConfig && id.IsDefault():
		fmt.Fprintf(w, "No local config found — wrote a default EducatesLocalConfig to %s\n", configPath)
		fmt.Fprintf(w, "Continuing with defaults; view or edit it with 'educates local config view' / 'educates local config edit'.\n\n")
	case createdConfig:
		fmt.Fprintf(w, "Local cluster %q had no config; wrote one to %s\n\n", id.Name, configPath)
	}

	// 0. Preflight: fail fast (before any docker / kind / k8s mutation)
	//    when the cluster already exists, the registry mirrors file does
	//    not load or a mirror it declares has a container with other
	//    values, another local cluster claims one of its domains, or a
	//    secure install, unless --cluster-only skips it, has no
	//    certificate source. The certificate case is the one the deploy
	//    resolves later: without this check it would only fail after the
	//    kind cluster and registry already exist.
	in, err := p.prepareLocalCreate(cfg, !o.ClusterOnly)
	if err != nil {
		return err
	}
	clusterConfig := cluster.NewKindClusterConfig(o.Kubeconfig, id)
	if exists, err := clusterConfig.ClusterExists(); err != nil {
		return err
	} else if exists {
		return fmt.Errorf("kind cluster '%s' already exists; run 'educates local cluster delete%s' first or use the existing cluster directly", id.KindName, localClusterFlagArg(id))
	}
	inventory, err := localClusterInventory(ctx)
	if err != nil {
		return err
	}
	// Every local cluster needs the registry mirrors the registry mirrors
	// file declares, and shares one container for each of them, which
	// step 4 reuses as it is. A file that does not load, or a declared
	// mirror whose container pulls from another URL or as another
	// username, is refused here rather than silently pulled through.
	declaredMirrors, err := config.LoadLocalMirrors(utils.GetEducatesHomeDir())
	if err != nil {
		return err
	}
	if err := inventory.CheckMirrors(declaredMirrors); err != nil {
		return err
	}
	// A name leads to one local cluster only, through the router and the
	// local DNS resolver alike, so a domain another local cluster, running
	// or stopped, claims is refused here, naming that cluster.
	resolverInput := localclusters.ResolverInput{
		LoadedConfig: localclusters.LoadedConfig{Identity: id, Config: in.Deploy, Source: configPath},
	}
	if err := inventory.CheckCreateDomains(resolverInput.LoadedConfig); err != nil {
		return err
	}
	// The router publishes port 6443 on the router address for every
	// local cluster's API server, so a config asking kind to publish the
	// API server there is refused before the router or kind is touched.
	if err := inventory.CheckCreateAPIServerPort(resolverInput.LoadedConfig); err != nil {
		return err
	}
	// The local DNS resolver serves the domains of every local cluster,
	// the default local cluster's config included when it has no cluster,
	// so a configuration it can't render is refused here too, whether or
	// not the resolver runs. Another cluster's config that the resolver
	// can't use is that cluster's problem, so it is only a warning. A
	// running resolver is refreshed once the kind cluster exists, from what
	// the Docker host holds then.
	_, leftOut, err := inventory.ResolverConfig(resolverInput)
	if err != nil {
		return err
	}
	warnLeftOutConfigs(w, leftOut)

	// The router, kind, registry, loopback and mirror phases all report
	// through one progress reporter so the create flow reads as a single
	// sequence of `→ … ✓` steps (the platform deploy tail-call builds its
	// own reporter against the same writer). Verbose turns off in-place
	// morphing so kind's full spinner and the per-step detail lines are
	// all preserved rather than overwritten.
	rep := progress.New(w, 0, isStdoutTTY(w) && !o.Verbose)

	// The router publishes ports 80, 443 and 6443 on the router address
	// for every local cluster, so it is created, or re-created on a new
	// image or address, before kind touches anything: its start is the check
	// that the ports are free, and its error names a cluster made by an
	// earlier release of the CLI that still publishes them from its node.
	// Creating it checks the router address and Docker Desktop's
	// privileged port setting first. A router that needs neither is
	// refreshed once the cluster exists.
	routerOps := dockerRouterOperations(utils.GetEducatesHomeDir(), w)
	if _, err := routerOps.ensureRouter(ctx, rep, inventory); err != nil {
		return err
	}

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
	// The cluster exists now, so a resolver that can't be refreshed is
	// only a warning. Its configuration is rendered again rather than
	// taken from the preflight, so that it serves every local cluster
	// another create running at the same time has made since: the last
	// create to refresh the resolver then serves them all. The configs
	// the resolver leaves out were warned about in the preflight.
	refreshLocalResolver(rep, func() ([]byte, []error, error) {
		content, _, err := observedResolverConfig(ctx, resolverInput)
		return content, nil, err
	})
	// The router routes the new cluster's names to its node from now on,
	// over the kind network kind has just made, which it joins. As for
	// the resolver, it renders every local cluster again, and a router it
	// can't refresh is only a warning.
	routerOps.refreshRouter(ctx, rep, localClusterInventory, resolverInput.LoadedConfig)
	// The registry, loopback and mirror steps talk to the cluster just
	// created, through its own context in the kubeconfig kind wrote it in.
	client, err := clusterConfig.Config.GetClient()
	if err != nil {
		return err
	}
	// Every node of the new cluster gets the registry and mirror config,
	// so pods scheduled on any node can pull through them.
	nodes, err := registryNodes(ctx, id)
	if err != nil {
		return err
	}

	// 2. always-on local registry + k8s Service for imgpkg pulls.
	if err := runStep(rep, "bringing up localhost:5001 registry", "ready", func(s progress.Step) error {
		if err := registry.DeployRegistryAndLinkToCluster(o.RegistryBindIP, nodes, client, s); err != nil {
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

	// 4. registry mirrors (pull-through caches): every mirror container
	//    that exists, whichever command created it, and every mirror the
	//    registry mirrors file declares, created first when missing.
	if err := linkRegistryMirrors(rep, inventory.MirrorsToLink(declaredMirrors), nodes); err != nil {
		return err
	}

	if o.ClusterOnly {
		rep.Note("cluster + registry ready (--cluster-only; skipped platform deploy)")
		noteKubeContext(rep, w, o.Kubeconfig, clusterConfig.Config.Kubeconfig, id)
		return nil
	}

	// 5. tail-call the v4 deploy. We have the loaded config, so rather
	//    than re-loading it inside runDeploy, translate here and call
	//    deployer.Deploy directly.
	flags := createDeployFlags(o, clusterConfig.Config.Kubeconfig, id)
	flags.Record = deployRevisionInput(p.Version, in.Deploy.GetKind(), "local cluster create, "+absPath(configPath), configPath)
	if err := tailCallDeploy(ctx, w, in.Deploy, in.Plan, flags); err != nil {
		return err
	}
	noteKubeContext(rep, w, o.Kubeconfig, clusterConfig.Config.Kubeconfig, id)
	return nil
}

// loadLocalConfig returns the loaded v4 config of the local cluster id +
// the path it came from (used by error messages) + whether it just wrote
// a config in place of a missing one (so the caller can tell the user).
// Accepts EducatesLocalConfig directly or EducatesConfig with
// target.provider=kind; everything else errors.
//
// The config comes from --config when given, and otherwise from the
// cluster's config in the data home, written from --domain when missing,
// as localclusters.EnsureConfig writes it, for the default local cluster
// and a named one alike. A v3 install of the default local cluster is
// migrated first. Either way the config follows the rules of
// localclusters.LoadConfigFile, and a named cluster's folder exists once
// it loads: --config makes it, without copying the file into it.
//
// A config that declares no ingress domain takes it from the router
// address, so the router file, router.yaml in the data home, is written
// first when it is missing, as ensureRouterFile writes it, telling w,
// after the v3 migration, which carries the router address over.
func loadLocalConfig(w io.Writer, o *LocalClusterCreateOptions, id localclusters.Identity) (v1alpha1.Config, string, bool, error) {
	flags := localclusters.ConfigFlags{Domain: o.Domain}
	path, created := id.ConfigPath, false
	if o.Config != "" {
		path = o.Config
		if !flags.IsZero() {
			return nil, path, false, fmt.Errorf("--%s writes a missing config in the data home, so it can't be combined with --config",
				localclusters.DomainFlagName)
		}
		// The config stays in the file, but a named cluster's folder in
		// the data home is what makes its kind cluster an Educates
		// cluster.
		if err := localclusters.MakeClusterFolder(id); err != nil {
			return nil, path, false, err
		}
	} else if id.IsDefault() {
		// Unlike render/deploy, a first-time laptop create shouldn't
		// dead-end on "run config init first", so a missing config is
		// written in place, below. Migration of an existing v3 install
		// still takes priority, and an unmigratable one refuses.
		if err := config.MaybeMigrateV3(utils.GetEducatesHomeDir(), os.Stderr); err != nil {
			return nil, path, false, err
		}
	}
	if _, err := ensureRouterFile(w); err != nil {
		return nil, path, false, err
	}
	if o.Config == "" {
		var err error
		if created, err = localclusters.EnsureConfig(id, flags); err != nil {
			return nil, path, false, err
		}
	}
	cfg, err := localclusters.LoadConfigFile(id, path)
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

	// Cluster declares the kind cluster.
	Cluster v1alpha1.LocalClusterConfig

	// Domain is the ingress domain the loopback service resolves under.
	// Empty when the config declares none.
	Domain string

	// Plan is the local certificate plan of an EducatesLocalConfig the
	// platform is deployed with, and nil for an EducatesConfig or when
	// no deploy follows.
	Plan *localcerts.Plan
}

// prepareLocalCreate defaults the loaded config and takes from it what
// the create flow needs, checking it before anything is created.
//
// An EducatesLocalConfig, whose ingress.domain the local cluster rules
// have already defaulted, gets the CLI defaults, and the local certificate
// plan when deploy says the platform is deployed after the cluster is
// created. Only that deploy uses the plan, so a create with --cluster-only
// creates the cluster of a secure domain the local secrets cache has no
// certificate for yet. An EducatesConfig gets neither: its custom resources
// are deployed as declared and translated by TranslateEscape, so the CLI
// neither decides its certificates nor syncs cached Secrets for it.
func (p *ProjectInfo) prepareLocalCreate(cfg v1alpha1.Config, deploy bool) (*localCreateInputs, error) {
	switch c := cfg.(type) {
	case *v1alpha1.EducatesLocalConfig:
		c.ApplyCLIDefaults(p.Version, p.ImageRepository)
		if !deploy {
			return &localCreateInputs{
				Deploy:  c,
				Cluster: c.Cluster,
				Domain:  c.Ingress.Domain,
			}, nil
		}
		// A secure install (the default; ingress.insecure not set)
		// serves HTTPS from a cached wildcard TLS certificate or a cached
		// root CA. The local cluster rules have already run, so an empty
		// domain has become the insecure install on a nip.io name, which
		// needs neither;
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
		return &localCreateInputs{
			Deploy:  c,
			Cluster: clusterCfg,
			Domain:  c.IngressDomain(),
		}, nil
	default:
		return nil, fmt.Errorf("unsupported kind %q for local cluster create", cfg.GetKind())
	}
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

// linkRegistryMirrors links each of mirrors into the node containers
// nodes of a local cluster, one step each, creating first a mirror whose
// container is missing. A mirror that can't be linked into a node is a
// warning; one whose container can't be created or joined to the cluster
// network is an error.
func linkRegistryMirrors(rep progress.Reporter, mirrors []v1alpha1.RegistryMirror, nodes []string) error {
	for _, m := range mirrors {
		label := "registry mirror " + m.Mirror
		if m.URL != "" {
			label += " → " + m.URL
		}
		mc := registryMirrorFromConfig(m)
		if err := runStep(rep, label, "ready", func(s progress.Step) error {
			return registry.DeployMirrorAndLinkToCluster(&mc, nodes, s)
		}); err != nil {
			return fmt.Errorf("mirror %s: %w", m.Mirror, err)
		}
	}
	return nil
}

func registryMirrorFromConfig(m v1alpha1.RegistryMirror) registry.MirrorConfig {
	return registry.MirrorConfig{
		Mirror:   m.Mirror,
		URL:      m.URL,
		Username: m.Username,
		Password: m.Password,
	}
}

// tailCallDeploy translates the already-loaded+defaulted config and
// runs the install with the local certificate plan the preflight in
// runLocalClusterCreate resolved, which is nil for an EducatesConfig.
// Shares the translate → deploy plumbing with runDeploy via
// translateAndDeploy.
func tailCallDeploy(ctx context.Context, w io.Writer, cfg v1alpha1.Config, plan *localcerts.Plan, flags deployPipelineFlags) error {
	return translateAndDeploy(ctx, w, cfg, plan, flags)
}

// createDeployFlags returns the connection flags of create's platform
// deploy into the local cluster id, which kind created with its context
// in the kubeconfig file kubeconfig. The deploy goes through that file
// and the cluster's own context, whatever KUBECONFIG or the file's
// current context say, so it always lands in the cluster create just
// made.
func createDeployFlags(o *LocalClusterCreateOptions, kubeconfig string, id localclusters.Identity) deployPipelineFlags {
	return deployPipelineFlags{
		Kubeconfig: kubeconfig,
		Context:    id.Context,
		Timeout:    o.Timeout,
		Verbose:    o.Verbose,
	}
}
