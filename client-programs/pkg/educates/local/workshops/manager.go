package workshops

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"carvel.dev/imgpkg/pkg/imgpkg/cmd"
	imgpkgcmd "carvel.dev/imgpkg/pkg/imgpkg/cmd"
	vendirsync "carvel.dev/vendir/pkg/vendir/cmd"
	yttcmd "carvel.dev/ytt/pkg/cmd/template"

	"github.com/cppforlife/go-cli-ui/ui"
	eduk8sWorkshops "github.com/educates/educates-training-platform/client-programs/pkg/educates/resources/workshops"
	"github.com/educates/educates-training-platform/client-programs/pkg/templates"
	"github.com/pkg/errors"
	"go.yaml.in/yaml/v2"
	"k8s.io/apimachinery/pkg/apis/meta/internalversion/scheme"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
)

type WorkshopManager struct {

}

type WorkshopNewConfig struct {
	Template    string
	Name        string
	Title       string
	Description string
	Image       string
}

type WorkshopExportConfig struct {
	Repository      string
	WorkshopFile    string
	WorkshopVersion string
	DataValuesFlags yttcmd.DataValuesFlags
}

type WorkshopPublishConfig struct {
	Image           string
	Repository      string
	WorkshopFile    string
	ExportWorkshop  string
	WorkshopVersion string
	RegistryFlags   imgpkgcmd.RegistryFlags
	DataValuesFlags yttcmd.DataValuesFlags
}

func NewWorkshopManager() *WorkshopManager {
	return &WorkshopManager{}
}

func (m *WorkshopManager) NewWorkshop(directory string,o *WorkshopNewConfig) error {
	var err error

	directory = filepath.Clean(directory)

	if directory, err = filepath.Abs(directory); err != nil {
		return errors.Wrapf(err, "could not convert path name %q to absolute path", directory)
	}

	if _, err = os.Stat(directory); err == nil {
		return errors.Errorf("target path name %q already exists", directory)
	}

	name := o.Name

	if name == "" {
		name = filepath.Base(directory)
	}

	if match, _ := regexp.MatchString("^[a-z0-9-]+$", name); !match {
		return errors.Errorf("invalid workshop name %q", name)
	}

	parameters := map[string]string{
		"WorkshopName":        name,
		"WorkshopTitle":       o.Title,
		"WorkshopDescription": o.Description,
		"WorkshopImage":       o.Image,
	}

	template := templates.InternalTemplate(o.Template)

	return template.Apply(directory, parameters)
}

func (m *WorkshopManager) Export(directory string,o *WorkshopExportConfig) error {
	// If image name hasn't been supplied read workshop definition file and
	// try to work out image name to Export workshop as.

	rootDirectory := directory
	workshopFilePath := o.WorkshopFile

	if !filepath.IsAbs(workshopFilePath) {
		workshopFilePath = filepath.Join(rootDirectory, workshopFilePath)
	}

	workshopFileData, err := os.ReadFile(workshopFilePath)

	if err != nil {
		return errors.Wrapf(err, "cannot open workshop definition %q", workshopFilePath)
	}

	// Process the workshop YAML data for ytt templating and data variables.

	if workshopFileData, err = eduk8sWorkshops.ProcessWorkshopDefinition(workshopFileData, o.DataValuesFlags); err != nil {
		return errors.Wrap(err, "unable to process workshop definition as template")
	}

	workshopFileData = []byte(strings.ReplaceAll(string(workshopFileData), "$(image_repository)", o.Repository))
	workshopFileData = []byte(strings.ReplaceAll(string(workshopFileData), "$(workshop_version)", o.WorkshopVersion))

	decoder := serializer.NewCodecFactory(scheme.Scheme).UniversalDecoder()

	workshop := &unstructured.Unstructured{}

	err = runtime.DecodeInto(decoder, workshopFileData, workshop)

	if err != nil {
		return errors.Wrap(err, "couldn't parse workshop definition")
	}

	if workshop.GetAPIVersion() != "training.educates.dev/v1beta1" || workshop.GetKind() != "Workshop" {
		return errors.New("invalid type for workshop definition")
	}

	// Insert workshop version property if not specified.

	_, found, _ := unstructured.NestedString(workshop.Object, "spec", "version")

	if !found && o.WorkshopVersion != "latest" {
		unstructured.SetNestedField(workshop.Object, o.WorkshopVersion, "spec", "version")
	}

	// Remove the publish section as will not be accurate after publising.

	unstructured.RemoveNestedField(workshop.Object, "spec", "publish")

	// Export modified workshop definition file.

	workshopFileData, err = yaml.Marshal(&workshop.Object)

	if err != nil {
		return errors.Wrap(err, "couldn't convert workshop definition back to YAML")
	}

	fmt.Print(string(workshopFileData))

	return nil
}

