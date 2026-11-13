package cmd

import (
	"fmt"

	"github.com/educates/educates-training-platform/client-programs/pkg/cluster"
	"github.com/educates/educates-training-platform/client-programs/pkg/educatesrestapi"
	"github.com/pkg/errors"
	"github.com/spf13/cobra"
)

var clusterSessionExtendExample = `
  # Extend the duration of a workshop session:
  educates cluster session extend my-session-name
`

type ClusterSessionExtendOptions struct {
	KubeconfigOptions
	Portal string
	Name   string
}

func (o *ClusterSessionExtendOptions) Run() error {
	var err error

	clusterConfig, err := cluster.NewClusterConfigIfAvailable(o.Kubeconfig, o.Context)

	if err != nil {
		return err
	}

	catalogApiRequester := educatesrestapi.NewWorkshopsCatalogRequester(
		clusterConfig,
		o.Portal,
	)
	logout, err := catalogApiRequester.Login()
	defer logout()
	if err != nil {
		return errors.Wrap(err, "failed to login to training portal")
	}

	details, err := catalogApiRequester.ExtendWorkshopSession(o.Name)
	if err != nil {
		return err
	}

	fmt.Println(printSessionDetails(details))

	return nil
}

func (p *ProjectInfo) NewClusterSessionExtendCmd() *cobra.Command {
	var o ClusterSessionExtendOptions

	var c = &cobra.Command{
		Args:    exactArgs(1, "session name is required", "NAME"),
		Use:     "extend NAME",
		Short:   "Extend duration of session in Kubernetes",
		Example: clusterSessionExtendExample,
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
	addLocalClusterContextFlag(c, &o.KubeconfigOptions)
	c.Flags().StringVarP(
		&o.Portal,
		"portal",
		"p",
		"educates-cli",
		"name of the training portal",
	)

	return c
}
