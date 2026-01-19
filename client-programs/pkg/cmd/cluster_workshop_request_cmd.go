package cmd

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"

	yttcmd "carvel.dev/ytt/pkg/cmd/template"
	"github.com/educates/educates-training-platform/client-programs/pkg/cluster"
	"github.com/educates/educates-training-platform/client-programs/pkg/constants"
	"github.com/educates/educates-training-platform/client-programs/pkg/educatesrestapi"
	"github.com/joho/godotenv"
	"github.com/pkg/errors"
	"github.com/spf13/cobra"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

type ClusterWorkshopRequestOptions struct {
	KubeconfigOptions
	Name              string
	Path              string
	Portal            string
	Params            []string
	ParamFiles        []string
	ParamsFiles       []string
	IndexUrl          string
	UserIdentity      string
	EnvironmentName   string
	ActivationTimeout int
	NoBrowser         bool
	WorkshopFile      string
	WorkshopVersion   string
	DataValuesFlags   yttcmd.DataValuesFlags
}

func (o *ClusterWorkshopRequestOptions) Run() error {
	var err error

	var name = o.Name

	// Process parameters.

	params := map[string]string{}

	for _, item := range o.Params {
		parts := strings.SplitN(item, "=", 2)

		if len(parts) != 2 {
			return errors.Errorf("invalid parameter format %s", item)
		}

		params[parts[0]] = parts[1]
	}

	for _, item := range o.ParamFiles {
		parts := strings.SplitN(item, "=", 2)

		if len(parts) != 2 {
			return errors.Errorf("invalid parameter format %s", item)
		}

		content, err := os.ReadFile(parts[1])

		if err != nil {
			return errors.Wrapf(err, "cannot read parameter data file %s", parts[1])
		}

		params[parts[0]] = string(content)
	}

	for _, item := range o.ParamsFiles {
		var paramsData map[string]string

		paramsData, err := godotenv.Read(item)

		if err != nil {
			return errors.Wrapf(err, "cannot read parameters data file %s", item)
		}

		for name, value := range paramsData {
			params[name] = value
		}
	}

	// Ensure have portal name.

	if o.Portal == "" {
		o.Portal = constants.DefaultPortalName
	}

	if name == "" {
		var path = o.Path

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

		if workshop, err = loadWorkshopDefinition(o.Name, path, o.Portal, o.WorkshopFile, o.WorkshopVersion, o.DataValuesFlags); err != nil {
			return err
		}

		name = workshop.GetName()
	}

	clusterConfig, err := cluster.NewClusterConfigIfAvailable(o.Kubeconfig, o.Context)

	if err != nil {
		return err
	}

	// check that the portal has the workshop we want to request
	err = ensurePortalHasWorkshop(clusterConfig, name, o.Portal)
	if err != nil {
		return err
	}

	// Request the workshop from the training portal.
	err = requestWorkshop(clusterConfig, name, o.EnvironmentName, o.Portal, params, o.IndexUrl, o.UserIdentity, o.ActivationTimeout, o.NoBrowser)

	if err != nil {
		return err
	}

	return nil
}

