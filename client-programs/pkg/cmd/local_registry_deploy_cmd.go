package cmd

import (
	"context"
	"errors"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/educates/educates-training-platform/client-programs/pkg/cluster"
	"github.com/educates/educates-training-platform/client-programs/pkg/deployer/progress"
	"github.com/educates/educates-training-platform/client-programs/pkg/localclusters"
	"github.com/educates/educates-training-platform/client-programs/pkg/registry"
	"github.com/educates/educates-training-platform/client-programs/pkg/utils"
)

var localRegistryDeployExample = `
  # Deploy a local image registry bound to localhost:
  educates local registry deploy

  # Deploy a local image registry reachable from the local network:
  educates local registry deploy --bind-ip 192.168.1.10
`

// localRegistryDeployKubeDeprecation is why the --kubeconfig and
// --context options of local registry deploy are deprecated, and what
// they still do.
const localRegistryDeployKubeDeprecation = "it still names a cluster in which the command also creates the registry " +
	"Service and EndpointSlice, but the command now configures every local cluster through its own kube context, " +
	"and the option will be removed in a future release"

type LocalRegistryDeployOptions struct {
	BindIP  string
	Verbose bool

	// KubeconfigOptions are the deprecated --kubeconfig and --context
	// options, which name a cluster to create the registry Service and
	// EndpointSlice in besides every local cluster. Neither set names
	// none.
	KubeconfigOptions
}

func (o *LocalRegistryDeployOptions) Run(ctx context.Context) error {
	err := stepOnStdout(o.Verbose, "deploy local registry", "ready", func(s progress.Step) error {
		if err := registry.DeployRegistry(o.BindIP, s); err != nil {
			return err
		}

		// Every local cluster shares the registry, so every one of them is
		// configured to pull from it. The registry also serves without any
		// cluster, which then configures itself when it is created.
		inventory, err := localClusterInventory(ctx)
		if err != nil {
			return err
		}
		targets := inventory.RegistryTargets()
		var errs []error
		for _, t := range targets {
			errs = append(errs, linkRegistryToLocalCluster(t, s))
		}

		// The deprecated options still name one more cluster, as they did
		// before the command configured every local cluster, unless it is
		// a local cluster just configured.
		if o.Kubeconfig != "" || o.Context != "" {
			if reachesALocalCluster(o.KubeconfigOptions, targets) {
				s.Update("the cluster --kubeconfig and --context name is a local cluster, already configured")
			} else {
				errs = append(errs, updateRegistryServiceIn(o.KubeconfigOptions, s))
			}
		}
		return errors.Join(errs...)
	})

	if err != nil {
		return fmt.Errorf("failed to deploy registry: %w", err)
	}

	return nil
}

// reachesALocalCluster reports whether the kube context the deprecated
// options kubeOpts name, the current context of their kubeconfig file
// when --context is not given, reaches the API server of one of the local
// clusters targets through its own kube context. Such a cluster is
// configured with every other local cluster, so it is not configured
// twice. A context that can't be resolved reaches none.
func reachesALocalCluster(kubeOpts KubeconfigOptions, targets []localclusters.RegistryTarget) bool {
	server := kubeContextServer(kubeOpts.Kubeconfig, kubeOpts.Context)
	if server == "" {
		return false
	}
	for _, t := range targets {
		if kubeContextServer(localClusterKubeconfig(""), t.Identity.Context) == server {
			return true
		}
	}
	return false
}

// kubeContextServer returns the API server URL of the kube context
// kubeContext in the kubeconfig file kubeconfig, of its current context
// when kubeContext is empty, with the default kubeconfig files when
// kubeconfig is empty. It returns nothing when the context can't be
// resolved.
func kubeContextServer(kubeconfig, kubeContext string) string {
	restConfig, err := cluster.GetConfig(kubeconfig, kubeContext)
	if err != nil {
		return ""
	}
	return restConfig.Host
}

// updateRegistryServiceIn creates the registry Service and EndpointSlice,
// pointing at the local registry, in the cluster the deprecated options
// kubeOpts name. A cluster that is not available is only a warning, since
// the registry serves without it.
func updateRegistryServiceIn(kubeOpts KubeconfigOptions, s progress.Step) error {
	desc := "the current kube context"
	if kubeOpts.Context != "" {
		desc = fmt.Sprintf("kube context %q", kubeOpts.Context)
	}
	s.Update("configuring " + desc)
	clusterConfig, err := cluster.NewClusterConfigIfAvailable(kubeOpts.Kubeconfig, kubeOpts.Context)
	if err != nil {
		s.Warn(fmt.Sprintf("%s not updated with the registry service: %v", desc, err))
		return nil
	}
	client, err := clusterConfig.GetClient()
	if err != nil {
		s.Warn(fmt.Sprintf("%s not updated with the registry service: %v", desc, err))
		return nil
	}
	if err := registry.UpdateRegistryK8SService(client); err != nil {
		return fmt.Errorf("failed to create service for registry in %s: %w", desc, err)
	}
	return nil
}

