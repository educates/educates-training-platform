package cmd

import (
	"context"

	"github.com/educates/educates-training-platform/client-programs/pkg/cluster"
	"github.com/pkg/errors"
	"github.com/spf13/cobra"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var clusterPortalDeleteExample = `
  # Delete the default training portal:
  educates cluster portal delete

  # Delete a named portal:
  educates cluster portal delete --portal my-portal
`

type ClusterPortalDeleteOptions struct {
	KubeconfigOptions
	Portal string
}

func (o *ClusterPortalDeleteOptions) Run() error {
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

	_, err = trainingPortalClient.Get(context.TODO(), o.Portal, metav1.GetOptions{})

	if k8serrors.IsNotFound(err) {
		return errors.New("no portal found")
	}

	err = trainingPortalClient.Delete(context.TODO(), o.Portal, metav1.DeleteOptions{})

	if err != nil {
		return errors.Wrap(err, "unable to delete portal")
	}

	return nil
}

func (p *ProjectInfo) NewClusterPortalDeleteCmd() *cobra.Command {
	var o ClusterPortalDeleteOptions

	var c = &cobra.Command{
		Args:    cobra.NoArgs,
		Use:     "delete",
		Short:   "Delete portal from Kubernetes",
		Example: clusterPortalDeleteExample,
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
