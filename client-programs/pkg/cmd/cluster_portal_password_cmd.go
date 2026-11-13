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

var clusterPortalPasswordExample = `
  # Print the access code for the default portal:
  educates cluster portal password

  # Print the admin credentials for a named portal:
  educates cluster portal password --portal my-portal --admin
`

type ClusterPortalPasswordOptions struct {
	KubeconfigOptions
	Admin  bool
	Portal string
}

func (o *ClusterPortalPasswordOptions) Run() error {
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
		return errors.New("no workshops deployed")
	}

	if o.Admin {
		username, found, err := unstructured.NestedString(trainingPortal.Object, "status", "educates", "credentials", "admin", "username")

		if err != nil || !found {
			return errors.New("unable to access credentials")
		}

		password, found, err := unstructured.NestedString(trainingPortal.Object, "status", "educates", "credentials", "admin", "password")

		if err != nil || !found {
			return errors.New("unable to access credentials")
		}

		fmt.Println(utils.PrintTable([]string{"USERNAME", "PASSWORD"}, [][]string{{username, password}}))
	} else {
		password, _, _ := unstructured.NestedString(trainingPortal.Object, "spec", "portal", "password")

		fmt.Println(password)
	}

	return nil
}

func (p *ProjectInfo) NewClusterPortalPasswordCmd() *cobra.Command {
	var o ClusterPortalPasswordOptions

	var c = &cobra.Command{
		Args:    cobra.NoArgs,
		Use:     "password",
		Short:   "View portal credentials in Kubernetes",
		Example: clusterPortalPasswordExample,
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
	c.Flags().BoolVar(
		&o.Admin,
		"admin",
		false,
		"view admin password for admin pages rather than access code",
	)
	c.Flags().StringVarP(
		&o.Portal,
		"portal",
		"p",
		"educates-cli",
		"name to be used for training portal and workshop name prefixes",
	)

	return c
}
