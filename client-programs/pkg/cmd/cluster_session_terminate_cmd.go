package cmd

import (
	"fmt"

	"github.com/educates/educates-training-platform/client-programs/pkg/cluster"
	"github.com/educates/educates-training-platform/client-programs/pkg/constants"
	"github.com/educates/educates-training-platform/client-programs/pkg/educates/resources/sessions"
	"github.com/educates/educates-training-platform/client-programs/pkg/utils"
	"github.com/spf13/cobra"
)

type ClusterSessionTerminateOptions struct {
	KubeconfigOptions
	Portal string
	Name   string
}

const clusterSessionTerminateExample = `
# Terminate running Educatessession "my-session" in default Educates portal
educates cluster session terminate my-session

# Terminate running Educates session "my-session" using a specific portal
educates cluster session terminate my-session --portal=my-portal

# Terminate running Educates session "my-session" using a specific portal and context
educates cluster session terminate my-session --portal=my-portal --kubeconfig ~/.kube/config --context=my-context
`

func (o *ClusterSessionTerminateOptions) Run() error {
	var err error

	clusterConfig := cluster.NewClusterConfig(o.Kubeconfig, o.Context)

	manager := sessions.NewSessionManager()
	result, err := manager.TerminateSession(sessions.TerminateSessionConfig{
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

func (p *ProjectInfo) NewClusterSessionTerminateCmd() *cobra.Command {
	var o ClusterSessionTerminateOptions

	var c = &cobra.Command{
		Args:    func(cmd *cobra.Command, args []string) error {
			if len(args) != 1 {
				return utils.CmdError(cmd, "session name is required", "NAME")
			}
			return nil
		},
		Use:     "delete NAME",
		Aliases: []string{"terminate"},
		Short:   "Terminate running session in Kubernetes",
		RunE:    func(_ *cobra.Command, args []string) error { o.Name = args[0]; return o.Run() },
		Example: clusterSessionTerminateExample,
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
