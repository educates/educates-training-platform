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
// --context options of local registry deploy are deprecated.
const localRegistryDeployKubeDeprecation = "the command now configures every local cluster, each through its own kube context"

type LocalRegistryDeployOptions struct {
	BindIP  string
	Verbose bool
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
		var errs []error
		for _, t := range inventory.RegistryTargets() {
			errs = append(errs, linkRegistryToLocalCluster(t, s))
		}
		return errors.Join(errs...)
	})

	if err != nil {
		return fmt.Errorf("failed to deploy registry: %w", err)
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

	clusterConfig := cluster.NewKindClusterConfig("", t.Identity).Config
	if err := cluster.IsClusterAvailableCheck(&clusterConfig); err != nil {
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
warning.`,
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
	// configure. They are still accepted, and ignored.
	var kubeconfig, kubeContext string

	c.Flags().StringVar(
		&kubeconfig,
		"kubeconfig",
		"",
		"kubeconfig file to use instead of $KUBECONFIG or $HOME/.kube/config",
	)
	_ = c.Flags().MarkDeprecated("kubeconfig", localRegistryDeployKubeDeprecation)

	c.Flags().StringVar(
		&kubeContext,
		"context",
		"",
		"Context to use from Kubeconfig",
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
