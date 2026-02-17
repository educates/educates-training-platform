package cmd

import (
	"fmt"

	"github.com/educates/educates-training-platform/client-programs/pkg/cluster"
	"github.com/educates/educates-training-platform/client-programs/pkg/constants"
	educatesResources "github.com/educates/educates-training-platform/client-programs/pkg/educates/resources"
	"github.com/educates/educates-training-platform/client-programs/pkg/utils"
	"github.com/spf13/cobra"
)

type ClusterSessionStatusOptions struct {
	KubeconfigOptions
	Portal string
	Name   string
}

const clusterSessionStatusExample = `
# Get status of Educates session "my-session" in default Educates portal
educates cluster session status my-session

# Get status of Educates session "my-session" using a specific portal
educates cluster session status my-session --portal=my-portal

# Get status of Educates session "my-session"  using a specific portal and context
educates cluster session status my-session --portal=my-portal --kubeconfig ~/.kube/config --context=my-context
`

func (o *ClusterSessionStatusOptions) Run() error {
	var err error

	clusterConfig := cluster.NewClusterConfig(o.Kubeconfig, o.Context)

	manager := educatesResources.NewSessionManager()
	result, err := manager.SessionStatus(educatesResources.SessionStatusConfig{
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

func (p *ProjectInfo) NewClusterSessionStatusCmd() *cobra.Command {
	var o ClusterSessionStatusOptions

	var c = &cobra.Command{
		Args:  func(cmd *cobra.Command, args []string) error {
			if len(args) != 1 {
				return utils.CmdError(cmd, "session name is required", "NAME")
			}
			return nil
		},
		Use:   "status NAME",
		Short: "Output status of session in Kubernetes",
		RunE:  func(_ *cobra.Command, args []string) error { o.Name = args[0]; return o.Run() },
		Example: clusterSessionStatusExample,
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
