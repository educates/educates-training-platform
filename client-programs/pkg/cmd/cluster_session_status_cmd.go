package cmd

import (
	"fmt"

	"github.com/educates/educates-training-platform/client-programs/pkg/cluster"
	"github.com/educates/educates-training-platform/client-programs/pkg/educatesrestapi"
	"github.com/educates/educates-training-platform/client-programs/pkg/utils"
	"github.com/pkg/errors"
	"github.com/spf13/cobra"
)

// printSessionDetails renders a workshop session's details as an aligned
// "Key: value" table. Shared by the session status, extend, and terminate
// commands, which all report the same fields.
func printSessionDetails(details *educatesrestapi.WorkshopSessionDetails) string {
	return utils.PrintKeyValuesTable([][]string{
		{"Started", details.Started},
		{"Expires", details.Expires},
		{"Expiring", fmt.Sprintf("%t", details.Expiring)},
		{"Countdown", fmt.Sprintf("%d", details.Countdown)},
		{"Extendable", fmt.Sprintf("%t", details.Extendable)},
		{"Status", details.Status},
	})
}

var clusterSessionStatusExample = `
  # Show the status of a workshop session:
  educates cluster session status my-session-name
`

type ClusterSessionStatusOptions struct {
	KubeconfigOptions
	Portal string
	Name   string
}

func (o *ClusterSessionStatusOptions) Run() error {
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

	details, err := catalogApiRequester.GetWorkshopSession(o.Name)
	if err != nil {
		return err
	}

	fmt.Println(printSessionDetails(details))

	return nil
}

func (p *ProjectInfo) NewClusterSessionStatusCmd() *cobra.Command {
	var o ClusterSessionStatusOptions

	var c = &cobra.Command{
		Args:    exactArgs(1, "session name is required", "NAME"),
		Use:     "status NAME",
		Short:   "Output status of session in Kubernetes",
		Example: clusterSessionStatusExample,
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
