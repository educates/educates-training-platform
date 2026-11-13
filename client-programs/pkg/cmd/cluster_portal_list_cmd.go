package cmd

import (
	"context"
	"fmt"

	"github.com/educates/educates-training-platform/client-programs/pkg/cluster"
	"github.com/educates/educates-training-platform/client-programs/pkg/utils"
	"github.com/pkg/errors"
	"github.com/spf13/cobra"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

var clusterPortalListExample = `
  # List the training portals deployed to the cluster:
  educates cluster portal list
`

type ClusterPortalListOptions struct {
	KubeconfigOptions
}

func (o *ClusterPortalListOptions) Run() error {
	var err error

	clusterConfig, err := cluster.NewClusterConfigIfAvailable(o.Kubeconfig, o.Context)

	if err != nil {
		return err
	}

	dynamicClient, err := clusterConfig.GetDynamicClient()

	if err != nil {
		return errors.Wrapf(err, "unable to create Kubernetes client")
	}

	trainingPortalClient := dynamicClient.Resource(trainingPortalResource)

	trainingPortals, err := trainingPortalClient.List(context.TODO(), metav1.ListOptions{})

	if k8serrors.IsNotFound(err) {
		fmt.Println("No portals found.")
		return nil
	}

	rows := make([][]string, 0, len(trainingPortals.Items))

	for _, item := range trainingPortals.Items {
		name := item.GetName()

		sessionsMaximum, propertyExists, err := unstructured.NestedInt64(item.Object, "spec", "portal", "sessions", "maximum")

		var capacity string

		if err == nil && propertyExists {
			capacity = fmt.Sprintf("%d", sessionsMaximum)
		}

		url, _, _ := unstructured.NestedString(item.Object, "status", "educates", "url")

		rows = append(rows, []string{name, capacity, url})
	}

	fmt.Println(utils.PrintTable([]string{"NAME", "CAPACITY", "URL"}, rows))

	return nil
}

func (p *ProjectInfo) NewClusterPortalListCmd() *cobra.Command {
	var o ClusterPortalListOptions

	var c = &cobra.Command{
		Args:    cobra.NoArgs,
		Use:     "list",
		Short:   "List portals deployed to Kubernetes",
		Example: clusterPortalListExample,
		RunE:    func(_ *cobra.Command, _ []string) error { return o.Run() },
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

	return c
}
