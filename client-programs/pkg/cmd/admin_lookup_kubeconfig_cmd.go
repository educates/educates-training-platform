package cmd

import (
	"context"
	"encoding/base64"
	"fmt"
	"io/ioutil"

	"github.com/educates/educates-training-platform/client-programs/pkg/cluster"
	"github.com/pkg/errors"
	"github.com/spf13/cobra"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var adminLookupKubeconfigExample = `
  # Print a kubeconfig for lookup-service remote access:
  educates admin lookup kubeconfig

  # Write the kubeconfig to a file:
  educates admin lookup kubeconfig --output kubeconfig.yaml
`

type LookupConfigOptions struct {
	KubeconfigOptions
	OutputPath string
}

func (o *LookupConfigOptions) Run() error {
	var err error

	clusterConfig, err := cluster.NewClusterConfigIfAvailable(o.Kubeconfig, o.Context)
	if err != nil {
		return err
	}

	client, err := clusterConfig.GetClient()

	if err != nil {
		return err
	}

	// We need to fetch the secret called "remote-access-token" from the
	// "educates" namespace. This contains a Kubernetes access token secret
	// giving access to just the Educates custom resources.

	secretsClient := client.CoreV1().Secrets("educates")

	secret, err := secretsClient.Get(context.TODO(), "remote-access-token", metav1.GetOptions{})

	if err != nil {
		return errors.Wrapf(err, "unable to fetch remote-access secret")
	}

	// Within the secret are data fields for "ca.crt" and "token". We need to
	// extract these and use them to create a kubeconfig file. Note that there
	// is no "server" property in the secret, so when constructing the kubeconfig
	// we need to use the server from the same cluster as we are requesting the
	// secret from.

	caCrt := secret.Data["ca.crt"]
	token := secret.Data["token"]

	// Get the server from the client for Kubernetes cluster access.

	serverScheme := client.CoreV1().RESTClient().Get().URL().Scheme
	serverHost := client.CoreV1().RESTClient().Get().URL().Host

	serverUrl := fmt.Sprintf("%s://%s", serverScheme, serverHost)

	// Construct the kubeconfig file. We need to base64 encode the ca.crt file
	// as it is a binary file.

	kubeconfig := fmt.Sprintf(`apiVersion: v1
kind: Config
clusters:
- name: training-platform
  cluster:
    server: %s
    certificate-authority-data: %s
contexts:
- name: training-platform
  context:
    cluster: training-platform
    user: remote-access
current-context: training-platform
users:
- name: remote-access
  user:
    token: %s
`, serverUrl, base64.StdEncoding.EncodeToString(caCrt), token)

	// Write out the kubeconfig to the output path if provided, otherwise
	// print it to stdout.

	if o.OutputPath != "" {
		err = ioutil.WriteFile(o.OutputPath, []byte(kubeconfig), 0644)

		if err != nil {
			return errors.Wrapf(err, "unable to write kubeconfig to %s", o.OutputPath)
		}
	} else {
		fmt.Print(kubeconfig)
	}

	return nil
}

func (p *ProjectInfo) NewAdminLookupKubeconfigCmd() *cobra.Command {
	var o LookupConfigOptions

	var c = &cobra.Command{
		Args:    cobra.NoArgs,
		Use:     "kubeconfig",
		Short:   "Fetch kubeconfig for lookup service remote access",
		Example: adminLookupKubeconfigExample,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return o.Run()
		},
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
		&o.OutputPath,
		"output",
		"o",
		"",
		"Path to write Kubeconfig file to",
	)

	return c
}
