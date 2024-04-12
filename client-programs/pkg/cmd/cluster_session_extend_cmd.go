package cmd

import (
	"fmt"

	"github.com/pkg/errors"
	"github.com/spf13/cobra"
	"github.com/vmware-tanzu-labs/educates-training-platform/client-programs/pkg/cluster"
	"github.com/vmware-tanzu-labs/educates-training-platform/client-programs/pkg/educatesrestapi"
)

type ClusterSessionExtendOptions struct {
	Kubeconfig string
	Portal     string
	Name       string
}

func (o *ClusterSessionExtendOptions) Run() error {
	var err error

	clusterConfig := cluster.NewClusterConfig(o.Kubeconfig)

	if !cluster.IsClusterAvailable(clusterConfig) {
		return errors.New("Cluster is not available")
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

	fmt.Println("Started:", details.Started)
	fmt.Println("Expires:", details.Expires)
	fmt.Println("Expiring:", details.Expiring)
	fmt.Println("Countdown:", details.Countdown)
	fmt.Println("Extendable:", details.Extendable)
	fmt.Println("Status:", details.Status)

	return nil
}

func (p *ProjectInfo) NewClusterSessionExtendCmd() *cobra.Command {
	var o ClusterSessionExtendOptions

	var c = &cobra.Command{
		Args:  cobra.ExactArgs(1),
		Use:   "extend NAME",
		Short: "Extend duration of session in Kubernetes",
		RunE:  func(_ *cobra.Command, args []string) error { o.Name = args[0]; return o.Run() },
	}

	c.Flags().StringVar(
		&o.Kubeconfig,
		"kubeconfig",
		"",
		"kubeconfig file to use instead of $KUBECONFIG or $HOME/.kube/config",
	)
	c.Flags().StringVarP(
		&o.Portal,
		"portal",
		"p",
		"educates-cli",
		"name of the training portal",
	)

	return c
}
