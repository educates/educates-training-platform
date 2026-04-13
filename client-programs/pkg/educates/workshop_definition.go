package educates

import (
	"bytes"
	"crypto/sha1"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"

	yttcmd "carvel.dev/ytt/pkg/cmd/template"
	"carvel.dev/ytt/pkg/files"
	"carvel.dev/ytt/pkg/yamlmeta"
	"github.com/educates/educates-training-platform/client-programs/pkg/constants"
	"github.com/educates/educates-training-platform/client-programs/pkg/logger"
	"github.com/pkg/errors"
	"k8s.io/apimachinery/pkg/apis/meta/internalversion/scheme"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
)

type WorkshopDefinitionConfig struct {
	Name string
	Path string
	Portal string
	WorkshopFile string
	WorkshopVersion string
	DataValueFlags yttcmd.DataValuesFlags
}



func LoadWorkshopDefinition(o *WorkshopDefinitionConfig) (*unstructured.Unstructured, error) {
	// Parse the workshop location so we can determine if it is a local file
	// or accessible using a HTTP/HTTPS URL.

	var urlInfo *url.URL
	var err error

	if urlInfo, err = url.Parse(o.Path); err != nil {
		return nil, errors.Wrap(err, "unable to parse workshop location")
	}

	// Check if file system path first (not HTTP/HTTPS) and if so normalize
	// the path. If it the path references a directory, then extend the path
	// so we look for the workshop file within that directory.

	if urlInfo.Scheme != "http" && urlInfo.Scheme != "https" {
		o.Path = filepath.Clean(o.Path)

		if o.Path, err = filepath.Abs(o.Path); err != nil {
			return nil, errors.Wrap(err, "couldn't convert workshop location to absolute path")
		}

		if !filepath.IsAbs(o.WorkshopFile) {
			fileInfo, err := os.Stat(o.Path)

			if err != nil {
				return nil, errors.Wrap(err, "couldn't test if workshop location is a directory")
			}

			if fileInfo.IsDir() {
				o.Path = filepath.Join(o.Path, o.WorkshopFile)
			}
		} else {
			o.Path = o.WorkshopFile
		}
	}

	// Read in the workshop definition as raw data ready for parsing.
	var workshopData []byte

	if urlInfo.Scheme != "http" && urlInfo.Scheme != "https" {
		if workshopData, err = os.ReadFile(o.Path); err != nil {
			return nil, errors.Wrap(err, "couldn't read workshop definition data file")
		}
	} else {
		var client http.Client

		resp, err := client.Get(o.Path)

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

	if workshopData, err = ProcessWorkshopDefinition(workshopData, o.DataValueFlags); err != nil {
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

	if workshop.GetAPIVersion() != constants.EducatesTrainingAPIGroupVersion || workshop.GetKind() != "Workshop" {
		return nil, errors.New("invalid type for workshop definition")
	}

	// Add annotations recording details about original workshop location.

	annotations := workshop.GetAnnotations()

	if annotations == nil {
		annotations = map[string]string{}
	}

	annotations[constants.EducatesWorkshopLabelAnnotationWorkshop] = workshop.GetName()

	if urlInfo.Scheme != "http" && urlInfo.Scheme != "https" {
		annotations[constants.EducatesWorkshopLabelAnnotationSource] = fmt.Sprintf("file://%s", o.Path)
	} else {
		annotations[constants.EducatesWorkshopLabelAnnotationSource] = o.Path
	}

	workshop.SetAnnotations(annotations)

	// Update the name for the workshop such that it incorporates a hash of
	// the workshop location.

	if o.Name == "" {
		o.Name = generateWorkshopName(o.Path, workshop, o.Portal)
	}

	workshop.SetName(o.Name)

	// Insert workshop version property if not specified.

	_, found, _ := unstructured.NestedString(workshop.Object, "spec", "version")

	if !found && o.WorkshopVersion != "latest" {
		unstructured.SetNestedField(workshop.Object, o.WorkshopVersion, "spec", "version")
	}

	// Remove the publish section as will not be accurate after publising.

	unstructured.RemoveNestedField(workshop.Object, "spec", "publish")

	return workshop, nil
}

func ProcessWorkshopDefinition(yamlData []byte, dataValueFlags yttcmd.DataValuesFlags) ([]byte, error) {
	templatingOptions := yttcmd.NewOptions()

	templatingOptions.IgnoreUnknownComments = true

	templatingOptions.DataValuesFlags = dataValueFlags

	var filesToProcess []*files.File

	mainInputFile := files.MustNewFileFromSource(files.NewBytesSource("workshop.yaml", yamlData))

	filesToProcess = append(filesToProcess, mainInputFile)

	output := templatingOptions.RunWithFiles(yttcmd.Input{Files: filesToProcess}, logger.NewStdoutUI())

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



func generateWorkshopName(path string, workshop *unstructured.Unstructured, portal string) string {
	name := workshop.GetName()

	h := sha1.New()

	io.WriteString(h, path)

	hv := fmt.Sprintf("%x", h.Sum(nil))

	name = fmt.Sprintf("%s--%s-%s", portal, name, hv[len(hv)-7:])

	return name
}
