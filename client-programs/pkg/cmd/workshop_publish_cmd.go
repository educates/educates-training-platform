package cmd

import (
	"time"

	"github.com/educates/educates-training-platform/client-programs/pkg/workshops"
	"github.com/spf13/cobra"
)

var (
	workshopPublishExample = `
  # Publish workshop files to local registry
  educates workshop publish

  # Publish workshop files to specific registry
  educates workshop publish --image-repository ghcr.io/myorg

  # Publish workshop files with specific version
  educates workshop publish --workshop-version v1.0.0

  # Publish workshop files with custom workshop definition
  educates workshop publish --workshop-file custom-workshop.yaml

  # Publish workshop files and export modified workshop definition
  educates workshop publish --export-workshop exported-workshop.yaml

  # Publish workshop files with registry authentication
  educates workshop publish --registry-username user --registry-password pass

  # Publish workshop files with data values
  educates workshop publish --data-value workshop.title="My Workshop" --data-value workshop.description="A great workshop"
`
)

func (p *ProjectInfo) NewWorkshopPublishCmd() *cobra.Command {
	var o workshops.FilesPublishOptions

	var c = &cobra.Command{
		Args:    cobra.MaximumNArgs(1),
		Use:     "publish [PATH]",
		Short:   "Publish workshop files to repository",
		RunE:    func(cmd *cobra.Command, args []string) error { return o.Run(args) },
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
