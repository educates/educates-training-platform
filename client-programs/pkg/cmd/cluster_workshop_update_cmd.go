package cmd

import (
	"fmt"

	yttcmd "carvel.dev/ytt/pkg/cmd/template"
	"github.com/educates/educates-training-platform/client-programs/pkg/cluster"
	"github.com/educates/educates-training-platform/client-programs/pkg/constants"
	"github.com/educates/educates-training-platform/client-programs/pkg/educates"
	educatesResources "github.com/educates/educates-training-platform/client-programs/pkg/educates/resources"
	"github.com/pkg/errors"
	"github.com/spf13/cobra"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

const (
	clusterWorkshopUpdateExample = `
  # Update Educates workshop in Kubernetes cluster in current workshop directory and using default workshop file
  educates cluster workshop update

  # Update Educates workshop in Kubernetes cluster with custom workshop file
  educates cluster workshop update --path ./workshop --workshop-file custom-workshop.yaml
`
)

type ClusterWorkshopUpdateOptions struct {
	KubeconfigOptions
	Name            string
	Path            string
	Portal          string
	WorkshopFile    string
	WorkshopVersion string
	DataValuesFlags yttcmd.DataValuesFlags
}

func (o *ClusterWorkshopUpdateOptions) Run() error {
	var err error

	var path = o.Path

	// Ensure have portal name.

	if o.Portal == "" {
		o.Portal = constants.DefaultPortalName
	}

	// If path not provided assume the current working directory. When loading
	// the workshop will then expect the workshop definition to reside in the
	// resources/workshop.yaml file under the directory, the same as if a
	// directory path was provided explicitly.

	if path == "" {
		path = "."
	}

	// Load the workshop definition. The path can be a HTTP/HTTPS URL for a
	// local file system path for a directory or file.

	var workshop *unstructured.Unstructured

	definitionConfig := educates.WorkshopDefinitionConfig{
		Name: o.Name,
		Path: o.Path,
		Portal: o.Portal,
		WorkshopFile: o.WorkshopFile,
		WorkshopVersion: o.WorkshopVersion,
		DataValueFlags: o.DataValuesFlags,
	}

	if workshop, err = educates.LoadWorkshopDefinition(&definitionConfig); err != nil {
		return err
	}

	clusterConfig, err := cluster.NewClusterConfigIfAvailable(o.Kubeconfig, o.Context)

	if err != nil {
		return err
	}

	dynamicClient, err := clusterConfig.GetDynamicClient()

	if err != nil {
		return errors.Wrapf(err, "unable to create Kubernetes client")
	}

	manager := educatesResources.NewWorkshopManager(dynamicClient)

	// Update the workshop resource in the Kubernetes cluster.
	updateConfig := educatesResources.UpdateWorkshopResourceConfig{
		Workshop: workshop,
	}

	err = manager.UpdateWorkshopResource(&updateConfig)

	if err != nil {
		return err
	}

	fmt.Printf("Loaded workshop %q.\n", workshop.GetName())

	return nil
}

func (p *ProjectInfo) NewClusterWorkshopUpdateCmd() *cobra.Command {
	var o ClusterWorkshopUpdateOptions

	var c = &cobra.Command{
		Args:  cobra.NoArgs,
		Use:   "update",
		Short: "Update workshop in Kubernetes",
		RunE:  func(_ *cobra.Command, _ []string) error { return o.Run() },
		Example: clusterWorkshopUpdateExample,
	}

	c.Flags().StringVarP(
		&o.Name,
		"name",
		"n",
		"",
		"name to be used for the workshop definition, generated if not set",
	)
	c.Flags().StringVarP(
		&o.Path,
		"file",
		"f",
		".",
		"path to local workshop directory, definition file, or URL for workshop definition file",
	)
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
	c.Flags().StringVarP(
		&o.Portal,
		"portal",
		"p",
		constants.DefaultPortalName,
		"name to be used for training portal and workshop name prefixes",
	)

	c.Flags().StringVar(
		&o.WorkshopFile,
		"workshop-file",
		"resources/workshop.yaml",
		"location of the workshop definition file",
	)

	c.Flags().StringVar(
		&o.WorkshopVersion,
		"workshop-version",
		"latest",
		"version of the workshop being published",
	)

	c.Flags().StringArrayVar(
		&o.DataValuesFlags.EnvFromStrings,
		"data-values-env",
		nil,
		"Extract data values (as strings) from prefixed env vars (format: PREFIX for PREFIX_all__key1=str) (can be specified multiple times)",
	)
	c.Flags().StringArrayVar(
		&o.DataValuesFlags.EnvFromYAML,
		"data-values-env-yaml",
		nil,
		"Extract data values (parsed as YAML) from prefixed env vars (format: PREFIX for PREFIX_all__key1=true) (can be specified multiple times)",
	)

	c.Flags().StringArrayVar(
		&o.DataValuesFlags.KVsFromStrings,
		"data-value",
		nil,
		"Set specific data value to given value, as string (format: all.key1.subkey=123) (can be specified multiple times)",
	)
	c.Flags().StringArrayVar(
		&o.DataValuesFlags.KVsFromYAML,
		"data-value-yaml",
		nil,
		"Set specific data value to given value, parsed as YAML (format: all.key1.subkey=true) (can be specified multiple times)",
	)
	c.Flags().StringArrayVar(
		&o.DataValuesFlags.KVsFromFiles,
		"data-value-file",
		nil,
		"Set specific data value to contents of a file (format: [@lib1:]all.key1.subkey={file path, HTTP URL, or '-' (i.e. stdin)}) (can be specified multiple times)",
	)
	c.Flags().StringArrayVar(
		&o.DataValuesFlags.FromFiles,
		"data-values-file",
		nil,
		"Set multiple data values via plain YAML files (format: [@lib1:]{file path, HTTP URL, or '-' (i.e. stdin)}) (can be specified multiple times)",
	)

	return c
}
