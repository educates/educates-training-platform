package cmd

import (
	"fmt"

	"github.com/educates/educates-training-platform/client-programs/pkg/cluster"
	"github.com/educates/educates-training-platform/client-programs/pkg/constants"
	educatesResources "github.com/educates/educates-training-platform/client-programs/pkg/educates/resources"
	"github.com/educates/educates-training-platform/client-programs/pkg/utils"
	"github.com/spf13/cobra"
)

type ClusterSessionExtendOptions struct {
	KubeconfigOptions
	Portal string
	Name   string
}

const clusterSessionExtendExample = `
# Extend duration of session "my-session" in Kubernetes
educates cluster session extend my-session SESSION_NAME

# Extend duration of session "my-session" in Kubernetes using a specific portal
educates cluster session extend my-session SESSION_NAME --portal=my-portal

# Extend duration of session "my-session" in Kubernetes using a specific portal and context
educates cluster session extend my-session SESSION_NAME --portal=my-portal --kubeconfig ~/.kube/config --context=my-context
`

func (o *ClusterSessionExtendOptions) Run() error {
	var err error

	clusterConfig, err := cluster.NewClusterConfigIfAvailable(o.Kubeconfig, o.Context)

	if err != nil {
		return err
	}

	manager := educatesResources.NewSessionManager()
	result, err := manager.ExtendSession(educatesResources.ExtendSessionConfig{
		ClusterConfig: clusterConfig,
		Portal: o.Portal,
		Name: o.Name,
	})
	if err != nil {
		return err
	}

	fmt.Println(result)

	return nil
}

func (p *ProjectInfo) NewClusterSessionExtendCmd() *cobra.Command {
	var o ClusterSessionExtendOptions

	var c = &cobra.Command{
		Args:  func(cmd *cobra.Command, args []string) error {
			if len(args) != 1 {
				return utils.CmdError(cmd, "session name is required", "NAME")
			}
			return nil
		},
		Use:   "extend NAME",
		Short: "Extend duration of session in Kubernetes",
		RunE:  func(_ *cobra.Command, args []string) error { o.Name = args[0]; return o.Run() },
		Example: clusterSessionExtendExample,
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
	c.Flags().StringVarP(
		&o.Portal,
		"portal",
		"p",
		constants.DefaultPortalName,
		"name of the training portal",
	)

	return c
}
