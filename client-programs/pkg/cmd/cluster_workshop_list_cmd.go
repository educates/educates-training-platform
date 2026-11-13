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

var clusterWorkshopListExample = `
  # List the workshops deployed to the default portal:
  educates cluster workshop list

  # List the workshops on a named portal:
  educates cluster workshop list --portal my-portal
`

type ClusterWorkshopsListOptions struct {
	KubeconfigOptions
	Portal string
}

func (o *ClusterWorkshopsListOptions) Run() error {
	var err error

	// Ensure have portal name.

	if o.Portal == "" {
		o.Portal = "educates-cli"
	}

	clusterConfig, err := cluster.NewClusterConfigIfAvailable(o.Kubeconfig, o.Context)
	if err != nil {
		return err
	}

	dynamicClient, err := clusterConfig.GetDynamicClient()

	if err != nil {
		return errors.Wrapf(err, "unable to create Kubernetes client")
	}

	trainingPortalClient := dynamicClient.Resource(trainingPortalResource)

	trainingPortal, err := trainingPortalClient.Get(context.TODO(), o.Portal, metav1.GetOptions{})

	if k8serrors.IsNotFound(err) {
		fmt.Println("No workshops found.")
		return nil
	}

	sessionsMaximum, sessionsMaximumExists, _ := unstructured.NestedInt64(trainingPortal.Object, "spec", "portal", "sessions", "maximum")

	workshops, _, err := unstructured.NestedSlice(trainingPortal.Object, "spec", "workshops")

	if err != nil {
		return errors.Wrap(err, "unable to retrieve workshops from training portal")
	}

	if len(workshops) == 0 {
		fmt.Println("No workshops found.")
		return nil
	}

	workshopsClient := dynamicClient.Resource(workshopResource)

	rows := make([][]string, 0, len(workshops))

	for _, item := range workshops {
		object := item.(map[string]interface{})
		name := object["name"].(string)

		var capacityField string

		capacity, capacityExists := object["capacity"]

		if capacityExists {
			capacityField = fmt.Sprintf("%d", capacity)
		} else if sessionsMaximumExists {
			capacityField = fmt.Sprintf("%d", sessionsMaximum)
		}

		workshop, err := workshopsClient.Get(context.TODO(), name, metav1.GetOptions{})

		source := ""

		if err == nil {
			annotations := workshop.GetAnnotations()

			if val, ok := annotations["training.educates.dev/source"]; ok {
				source = val
			}
		}

		alias, _ := object["alias"].(string)

		rows = append(rows, []string{name, alias, capacityField, source})
	}

	fmt.Println(utils.PrintTable([]string{"NAME", "ALIAS", "CAPACITY", "SOURCE"}, rows))

	return nil
}

func (p *ProjectInfo) NewClusterWorkshopListCmd() *cobra.Command {
	var o ClusterWorkshopsListOptions

	var c = &cobra.Command{
		Args:    cobra.NoArgs,
		Use:     "list",
		Short:   "List workshops deployed to Kubernetes",
		Example: clusterWorkshopListExample,
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
	c.Flags().StringVarP(
		&o.Portal,
		"portal",
		"p",
		"educates-cli",
		"name to be used for training portal and workshop name prefixes",
	)

	return c
}
