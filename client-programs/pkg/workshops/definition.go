package workshops

import (
	"bytes"
	"context"
	"crypto/sha1"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"

	yttcmd "carvel.dev/ytt/pkg/cmd/template"
	yttcmdui "carvel.dev/ytt/pkg/cmd/ui"
	"carvel.dev/ytt/pkg/files"
	"carvel.dev/ytt/pkg/yamlmeta"
	"github.com/pkg/errors"
	"k8s.io/apimachinery/pkg/apis/meta/internalversion/scheme"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/dynamic"
)


func LoadWorkshopDefinition(name string, path string, portal string, workshopFile string, workshopVersion string, dataValueFlags yttcmd.DataValuesFlags) (*unstructured.Unstructured, error) {
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

	if workshopData, err = ProcessWorkshopDefinition(workshopData, dataValueFlags); err != nil {
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
		name = GenerateWorkshopName(path, workshop, portal)
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

func GenerateWorkshopName(path string, workshop *unstructured.Unstructured, portal string) string {
	name := workshop.GetName()

	h := sha1.New()

	io.WriteString(h, path)

	hv := fmt.Sprintf("%x", h.Sum(nil))

	name = fmt.Sprintf("%s--%s-%s", portal, name, hv[len(hv)-7:])

	return name
}

func GetWorkshopResource() schema.GroupVersionResource {
	return schema.GroupVersionResource{Group: "training.educates.dev", Version: "v1beta1", Resource: "workshops"}
}

func UpdateWorkshopResource(client dynamic.Interface, workshop *unstructured.Unstructured) error {
	workshopsClient := client.Resource(GetWorkshopResource())

	// _, err := workshopsClient.Apply(context.TODO(), workshop.GetName(), workshop, metav1.ApplyOptions{FieldManager: workshops.DefaultPortalName, Force: true})

	workshopBytes, err := runtime.Encode(unstructured.UnstructuredJSONScheme, workshop)

	if err != nil {
		return errors.Wrapf(err, "unable to update workshop definition in cluster %q", workshop.GetName())
	}

	_, err = workshopsClient.Patch(context.TODO(), workshop.GetName(), types.ApplyPatchType, workshopBytes, metav1.ApplyOptions{FieldManager: DefaultPortalName, Force: true}.ToPatchOptions())

	if err != nil {
		return errors.Wrapf(err, "unable to update workshop definition in cluster %q", workshop.GetName())
	}

	return nil
}

func ProcessWorkshopDefinition(yamlData []byte, dataValueFlags yttcmd.DataValuesFlags) ([]byte, error) {
	templatingOptions := yttcmd.NewOptions()

	templatingOptions.IgnoreUnknownComments = true

	templatingOptions.DataValuesFlags = dataValueFlags

	var filesToProcess []*files.File

	mainInputFile := files.MustNewFileFromSource(files.NewBytesSource("workshop.yaml", yamlData))

	filesToProcess = append(filesToProcess, mainInputFile)

	logUI := yttcmdui.NewCustomWriterTTY(false, log.Writer(), log.Writer())

	output := templatingOptions.RunWithFiles(yttcmd.Input{Files: filesToProcess}, logUI)

	if output.Err != nil {
		return []byte{}, fmt.Errorf("execution of ytt failed: %s", output.Err)
	}

	if len(output.DocSet.Items) == 0 {
		return []byte{}, nil
	}

	var buf bytes.Buffer

	yamlmeta.NewYAMLPrinter(&buf).Print(output.DocSet.Items[0])

	return buf.Bytes(), nil
}
