package workshops

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	yttcmd "carvel.dev/ytt/pkg/cmd/template"
	"github.com/pkg/errors"
	"gopkg.in/yaml.v2"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/kubectl/pkg/scheme"
)

type FilesExportOptions struct {
	Repository      string
	WorkshopFile    string
	WorkshopVersion string
	DataValuesFlags yttcmd.DataValuesFlags
}

func (o *FilesExportOptions) Run(args []string) error {
	var err error

	var workshopDir string

	if len(args) != 0 {
		workshopDir = filepath.Clean(args[0])
	} else {
		workshopDir = "."
	}

	if workshopDir, err = filepath.Abs(workshopDir); err != nil {
		return errors.Wrap(err, "couldn't convert workshop directory to absolute path")
	}

	fileInfo, err := os.Stat(workshopDir)

	if err != nil || !fileInfo.IsDir() {
		return errors.New("workshop directory does not exist or path is not a directory")
	}

	return o.Export(workshopDir)
}

func (o *FilesExportOptions) Export(workshopDir string) error {
	rootDirectory := workshopDir
	workshopFilePath := o.WorkshopFile

	// 1. Find the workshop definition file
	if !filepath.IsAbs(workshopFilePath) {
		workshopFilePath = filepath.Join(rootDirectory, workshopFilePath)
	}

	workshopFileData, err := os.ReadFile(workshopFilePath)

	if err != nil {
		return errors.Wrapf(err, "cannot open workshop definition %q", workshopFilePath)
	}

	// 2. Process the workshop definition file through the ytt templating engine
	if workshopFileData, err = ProcessWorkshopDefinition(workshopFileData, o.DataValuesFlags); err != nil {
		return errors.Wrap(err, "unable to process workshop definition as template")
	}

	// 3. Replace the image repository and workshop version placeholders with the actual values valid for exporting and publishing
	workshopFileData = []byte(strings.ReplaceAll(string(workshopFileData), "$(image_repository)", o.Repository))
	workshopFileData = []byte(strings.ReplaceAll(string(workshopFileData), "$(workshop_version)", o.WorkshopVersion))

	// 4. Decode the workshop definition and perform validations
	decoder := serializer.NewCodecFactory(scheme.Scheme).UniversalDecoder()

	workshop := &unstructured.Unstructured{}

	err = runtime.DecodeInto(decoder, workshopFileData, workshop)

	if err != nil {
		return errors.Wrap(err, "couldn't parse workshop definition")
	}

	if workshop.GetAPIVersion() != "training.educates.dev/v1beta1" || workshop.GetKind() != "Workshop" {
		return errors.New("invalid type for workshop definition")
	}

	_, found, _ := unstructured.NestedString(workshop.Object, "spec", "version")

	if !found && o.WorkshopVersion != "latest" {
		unstructured.SetNestedField(workshop.Object, o.WorkshopVersion, "spec", "version")
	}

	// 5. Remove the publish field from the workshop definition
	unstructured.RemoveNestedField(workshop.Object, "spec", "publish")

	// 6. Convert the workshop definition back to YAML format
	workshopFileData, err = yaml.Marshal(&workshop.Object)

	if err != nil {
		return errors.Wrap(err, "couldn't convert workshop definition back to YAML")
	}

	// 7. Print the workshop definition to stdout
	fmt.Print(string(workshopFileData))

	return nil
}
