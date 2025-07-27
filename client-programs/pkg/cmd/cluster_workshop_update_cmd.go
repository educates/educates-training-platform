package cmd

import (
	"context"
	"crypto/sha1"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"

	yttcmd "carvel.dev/ytt/pkg/cmd/template"
	"github.com/educates/educates-training-platform/client-programs/pkg/cluster"
	"github.com/educates/educates-training-platform/client-programs/pkg/workshops"
	"github.com/pkg/errors"
	"github.com/spf13/cobra"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/dynamic"
	"k8s.io/kubectl/pkg/scheme"
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
		o.Portal = "educates-cli"
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

	if workshop, err = loadWorkshopDefinition(o.Name, path, o.Portal, o.WorkshopFile, o.WorkshopVersion, o.DataValuesFlags); err != nil {
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

	// Update the workshop resource in the Kubernetes cluster.

	err = updateWorkshopResource(dynamicClient, workshop)

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
		"educates-cli",
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

func loadWorkshopDefinition(name string, path string, portal string, workshopFile string, workshopVersion string, dataValueFlags yttcmd.DataValuesFlags) (*unstructured.Unstructured, error) {
	// Parse the workshop location so we can determine if it is a local file
	// or accessible using a HTTP/HTTPS URL.

	var urlInfo *url.URL
	var err error

	if urlInfo, err = url.Parse(path); err != nil {
		return nil, errors.Wrap(err, "unable to parse workshop location")
	}

	// Check if file system path first (not HTTP/HTTPS) and if so normalize
	// the path. If it the path references a directory, then extend the path
	// so we look for the workshop file within that directory.

	if urlInfo.Scheme != "http" && urlInfo.Scheme != "https" {
		path = filepath.Clean(path)

		if path, err = filepath.Abs(path); err != nil {
			return nil, errors.Wrap(err, "couldn't convert workshop location to absolute path")
		}

		if !filepath.IsAbs(workshopFile) {
			fileInfo, err := os.Stat(path)

			if err != nil {
				return nil, errors.Wrap(err, "couldn't test if workshop location is a directory")
			}

			if fileInfo.IsDir() {
				path = filepath.Join(path, workshopFile)
			}
		} else {
			path = workshopFile
		}
	}

	// Read in the workshop definition as raw data ready for parsing.

	var workshopData []byte

	if urlInfo.Scheme != "http" && urlInfo.Scheme != "https" {
		if workshopData, err = os.ReadFile(path); err != nil {
			return nil, errors.Wrap(err, "couldn't read workshop definition data file")
		}
	} else {
		var client http.Client

		resp, err := client.Get(path)

		if err != nil {
			return nil, errors.Wrap(err, "couldn't download workshop definition from host")
		}

		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			return nil, errors.New("failed to download workshop definition from host")
		}

		workshopData, err = io.ReadAll(resp.Body)

		if err != nil {
			return nil, errors.Wrap(err, "failed to read workshop definition from host")
		}
	}

	// Process the workshop YAML data in case it contains ytt templating.

	if workshopData, err = workshops.ProcessWorkshopDefinition(workshopData, dataValueFlags); err != nil {
		return nil, errors.Wrap(err, "unable to process workshop definition as template")
	}

	// Parse the workshop definition.

	decoder := serializer.NewCodecFactory(scheme.Scheme).UniversalDecoder()

	workshop := &unstructured.Unstructured{}

	err = runtime.DecodeInto(decoder, workshopData, workshop)

	if err != nil {
		return nil, errors.Wrap(err, "couldn't parse workshop definition")
	}

	// Verify the type of resource definition.

	if workshop.GetAPIVersion() != "training.educates.dev/v1beta1" || workshop.GetKind() != "Workshop" {
		return nil, errors.New("invalid type for workshop definition")
	}

	// Add annotations recording details about original workshop location.

	annotations := workshop.GetAnnotations()

	if annotations == nil {
		annotations = map[string]string{}
	}

	annotations["training.educates.dev/workshop"] = workshop.GetName()

	if urlInfo.Scheme != "http" && urlInfo.Scheme != "https" {
		annotations["training.educates.dev/source"] = fmt.Sprintf("file://%s", path)
	} else {
		annotations["training.educates.dev/source"] = path
	}

	workshop.SetAnnotations(annotations)

	// Update the name for the workshop such that it incorporates a hash of
	// the workshop location.

	if name == "" {
		name = generateWorkshopName(path, workshop, portal)
	}

	workshop.SetName(name)

	// Insert workshop version property if not specified.

	_, found, _ := unstructured.NestedString(workshop.Object, "spec", "version")

	if !found && workshopVersion != "latest" {
		unstructured.SetNestedField(workshop.Object, workshopVersion, "spec", "version")
	}

	// Remove the publish section as will not be accurate after publising.

	unstructured.RemoveNestedField(workshop.Object, "spec", "publish")

	return workshop, nil
}

func generateWorkshopName(path string, workshop *unstructured.Unstructured, portal string) string {
	name := workshop.GetName()

	h := sha1.New()

	io.WriteString(h, path)

	hv := fmt.Sprintf("%x", h.Sum(nil))

	name = fmt.Sprintf("%s--%s-%s", portal, name, hv[len(hv)-7:])

	return name
}

var workshopResource = schema.GroupVersionResource{Group: "training.educates.dev", Version: "v1beta1", Resource: "workshops"}

func updateWorkshopResource(client dynamic.Interface, workshop *unstructured.Unstructured) error {
	workshopsClient := client.Resource(workshopResource)

	// _, err := workshopsClient.Apply(context.TODO(), workshop.GetName(), workshop, metav1.ApplyOptions{FieldManager: "educates-cli", Force: true})

	workshopBytes, err := runtime.Encode(unstructured.UnstructuredJSONScheme, workshop)

	if err != nil {
		return errors.Wrapf(err, "unable to update workshop definition in cluster %q", workshop.GetName())
	}

	_, err = workshopsClient.Patch(context.TODO(), workshop.GetName(), types.ApplyPatchType, workshopBytes, metav1.ApplyOptions{FieldManager: "educates-cli", Force: true}.ToPatchOptions())

	if err != nil {
		return errors.Wrapf(err, "unable to update workshop definition in cluster %q", workshop.GetName())
	}

	return nil
}
