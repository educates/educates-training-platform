package cmd

import (
	"fmt"

	"github.com/educates/educates-training-platform/client-programs/pkg/cluster"
	"github.com/educates/educates-training-platform/client-programs/pkg/constants"
	"github.com/educates/educates-training-platform/client-programs/pkg/educatesrestapi"
	"github.com/pkg/errors"
	"github.com/spf13/cobra"
)

type ClusterSessionTerminateOptions struct {
	KubeconfigOptions
	Portal string
	Name   string
}

func (o *ClusterSessionTerminateOptions) Run() error {
	var err error

	clusterConfig := cluster.NewClusterConfig(o.Kubeconfig, o.Context)

	catalogApiRequester := educatesrestapi.NewWorkshopsCatalogRequester(
		clusterConfig,
		o.Portal,
	)
	logout, err := catalogApiRequester.Login()
	defer logout()
	if err != nil {
		return errors.Wrap(err, "failed to login to training portal")
	}

	details, err := catalogApiRequester.TerminateWorkshopSession(o.Name)
	if err != nil {
		return err
	}

	fmt.Println("Started:", details.Started)
	fmt.Println("Expires:", details.Expires)
	fmt.Println("Expiring:", details.Expiring)
	fmt.Println("Countdown:", details.Countdown)
	fmt.Println("Extendable:", details.Extendable)
	fmt.Println("Status:", details.Status)

	return nil
}

func (p *ProjectInfo) NewClusterSessionTerminateCmd() *cobra.Command {
	var o ClusterSessionTerminateOptions

	var c = &cobra.Command{
		Args:    cobra.ExactArgs(1),
		Use:     "delete NAME",
		Aliases: []string{"terminate"},
		Short:   "Terminate running session in Kubernetes",
		RunE:    func(_ *cobra.Command, args []string) error { o.Name = args[0]; return o.Run() },
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
