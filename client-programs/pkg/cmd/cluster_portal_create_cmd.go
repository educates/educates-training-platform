package cmd

import (
	"context"
	"strings"

	"github.com/educates/educates-training-platform/client-programs/pkg/cluster"
	"github.com/pkg/errors"
	"github.com/spf13/cobra"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/dynamic"
)

var clusterPortalCreateExample = `
  # Create a training portal with default settings:
  educates cluster portal create

  # Create a named portal with a session capacity:
  educates cluster portal create --portal my-portal --capacity 10
`

type ClusterConfigViewOptions struct {
	KubeconfigOptions
	Portal       string
	Hostname     string
	Repository   string
	Capacity     uint
	Password     string
	ThemeName    string
	CookieDomain string
	Labels       []string
}

func (o *ClusterConfigViewOptions) Run(isPasswordSet bool) error {
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

	// Update the training portal, creating it if necessary.

	err = createTrainingPortal(dynamicClient, o.Portal, o.Hostname, o.Repository, o.Capacity, o.Password, isPasswordSet, o.ThemeName, o.CookieDomain, o.Labels)

	if err != nil {
		return err
	}

	return nil
}

func (p *ProjectInfo) NewClusterPortalCreateCmd() *cobra.Command {
	var o ClusterConfigViewOptions

	var c = &cobra.Command{
		Args:    cobra.NoArgs,
		Use:     "create",
		Short:   "Create portal in Kubernetes",
		Example: clusterPortalCreateExample,
		RunE: func(cmd *cobra.Command, _ []string) error {
			isPasswordSet := cmd.Flags().Lookup("password").Changed

			return o.Run(isPasswordSet)
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
		&o.Portal,
		"portal",
		"p",
		"educates-cli",
		"name to be used for training portal and workshop name prefixes",
	)
	c.Flags().StringVar(
		&o.Hostname,
		"hostname",
		"",
		"override hostname for training portal and workshops",
	)
	c.Flags().StringVar(
		&o.Repository,
		"image-repository",
		"",
		"the address of the default image repository",
	)
	c.Flags().UintVar(
		&o.Capacity,
		"capacity",
		5,
		"maximum number of current sessions for the training portal",
	)
	c.Flags().StringVar(
		&o.Password,
		"password",
		"",
		"override password for training portal access",
	)
	c.Flags().StringVar(
		&o.ThemeName,
		"theme-name",
		"",
		"override theme used by training portal and workshops",
	)
	c.Flags().StringVar(
		&o.CookieDomain,
		"cookie-domain",
		"",
		"override cookie domain used by training portal and workshops",
	)
	c.Flags().StringSliceVarP(
		&o.Labels,
		"labels",
		"l",
		[]string{},
		"label overrides for portal",
	)

	return c
}

func createTrainingPortal(client dynamic.Interface, portal string, hostname string, registry string, capacity uint, password string, isPasswordSet bool, themeName string, cookieDomain string, labels []string) error {
	trainingPortalClient := client.Resource(trainingPortalResource)

	_, err := trainingPortalClient.Get(context.TODO(), portal, metav1.GetOptions{})

	if err != nil {
		if !k8serrors.IsNotFound(err) {
			return errors.Wrap(err, "unable to query training portal")
		}
	} else {
		return errors.New("training portal already exists")
	}

	trainingPortal := &unstructured.Unstructured{}

	if !isPasswordSet {
		password = randomPassword(12)
	}

	type LabelDetails struct {
		Name  string `json:"name"`
		Value string `json:"value"`
	}

	var labelOverrides []LabelDetails

	for _, value := range labels {
		parts := strings.SplitN(value, "=", 2)
		labelOverrides = append(labelOverrides, LabelDetails{
			Name:  parts[0],
			Value: parts[1],
		})
	}

	type RegistryDetails struct {
		Host      string `json:"host"`
		Namespace string `json:"namespace"`
	}

	registryHost := ""
	registryNamespace := ""

	if registry != "" {
		parts := strings.SplitN(registry, "/", 2)

		registryHost = parts[0]

		if len(parts) > 1 {
			registryNamespace = parts[1]
		}

	}

	trainingPortal.SetUnstructuredContent(map[string]interface{}{
		"apiVersion": "training.educates.dev/v1beta1",
		"kind":       "TrainingPortal",
		"metadata": map[string]interface{}{
			"name": portal,
		},
		"spec": map[string]interface{}{
			"portal": map[string]interface{}{
				"password": password,
				"registration": struct {
					Type string `json:"type"`
				}{
					Type: "anonymous",
				},
				"updates": struct {
					Workshop bool `json:"workshop"`
				}{
					Workshop: true,
				},
				"sessions": struct {
					Maximum int64 `json:"maximum"`
				}{
					Maximum: int64(capacity),
				},
				"workshop": map[string]interface{}{
					"defaults": struct {
						Reserved int             `json:"reserved"`
						Registry RegistryDetails `json:"registry"`
					}{
						Reserved: 0,
						Registry: RegistryDetails{
							Host:      registryHost,
							Namespace: registryNamespace,
						},
					},
				},
				"ingress": struct {
					Hostname string `json:"hostname"`
				}{
					Hostname: hostname,
				},
				"theme": struct {
					Name string `json:"name"`
				}{
					Name: themeName,
				},
				"cookies": struct {
					Domain string `json:"domain"`
				}{
					Domain: cookieDomain,
				},
				"labels": labelOverrides,
			},
			"workshops": []interface{}{},
		},
	})

	_, err = trainingPortalClient.Create(context.TODO(), trainingPortal, metav1.CreateOptions{FieldManager: "educates-cli"})

	if err != nil {
		return errors.Wrapf(err, "unable to create training portal %q in cluster", portal)
	}

	return nil
}
