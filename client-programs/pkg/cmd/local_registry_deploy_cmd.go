package cmd

import (
	"fmt"

	"github.com/pkg/errors"
	"github.com/spf13/cobra"

	"github.com/educates/educates-training-platform/client-programs/pkg/cluster"
	"github.com/educates/educates-training-platform/client-programs/pkg/registry"
	"github.com/educates/educates-training-platform/client-programs/pkg/utils"
)

type LocalRegistryDeployOptions struct {
	KubeconfigOptions
	BindIP string
}

func (o *LocalRegistryDeployOptions) Run() error {
	err := registry.DeployRegistry(o.BindIP)

	if err != nil {
		return errors.Wrap(err, "failed to deploy registry")
	}

	// This will fail if you do not have a Kubernetes cluster, but we can still
	// deploy just the image registry alone without Kubernetes. If a Kubernetes
	// cluster is created later, then the registry service will be added then.

	clusterConfig, err := cluster.NewClusterConfigIfAvailable(o.Kubeconfig, o.Context)

	if err != nil {
		fmt.Println("Warning: Kubernetes cluster not available")
		return nil
	}

	client, err := clusterConfig.GetClient()

	if err != nil {
		fmt.Println("Warning: Kubernetes cluster not updated with registry service.")

		return nil
	}

	if err = registry.UpdateRegistryK8SService(client); err != nil {
		return errors.Wrap(err, "failed to create service for registry")
	}

	return nil
}

func (p *ProjectInfo) NewLocalRegistryDeployCmd() *cobra.Command {
	var o LocalRegistryDeployOptions

	var c = &cobra.Command{
		Args:  cobra.NoArgs,
		Use:   "deploy",
		Short: "Deploys a local image registry",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ip, err := utils.ValidateAndResolveIP(o.BindIP)
			if err != nil {
				return errors.Wrap(err, "invalid registry bind IP")
			}
			o.BindIP = ip

			return o.Run()
		},
	}

	c.Flags().StringVar(
		&o.Kubeconfig,
		"kubeconfig",
		"",
		"kubeconfig file to use instead of $KUBECONFIG or $HOME/.kube/config",
	)

	c.Flags().StringVar(
		&o.Context,
		"context",
		"",
		"Context to use from Kubeconfig",
	)

	c.Flags().StringVar(
		&o.BindIP,
		"bind-ip",
		"127.0.0.1",
		"Bind ip for the registry service",
	)

	return c
}
