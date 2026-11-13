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
	"k8s.io/apimachinery/pkg/runtime/schema"
)

var clusterSessionListExample = `
  # List active sessions on the default portal:
  educates cluster session list

  # List sessions for a specific workshop environment:
  educates cluster session list --environment my-workshop-w01
`

type ClusterSessionListOptions struct {
	KubeconfigOptions
	Portal      string
	Environment string
}

var workshopSessionResource = schema.GroupVersionResource{Group: "training.educates.dev", Version: "v1beta1", Resource: "workshopsessions"}

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

	workshopSessionClient := dynamicClient.Resource(workshopSessionResource)

	workshopSessions, err := workshopSessionClient.List(context.TODO(), metav1.ListOptions{})

	if k8serrors.IsNotFound(err) {
		fmt.Println("No sessions found.")
		return nil
	}

	var sessions []unstructured.Unstructured

	for _, item := range workshopSessions.Items {
		labels := item.GetLabels()

		portal, ok := labels["training.educates.dev/portal.name"]

		if ok && portal == o.Portal {
			if o.Environment != "" {
				environment, ok := labels["training.educates.dev/environment.name"]

				if ok && environment == o.Environment {
					sessions = append(sessions, item)
				}
			} else {
				sessions = append(sessions, item)
			}

		}
	}

	if len(sessions) == 0 {
		fmt.Println("No sessions found.")
		return nil
	}

	rows := make([][]string, 0, len(sessions))

	for _, item := range sessions {
		name := item.GetName()
		labels := item.GetLabels()

		portal := labels["training.educates.dev/portal.name"]
		environment := labels["training.educates.dev/environment.name"]

		status, _, _ := unstructured.NestedString(item.Object, "status", "educates", "phase")

		rows = append(rows, []string{name, portal, environment, status})
	}

	fmt.Println(utils.PrintTable([]string{"NAME", "PORTAL", "ENVIRONMENT", "STATUS"}, rows))

	return nil
}

func (p *ProjectInfo) NewClusterSessionListCmd() *cobra.Command {
	var o ClusterSessionListOptions

	var c = &cobra.Command{
		Args:    cobra.NoArgs,
		Use:     "list",
		Short:   "List active sessions in Kubernetes",
		Example: clusterSessionListExample,
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
