package cmd

import (
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/educates/educates-training-platform/client-programs/pkg/cluster"
	"github.com/educates/educates-training-platform/client-programs/pkg/constants"
	"github.com/educates/educates-training-platform/client-programs/pkg/educates/resources/portal"
	"github.com/educates/educates-training-platform/client-programs/pkg/utils"
	"github.com/pkg/errors"
	"github.com/spf13/cobra"
)

type ClusterPortalOpenOptions struct {
	KubeconfigOptions
	Admin  bool
	Portal string
}

const clusterPortalOpenExample = `
# Open TrainingPortal in Educates cluster with default name
educates cluster portal open

# Open TrainingPortal in Educates cluster with specific name
educates cluster portal open --portal=my-portal

# Open admin interface of specific TrainingPortal
educates cluster portal open --portal=my-portal --admin

# Open given TrainingPortal in given Educates cluster
educates cluster portal open --portal=my-portal --kubeconfig ~/.kube/config --context=my-context
`

func (o *ClusterPortalOpenOptions) Run() error {
	var err error

	// Ensure have portal name.
	if o.Portal == "" {
		o.Portal = constants.DefaultPortalName
	}

	clusterConfig, err := cluster.NewClusterConfigIfAvailable(o.Kubeconfig, o.Context)

	if err != nil {
		return err
	}

	dynamicClient, err := clusterConfig.GetDynamicClient()

	if err != nil {
		return errors.Wrapf(err, "unable to create Kubernetes client")
	}

	config := portal.TrainingPortalOpenConfig{
		Portal: o.Portal,
		Admin: o.Admin,
	}

	manager := portal.NewPortalManager(dynamicClient)

	targetUrl, err := manager.GetTrainingPortalBrowserUrl(&config)

	if err != nil {
		return err
	}

	fmt.Printf("Training portal %q.\n", o.Portal)

	fmt.Print("Checking training portal is ready.\n")

	spinner := func(iteration int) string {
		spinners := `|/-\`
		return string(spinners[iteration%len(spinners)])
	}

	for i := 1; i < 300; i++ {
		fmt.Printf("\r[%s] Waiting...", spinner(i))

		time.Sleep(time.Second)

		resp, err := http.Get(targetUrl)

		if err != nil || resp.StatusCode == 503 {
			continue
		}

		defer resp.Body.Close()
		io.ReadAll(resp.Body)

		break
	}

	fmt.Print("\r              \r")

	fmt.Printf("Opening training portal %s.\n", targetUrl)

	return utils.OpenBrowser(targetUrl)
}

func (p *ProjectInfo) NewClusterPortalOpenCmd() *cobra.Command {
	var o ClusterPortalOpenOptions

	var c = &cobra.Command{
		Args:  cobra.NoArgs,
		Use:   "open",
		Short: "Browse portal in Kubernetes",
		RunE:  func(_ *cobra.Command, _ []string) error { return o.Run() },
		Example: clusterPortalOpenExample,
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
	c.Flags().BoolVar(
		&o.Admin,
		"admin",
		false,
		"open URL for admin login instead of workshops catalog",
	)
	c.Flags().StringVarP(
		&o.Portal,
		"portal",
		"p",
		constants.DefaultPortalName,
		"name to be used for training portal and workshop name prefixes",
	)

	return c
}
