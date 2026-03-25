package cmd

import (
	"archive/zip"
	"io"
	"os"
	"path/filepath"
	"strings"

	yttcmd "carvel.dev/ytt/pkg/cmd/template"
	"github.com/pkg/errors"
	"github.com/spf13/cobra"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/kubectl/pkg/scheme"

	"github.com/educates/educates-training-platform/client-programs/pkg/renderer"
	"github.com/educates/educates-training-platform/client-programs/pkg/workshops"
)

func createZIPFile(tempDir string, outputFile string) error {
	zipFile, err := os.Create(outputFile)

	if err != nil {
		return errors.Wrap(err, "unable to create ZIP file")
	}

	defer zipFile.Close()

	zipWriter := zip.NewWriter(zipFile)
	defer zipWriter.Close()

	publicDir := filepath.Join(tempDir, "public")

	err = filepath.Walk(publicDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if info.IsDir() {
			return nil
		}

		relPath, err := filepath.Rel(publicDir, path)
		if err != nil {
			return err
		}

		// Create a new file entry in the ZIP
		writer, err := zipWriter.Create(relPath)
		if err != nil {
			return err
		}

		// Open the source file
		file, err := os.Open(path)
		if err != nil {
			return err
		}
		defer file.Close()

		// Copy the file contents to the ZIP
		_, err = io.Copy(writer, file)
		return err
	})

	if err != nil {
		return errors.Wrap(err, "unable to create ZIP file")
	}

	err = zipWriter.Close()

	if err != nil {
		return errors.Wrap(err, "unable to close ZIP file")
	}

	return nil
}

type FilesRenderOptions struct {
	Repository      string
	WorkshopFile    string
	WorkshopVersion string
	DataValuesFlags yttcmd.DataValuesFlags
	OutputFile      string
}

func (o *FilesRenderOptions) Run(args []string) error {
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

	return o.Render(directory, o.OutputFile)
}

func (o *FilesRenderOptions) Render(directory string, outputFile string) error {
	// If image name hasn't been supplied read workshop definition file and
	// try to work out image name to Export workshop as.

	workshopDir := directory + "/workshop"
	workshopFilePath := o.WorkshopFile

	if !filepath.IsAbs(workshopFilePath) {
		workshopFilePath = filepath.Join(directory, workshopFilePath)
	}

	workshopFileData, err := os.ReadFile(workshopFilePath)

	if err != nil {
		return errors.Wrapf(err, "cannot open workshop definition %q", workshopFilePath)
	}

	// Process the workshop YAML data for ytt templating and data variables.

	if workshopFileData, err = workshops.ProcessWorkshopDefinition(workshopFileData, o.DataValuesFlags); err != nil {
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

	// First create directory to hold unpacked files for Hugo to use.

	var tempDir string

	if tempDir, err = renderer.PopulateTemporaryDirectory(); err != nil {
		return err
	}

	defer os.RemoveAll(tempDir)

	// Generate (or regenerate) the Hugo configuration.

	params := map[string]string{}

	workshopTitle, _, _ := unstructured.NestedString(workshop.Object, "spec", "title")
	workshopDescription, _, _ := unstructured.NestedString(workshop.Object, "spec", "description")

	params["workshop_title"] = workshopTitle
	params["workshop_description"] = workshopDescription

	params["assets_path"] = "/static"

	err = renderer.GenerateHugoConfiguration(workshopDir, tempDir, params, "")

	if err != nil {
		return errors.Wrap(err, "unable to generate Hugo configuration")
	}

	// Build the static HTML files.

	err = renderer.RenderHugoStaticHTML(workshopDir, tempDir)

	if err != nil {
		return errors.Wrap(err, "unable to build static HTML files")
	}

	// Create ZIP file of the static HTML files.

	err = createZIPFile(tempDir, outputFile)

	if err != nil {
		return errors.Wrap(err, "unable to create ZIP file")
	}

	return nil
}

func (p *ProjectInfo) NewWorkshopRenderCmd() *cobra.Command {
	var o FilesRenderOptions

	var c = &cobra.Command{
		Args:  cobra.MaximumNArgs(1),
		Use:   "render [PATH]",
		Short: "Render workshop as static HTML",
		RunE:  func(cmd *cobra.Command, args []string) error { return o.Run(args) },
	}

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
		&o.WorkshopVersion,
		"workshop-version",
		"latest",
		"version of the workshop being published",
	)

	c.Flags().StringVarP(
		&o.OutputFile,
		"output-file",
		"o",
		"",
		"location of the ZIP output file",
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

	c.MarkFlagRequired("output-file")

	return c
}
