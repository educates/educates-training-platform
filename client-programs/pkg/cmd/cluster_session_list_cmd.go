package cmd

import (
	"fmt"

	"github.com/educates/educates-training-platform/client-programs/pkg/cluster"
	"github.com/educates/educates-training-platform/client-programs/pkg/constants"
	educatesResources "github.com/educates/educates-training-platform/client-programs/pkg/educates/resources"
	"github.com/pkg/errors"
	"github.com/spf13/cobra"
)

type ClusterSessionListOptions struct {
	KubeconfigOptions
	Portal      string
	Environment string
}

const clusterSessionListExample = `
# List active Educates sessions in default Educates portal and cluster
educates cluster session list

# List active Educates sessions using a specific portal
educates cluster session list --portal=my-portal

# List active Educates sessions in Kubernetes using a specific portal and context
educates cluster session list --portal=my-portal --kubeconfig ~/.kube/config --context=my-context
`

func (o *ClusterSessionListOptions) Run() error {
	var err error

	clusterConfig, err := cluster.NewClusterConfigIfAvailable(o.Kubeconfig, o.Context)

	if err != nil {
		return err
	}

	dynamicClient, err := clusterConfig.GetDynamicClient()

	if err != nil {
		return errors.Wrapf(err, "unable to create Kubernetes client")
	}

	manager := educatesResources.NewSessionManager()

	list, err := manager.ListSessions(educatesResources.ListSessionsConfig{
		Client: dynamicClient,
		Portal: o.Portal,
		Environment: o.Environment,
	})
	if err != nil {
		return err
	}

	fmt.Println(list)

	return nil
}

func (p *ProjectInfo) NewClusterSessionListCmd() *cobra.Command {
	var o ClusterSessionListOptions

	var c = &cobra.Command{
		Args:  cobra.NoArgs,
		Use:   "list",
		Short: "List active sessions in Kubernetes",
		RunE:  func(_ *cobra.Command, _ []string) error { return o.Run() },
		Example: clusterSessionListExample,
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
	c.Flags().StringVarP(
		&o.Environment,
		"environment",
		"e",
		"",
		"name of the workshop environment to filter",
	)

	return c
}
