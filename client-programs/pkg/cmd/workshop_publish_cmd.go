package cmd

import (
	"os"
	"path/filepath"
	"time"

	imgpkgcmd "carvel.dev/imgpkg/pkg/imgpkg/cmd"
	yttcmd "carvel.dev/ytt/pkg/cmd/template"
	"github.com/educates/educates-training-platform/client-programs/pkg/educates/local/workshops"
	"github.com/educates/educates-training-platform/client-programs/pkg/utils"
	"github.com/pkg/errors"
	"github.com/spf13/cobra"
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

const workshopPublishExample = `
  # Publish workshop files to repository in current directory
  educates workshop publish

  # Publish workshop files to repository in my-workshop directory
  educates workshop publish my-workshop

  # Publish workshop files to repository with a specific image in my-workshop directory
  educates workshop publish my-workshop --image=my-workshop-image-files

  # Publish workshop files to repository with a specific image and repository in my-workshop directory
  educates workshop publish my-workshop --image=my-workshop-image-files --image-repository=ghcr.io/educates --workshop-version=1.0.0
`
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

	config := workshops.WorkshopPublishConfig{
		Image: o.Image,
		Repository: o.Repository,
		WorkshopFile: o.WorkshopFile,
		ExportWorkshop: o.ExportWorkshop,
		WorkshopVersion: o.WorkshopVersion,
		RegistryFlags: o.RegistryFlags,
		DataValuesFlags: o.DataValuesFlags,
	}

	m := workshops.NewWorkshopManager()

	return m.Publish(directory, &config)
}


func (p *ProjectInfo) NewWorkshopPublishCmd() *cobra.Command {
	var o FilesPublishOptions

	var c = &cobra.Command{
		Args:  func(cmd *cobra.Command, args []string) error {
			if len(args) > 1 {
				return utils.CmdError(cmd, "too many arguments", "[PATH]")
			}
			return nil
		},
		Use:   "publish [PATH]",
		Short: "Publish workshop files to repository",
		RunE:  func(cmd *cobra.Command, args []string) error { return o.Run(args) },
		Example: workshopPublishExample,
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