// linkRegistryToLocalCluster configures the local cluster t to pull from
// the local registry: the containerd host config of each of its nodes,
// and in the cluster the registry Service and EndpointSlice and the
// local-registry-hosting ConfigMap, through the cluster's own context in
// the kubeconfig file local cluster create writes it into. A stopped or
// unreachable cluster, or a node that can't take the config, is only a
// warning, since the registry serves without it.
func linkRegistryToLocalCluster(t localclusters.RegistryTarget, s progress.Step) error {
	desc := t.Identity.Description()
	if t.State == localclusters.Stopped {
		s.Warn(fmt.Sprintf("%s is stopped and was not configured; run 'educates local registry deploy' again once it runs", desc))
		return nil
	}

	s.Update("configuring " + desc)
	if err := registry.LinkRegistryToKindNodes(t.Nodes, s); err != nil {
		s.Warn(fmt.Sprintf("registry not added to the kind nodes of %s: %v", desc, err))
	}

	clusterConfig := cluster.NewClusterConfig(localClusterKubeconfig(""), t.Identity.Context)
	if err := cluster.IsClusterAvailableCheck(clusterConfig); err != nil {
		s.Warn(fmt.Sprintf("%s not updated with the registry service: %v", desc, err))
		return nil
	}
	client, err := clusterConfig.GetClient()
	if err != nil {
		s.Warn(fmt.Sprintf("%s not updated with the registry service: %v", desc, err))
		return nil
	}
	if err := registry.UpdateRegistryK8SService(client); err != nil {
		return fmt.Errorf("failed to create service for registry in %s: %w", desc, err)
	}
	if err := registry.DocumentLocalRegistry(client); err != nil {
		return fmt.Errorf("failed to document registry config in %s: %w", desc, err)
	}
	return nil
}

func (p *ProjectInfo) NewLocalRegistryDeployCmd() *cobra.Command {
	var o LocalRegistryDeployOptions

	var c = &cobra.Command{
		Args:  cobra.NoArgs,
		Use:   "deploy",
		Short: "Deploys a local image registry",
		Long: `Deploys the local image registry on localhost:5001, or keeps the one which
already exists and moves it to its fixed address on the kind network.

Every local cluster shares the registry, so the command configures every
one of them to pull from it: each node's containerd config for
localhost:5001 and registry.default.svc.cluster.local, and in each cluster,
through its own kube context, the registry Service and EndpointSlice and
the local-registry-hosting ConfigMap. A stopped cluster is skipped with a
warning.

The deprecated --kubeconfig and --context options still name one more
cluster, as they did before the command configured every local cluster:
the command also creates the registry Service and EndpointSlice in it,
unless it is a local cluster, which is configured once. They will be
removed in a future release.`,
		Example: localRegistryDeployExample,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ip, err := utils.ValidateAndResolveIP(o.BindIP)
			if err != nil {
				return fmt.Errorf("invalid registry bind IP: %w", err)
			}
			o.BindIP = ip

			return o.Run(cmd.Context())
		},
	}

	// --kubeconfig and --context chose the one cluster the command used to
	// configure. Deprecated, they still name a cluster it configures too.
	c.Flags().StringVar(
		&o.Kubeconfig,
		"kubeconfig",
		"",
		"deprecated: kubeconfig file of a cluster to also create the registry Service and EndpointSlice in, besides every local cluster, instead of $KUBECONFIG or $HOME/.kube/config",
	)
	_ = c.Flags().MarkDeprecated("kubeconfig", localRegistryDeployKubeDeprecation)

	c.Flags().StringVar(
		&o.Context,
		"context",
		"",
		"deprecated: kube context of a cluster to also create the registry Service and EndpointSlice in, besides every local cluster, instead of the current context",
	)
	_ = c.Flags().MarkDeprecated("context", localRegistryDeployKubeDeprecation)

	c.Flags().StringVar(
		&o.BindIP,
		"bind-ip",
		"127.0.0.1",
		"Bind ip for the registry service",
	)

	c.Flags().BoolVar(
		&o.Verbose,
		"verbose",
		false,
		"show per-step detail instead of a single collapsed line",
	)

	return c
}
