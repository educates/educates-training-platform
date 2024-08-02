package cmd

import (
	"bytes"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	imgpkgcmd "carvel.dev/imgpkg/pkg/imgpkg/cmd"
	"carvel.dev/kapp/pkg/kapp/cmd"
	vendirsync "carvel.dev/vendir/pkg/vendir/cmd"
	yttcmd "carvel.dev/ytt/pkg/cmd/template"
	yttcmdui "carvel.dev/ytt/pkg/cmd/ui"
	"carvel.dev/ytt/pkg/files"
	"carvel.dev/ytt/pkg/yamlmeta"
	"github.com/cppforlife/go-cli-ui/ui"
	"github.com/pkg/errors"
	"github.com/spf13/cobra"
	"gopkg.in/yaml.v2"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/kubectl/pkg/scheme"
)

type FilesPublishOptions struct {
	Image           string
	Repository      string
	WorkshopFile    string
	ExportWorkshop  string
	WorkshopVersion string
	RegistryFlags   imgpkgcmd.RegistryFlags
	DataValuesFlags yttcmd.DataValuesFlags
}

func (o *FilesPublishOptions) Run(args []string) error {
	var err error

	var directory string

	if len(args) != 0 {
		directory = filepath.Clean(args[0])
	} else {
		directory = "."
	}

	if directory, err = filepath.Abs(directory); err != nil {
		return errors.Wrap(err, "couldn't convert workshop directory to absolute path")
	}

	fileInfo, err := os.Stat(directory)

	if err != nil || !fileInfo.IsDir() {
		return errors.New("workshop directory does not exist or path is not a directory")
	}

	return o.Publish(directory)
}

func (o *FilesPublishOptions) Publish(directory string) error {
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

	if workshopFileData, err = processWorkshopDefinition(workshopFileData, o.DataValuesFlags); err != nil {
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

func (p *ProjectInfo) NewWorkshopPublishCmd() *cobra.Command {
	var o FilesPublishOptions

	var c = &cobra.Command{
		Args:  cobra.MaximumNArgs(1),
		Use:   "publish [PATH]",
		Short: "Publish workshop files to repository",
		RunE:  func(cmd *cobra.Command, args []string) error { return o.Run(args) },
	}

	c.Flags().StringVar(
		&o.Image,
		"image",
		"",
		"name of the workshop files image artifact",
	)
	c.Flags().StringVar(
		&o.Repository,
		"image-repository",
		"localhost:5001",
		"the address of the image repository",
	)
	c.Flags().StringVar(
		&o.WorkshopFile,
		"workshop-file",
		"resources/workshop.yaml",
		"location of the workshop definition file",
	)
	c.Flags().StringVar(
		&o.ExportWorkshop,
		"export-workshop",
		"",
		"location to save modified workshop file",
	)

	c.Flags().StringVar(
		&o.WorkshopVersion,
		"workshop-version",
		"latest",
		"version of the workshop being published",
	)

	c.Flags().StringSliceVar(
		&o.RegistryFlags.CACertPaths,
		"registry-ca-cert-path",
		nil,
		"Add CA certificates for registry API",
	)
	c.Flags().BoolVar(
		&o.RegistryFlags.VerifyCerts,
		"registry-verify-certs",
		true,
		"Set whether to verify server's certificate chain and host name",
	)
	c.Flags().BoolVar(
		&o.RegistryFlags.Insecure,
		"registry-insecure",
		false,
		"Allow the use of http when interacting with registries",
	)

	c.Flags().StringVar(
		&o.RegistryFlags.Username,
		"registry-username",
		"",
		"Set username for registry authentication",
	)
	c.Flags().StringVar(
		&o.RegistryFlags.Password,
		"registry-password",
		"",
		"Set password for registry authentication",
	)
	c.Flags().StringVar(
		&o.RegistryFlags.Token,
		"registry-token",
		"",
		"Set token for registry authentication",
	)
	c.Flags().BoolVar(
		&o.RegistryFlags.Anon,
		"registry-anon",
		false,
		"Set anonymous for registry authentication",
	)

	c.Flags().DurationVar(
		&o.RegistryFlags.ResponseHeaderTimeout,
		"registry-response-header-timeout",
		30*time.Second,
		"Maximum time to allow a request to wait for a server's response headers from the registry (ms|s|m|h)",
	)
	c.Flags().IntVar(
		&o.RegistryFlags.RetryCount,
		"registry-retry-count",
		5,
		"Set the number of times imgpkg retries to send requests to the registry in case of an error",
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

func processWorkshopDefinition(yamlData []byte, dataValueFlags yttcmd.DataValuesFlags) ([]byte, error) {
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