func (m *WorkshopManager) Publish(directory string,o *WorkshopPublishConfig) error {
	// If image name hasn't been supplied read workshop definition file and
	// try to work out image name to publish workshop as.

	rootDirectory := directory
	workshopFilePath := o.WorkshopFile

	workingDirectory, err := os.Getwd()

	if err != nil {
		return errors.Wrap(err, "cannot determine current working directory")
	}

	includePaths := []string{directory}
	excludePaths := []string{".git"}

	if !filepath.IsAbs(workshopFilePath) {
		workshopFilePath = filepath.Join(rootDirectory, workshopFilePath)
	}

	workshopFileData, err := os.ReadFile(workshopFilePath)

	if err != nil {
		return errors.Wrapf(err, "cannot open workshop definition %q", workshopFilePath)
	}

	// Process the workshop YAML data for ytt templating and data variables.

	if workshopFileData, err = eduk8sWorkshops.ProcessWorkshopDefinition(workshopFileData, o.DataValuesFlags); err != nil {
		return errors.Wrap(err, "unable to process workshop definition as template")
	}

	workshopFileData = []byte(strings.ReplaceAll(string(workshopFileData), "$(image_repository)", o.Repository))
	workshopFileData = []byte(strings.ReplaceAll(string(workshopFileData), "$(workshop_version)", o.WorkshopVersion))

	decoder := serializer.NewCodecFactory(scheme.Scheme).UniversalDecoder()

	workshop := &unstructured.Unstructured{}

	err = runtime.DecodeInto(decoder, workshopFileData, workshop)

	if err != nil {
		return errors.Wrap(err, "couldn't parse workshop definition")
	}

	fmt.Printf("Processing workshop with name %q.\n", workshop.GetName())

	if workshop.GetAPIVersion() != "training.educates.dev/v1beta1" || workshop.GetKind() != "Workshop" {
		return errors.New("invalid type for workshop definition")
	}

	image := o.Image

	if image == "" {
		image, _, _ = unstructured.NestedString(workshop.Object, "spec", "publish", "image")
	}

	if image == "" {
		return errors.Errorf("cannot find image name for publishing workshop %q", workshopFilePath)
	}

	// Extract vendir snippet describing subset of files to package up as the
	// workshop image.

	confUI := ui.NewConfUI(ui.NewNoopLogger())

	uiFlags := cmd.UIFlags{
		Color:          true,
		JSON:           false,
		NonInteractive: true,
	}

	uiFlags.ConfigureUI(confUI)

	defer confUI.Flush()

	if fileArtifacts, found, _ := unstructured.NestedSlice(workshop.Object, "spec", "publish", "files"); found && len(fileArtifacts) != 0 {
		tempDir, err := os.MkdirTemp("", "educates-imgpkg")

		if err != nil {
			return errors.Wrapf(err, "unable to create temporary working directory")
		}

		defer os.RemoveAll(tempDir)

		for _, artifactEntry := range fileArtifacts {
			vendirConfig := map[string]interface{}{
				"apiVersion":  "vendir.k14s.io/v1alpha1",
				"kind":        "Config",
				"directories": []interface{}{},
			}

			dir := filepath.Join(tempDir, "files")

			if filePath, found := artifactEntry.(map[string]interface{})["path"].(string); found {
				dir = filepath.Join(tempDir, "files", filepath.Clean(filePath))
			}

			if directoryConfig, found := artifactEntry.(map[string]interface{})["directory"]; found {
				if directoryPath, found := directoryConfig.(map[string]interface{})["path"].(string); found {
					if !filepath.IsAbs(directoryPath) {
						directoryConfig.(map[string]interface{})["path"] = filepath.Join(directory, directoryPath)
					}
				}
			}

			artifactEntry.(map[string]interface{})["path"] = "."

			directoryConfig := map[string]interface{}{
				"path":     dir,
				"contents": []interface{}{artifactEntry},
			}

			vendirConfig["directories"] = append(vendirConfig["directories"].([]interface{}), directoryConfig)

			yamlData, err := yaml.Marshal(&vendirConfig)

			if err != nil {
				return errors.Wrap(err, "unable to generate vendir config")
			}

			vendirConfigFile, err := os.Create(filepath.Join(tempDir, "vendir.yml"))

			if err != nil {
				return errors.Wrap(err, "unable to create vendir config file")
			}

			defer vendirConfigFile.Close()

			_, err = vendirConfigFile.Write(yamlData)

			if err != nil {
				return errors.Wrap(err, "unable to write vendir config file")
			}

			syncOptions := vendirsync.NewSyncOptions(confUI)

			syncOptions.Directories = nil
			syncOptions.Files = []string{filepath.Join(tempDir, "vendir.yml")}

			// Note that Chdir here actually changes the process working directory.

			syncOptions.LockFile = filepath.Join(tempDir, "lock-file")
			syncOptions.Locked = false
			syncOptions.Chdir = tempDir
			syncOptions.AllowAllSymlinkDestinations = false

			if err = syncOptions.Run(); err != nil {
				fmt.Println(string(yamlData))

				return errors.Wrap(err, "failed to prepare image files for publishing")
			}
		}

		// Restore working directory as was changed.

		os.Chdir((workingDirectory))

		rootDirectory = filepath.Join(tempDir, "files")
		includePaths = []string{rootDirectory}
	}

	// Now publish workshop directory contents as OCI image artifact.

	fmt.Printf("Publishing workshop files to %q.\n", image)

	pushOptions := imgpkgcmd.NewPushOptions(confUI)

	pushOptions.ImageFlags.Image = image
	pushOptions.FileFlags.Files = append(pushOptions.FileFlags.Files, includePaths...)
	pushOptions.FileFlags.ExcludedFilePaths = append(pushOptions.FileFlags.ExcludedFilePaths, excludePaths...)

	pushOptions.RegistryFlags = o.RegistryFlags

	err = pushOptions.Run()

	if err != nil {
		return errors.Wrap(err, "unable to push image artifact for workshop")
	}

	// We add a newline to output for better readability.
	fmt.Println()

	// Export modified workshop definition file.

	exportWorkshop := o.ExportWorkshop

	if exportWorkshop != "" {
		// Insert workshop version property if not specified.

		_, found, _ := unstructured.NestedString(workshop.Object, "spec", "version")

		if !found && o.WorkshopVersion != "latest" {
			unstructured.SetNestedField(workshop.Object, o.WorkshopVersion, "spec", "version")
		}

		// Remove the publish section as will not be accurate after publising.

		unstructured.RemoveNestedField(workshop.Object, "spec", "publish")

		workshopFileData, err = yaml.Marshal(&workshop.Object)

		if err != nil {
			return errors.Wrap(err, "couldn't convert workshop definition back to YAML")
		}

		if !filepath.IsAbs(exportWorkshop) {
			exportWorkshop = filepath.Join(workingDirectory, exportWorkshop)
		}

		exportWorkshopFile, err := os.Create(exportWorkshop)

		if err != nil {
			return errors.Wrap(err, "unable to create exported workshop definition file")
		}

		defer exportWorkshopFile.Close()

		_, err = exportWorkshopFile.Write(workshopFileData)

		if err != nil {
			return errors.Wrap(err, "unable to write exported workshop definition file")
		}
	}

	return nil
}
