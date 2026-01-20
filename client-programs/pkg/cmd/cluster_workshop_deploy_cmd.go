package cmd

import (
	"fmt"

	yttcmd "carvel.dev/ytt/pkg/cmd/template"
	"github.com/educates/educates-training-platform/client-programs/pkg/cluster"
	"github.com/educates/educates-training-platform/client-programs/pkg/constants"
	"github.com/educates/educates-training-platform/client-programs/pkg/educates/resources/workshops"
	"github.com/pkg/errors"
	"github.com/spf13/cobra"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

const (
	clusterWorkshopDeployExample = `
  # Deploy Educates workshop to cluster in current workshop directory and using default workshop file
  educates cluster workshop deploy

  # Deploy Educates workshop to cluster with custom workshop name and alias and custom workshop settings
  educates cluster workshop deploy --name my-workshop --alias my-workshop -initial 10 -reserved 5 -expires 1h -overtime 10m -deadline 2h -orphaned 10m -overdue 10s

  # Deploy Educates workshop to cluster with custom workshop file
  educates cluster workshop deploy --path ./workshop --workshop-file custom-workshop.yaml
`
)

type ClusterWorkshopDeployOptions struct {
	KubeconfigOptions
	Name            string
	Alias           string
	Path            string
	Portal          string
	Capacity        uint
	Reserved        uint
	Initial         uint
	Expires         string
	Overtime        string
	Deadline        string
	Orphaned        string
	Overdue         string
	Refresh         string
	Repository      string
	Environ         []string
	Labels          []string
	WorkshopFile    string
	WorkshopVersion string
	OpenBrowser     bool
	DataValuesFlags yttcmd.DataValuesFlags
}

func (o *ClusterWorkshopDeployOptions) Run() error {
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

	loadConfig := workshops.WorkshopDefinitionConfig{
		Name: o.Name,
		Path: path,
		Portal: o.Portal,
		WorkshopFile: o.WorkshopFile,
		WorkshopVersion: o.WorkshopVersion,
		DataValueFlags: o.DataValuesFlags,
	}

	if workshop, err = workshops.LoadWorkshopDefinition(&loadConfig); err != nil {
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

	manager := workshops.NewWorkshopManager(dynamicClient)

	// Update the workshop resource in the Kubernetes cluster.
	updateConfig := workshops.UpdateWorkshopResourceConfig{
		Workshop: workshop,
	}

	err = manager.UpdateWorkshopResource(&updateConfig)

	if err != nil {
		return err
	}

	fmt.Printf("Loaded workshop %q.\n", workshop.GetName())

	// Update the training portal, creating it if necessary.

	deployConfig := workshops.DeployWorkshopConfig{
		Workshop: workshop,
		Alias: o.Alias,
		Portal: o.Portal,
		Capacity: o.Capacity,
		Reserved: o.Reserved,
		Initial: o.Initial,
		Expires: o.Expires,
		Overtime: o.Overtime,
		Deadline: o.Deadline,
		Orphaned: o.Orphaned,
		Overdue: o.Overdue,
		Refresh: o.Refresh,
		Registry: o.Repository,
		Environ: o.Environ,
		Labels: o.Labels,
		OpenBrowser: o.OpenBrowser,
	}
	err = manager.DeployWorkshopResource(&deployConfig)

	// TODO: Move open browser logic to separate function and extract logic here
	// if o.OpenBrowser {
	// 	err = manager.OpenBrowser(&deployConfig)
	// 	if err != nil {
	// 		return err
	// 	}
	// }

	if err != nil {
		return err
	}

	return nil
}

func (p *ProjectInfo) NewClusterWorkshopDeployCmd() *cobra.Command {
	var o ClusterWorkshopDeployOptions

	var c = &cobra.Command{
		Args:  cobra.NoArgs,
		Use:   "deploy",
		Short: "Deploy workshop to Kubernetes",
		RunE:  func(_ *cobra.Command, _ []string) error { return o.Run() },
		Example: clusterWorkshopDeployExample,
	}

	c.Flags().StringVarP(
		&o.Name,
		"name",
		"n",
		"",
		"name to be used for the workshop definition, generated if not set",
	)
	c.Flags().StringVarP(
		&o.Alias,
		"alias",
		"a",
		"",
		"alias to be used to identify the workshop by the training portal",
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
	c.Flags().UintVar(
		&o.Capacity,
		"capacity",
		0,
		"maximum number of concurrent sessions for this workshop",
	)
	c.Flags().UintVar(
		&o.Reserved,
		"reserved",
		0,
		"number of workshop sessions to maintain ready in reserve",
	)
	c.Flags().UintVar(
		&o.Initial,
		"initial",
		0,
		"number of workshop sessions to create when first deployed",
	)
	c.Flags().StringVar(
		&o.Expires,
		"expires",
		"",
		"time duration before the workshop is expired",
	)
	c.Flags().StringVar(
		&o.Overtime,
		"overtime",
		"",
		"time extension allowed for the workshop",
	)
	c.Flags().StringVar(
		&o.Deadline,
		"deadline",
		"",
		"maximum time duration allowed for the workshop",
	)
	c.Flags().StringVar(
		&o.Orphaned,
		"orphaned",
		"5m",
		"allowed inactive time before workshop is terminated",
	)
	c.Flags().StringVar(
		&o.Overdue,
		"overdue",
		"2m",
		"allowed startup time before workshop is deemed failed",
	)
	c.Flags().StringVar(
		&o.Refresh,
		"refresh",
		"",
		"interval after which workshop environment is recreated",
	)
	c.Flags().StringSliceVarP(
		&o.Environ,
		"env",
		"e",
		[]string{},
		"environment variable overrides for workshop",
	)
	c.Flags().StringSliceVarP(
		&o.Labels,
		"labels",
		"l",
		[]string{},
		"label overrides for workshop",
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

	c.Flags().StringVar(
		&o.Repository,
		"image-repository",
		"",
		"the address of the image repository",
	)

	c.Flags().BoolVar(
		&o.OpenBrowser,
		"open-browser",
		false,
		"automatically launch browser on portal",
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