func (p *ProjectInfo) NewClusterWorkshopRequestCmd() *cobra.Command {
	var o ClusterWorkshopRequestOptions

	var c = &cobra.Command{
		Args:  cobra.NoArgs,
		Use:   "request",
		Short: "Request workshop in Kubernetes",
		RunE:  func(_ *cobra.Command, _ []string) error { return o.Run() },
	}

	c.Flags().StringVarP(
		&o.Name,
		"name",
		"n",
		"",
		"name of the workshop being requested, overrides derived workshop name",
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
	c.Flags().StringArrayVarP(
		&o.Params,
		"param",
		"",
		[]string{},
		"set request parameter data value, as string, (format name=value)",
	)
	c.Flags().StringArrayVarP(
		&o.ParamFiles,
		"param-file",
		"",
		[]string{},
		"set request parameter data value, from file, (format name=path)",
	)
	c.Flags().StringArrayVarP(
		&o.ParamsFiles,
		"params-file",
		"",
		[]string{},
		"set request parameter data values from dotenv file",
	)
	c.Flags().StringVar(
		&o.IndexUrl,
		"index-url",
		"",
		"the URL to redirect to when workshop session is complete",
	)

	c.Flags().StringVar(
		&o.UserIdentity,
		"user",
		"",
		"the training portal user identifier",
	)

	c.Flags().IntVar(
		&o.ActivationTimeout,
		"timeout",
		60,
		"maximum time in seconds to activate the workshop",
	)

	c.Flags().StringVar(
		&o.EnvironmentName,
		"environment-name",
		"",
		"workshop environment name, overrides derived environment name",
	)

	c.Flags().BoolVar(
		&o.NoBrowser,
		"no-browser",
		false,
		"flag indicate whether to open browser automatically",
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

func ensurePortalHasWorkshop(clusterConfig *cluster.ClusterConfig, name string, portal string) error {
	client, err := clusterConfig.GetDynamicClient()

	if err != nil {
		return errors.Wrapf(err, "unable to create Kubernetes client")
	}

	trainingPortalClient := client.Resource(trainingPortalResource)

	trainingPortal, err := trainingPortalClient.Get(context.TODO(), portal, metav1.GetOptions{})

	if k8serrors.IsNotFound(err) {
		return errors.Wrap(err, "unable to retrieve training portal")
	}

	workshops, _, err := unstructured.NestedSlice(trainingPortal.Object, "spec", "workshops")

	if err != nil {
		return errors.Wrap(err, "unable to retrieve workshops from training portal")
	}

	var foundWorkshop = false

	for _, item := range workshops {
		object := item.(map[string]interface{})

		if object["name"] == name {
			foundWorkshop = true
		}
	}

	if !foundWorkshop {
		return errors.Wrapf(err, "unable to find workshop %s", name)
	}
	return nil
}

func requestWorkshop(clusterConfig *cluster.ClusterConfig, workshopName string, environmentName string, portalName string, params map[string]string, indexUrl string, user string, timeout int, noBrowser bool) error {
	catalogApiRequester := educatesrestapi.NewWorkshopsCatalogRequester(
		clusterConfig,
		portalName,
	)
	logout, err := catalogApiRequester.Login()
	if err != nil {
		return err
	}
	defer logout()

	// Get the list of workshops so we can know which workshop environment
	listEnvironmentsResult, err := catalogApiRequester.GetWorkshopsCatalog()
	if err != nil {
		return errors.Wrap(err, "failed to get workshops catalog")
	}

	// Work out the name of the workshop environment.
	if environmentName == "" {
		for _, item := range listEnvironmentsResult.Environments {
			if item.Workshop.Name == workshopName && item.State == "RUNNING" {
				environmentName = item.Name
			}
		}
	}

	if environmentName == "" {
		return errors.Errorf("cannot find workshop environment for workshop %s", workshopName)
	}

	// Now request the workshop from the required workshop environment.
	requestWorkshopResult, err := catalogApiRequester.RequestWorkshop(workshopName, environmentName, params, indexUrl, user, timeout)
	if err != nil {
		return err
	}

	fmt.Printf("Assigned training portal user %q.\n", requestWorkshopResult.User)
	fmt.Printf("Workshop session name is %q.\n", requestWorkshopResult.Name)

	workshopUrl := fmt.Sprintf("%s%s", catalogApiRequester.PortalUrl, requestWorkshopResult.URL)

	if noBrowser {
		fmt.Printf("Workshop activation URL is %s.\n", workshopUrl)

		return nil
	}

	fmt.Printf("Opening workshop URL %s.\n", workshopUrl)

	switch runtime.GOOS {
	case "linux":
		err = exec.Command("xdg-open", workshopUrl).Start()
	case "windows":
		err = exec.Command("rundll32", "url.dll,FileProtocolHandler", workshopUrl).Start()
	case "darwin":
		err = exec.Command("open", workshopUrl).Start()
	default:
		err = fmt.Errorf("unsupported platform")
	}

	if err != nil {
		return errors.Wrap(err, "unable to open web browser on workshop")
	}

	return nil
}
