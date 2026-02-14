package cmd

import (
	yttcmd "carvel.dev/ytt/pkg/cmd/template"
	"github.com/educates/educates-training-platform/client-programs/pkg/constants"
	"github.com/educates/educates-training-platform/client-programs/pkg/docker"
	"github.com/educates/educates-training-platform/client-programs/pkg/educates"
	"github.com/spf13/cobra"
)

const dockerWorkshopDeleteExample = `
  # Delete Educates workshop from Docker in current workshop directory and using default workshop file
  educates docker workshop delete

  # Delete Educates workshop from Docker from specific portal
  educates docker workshop delete --portal my-portal

  # Delete Educates workshop from Docker defined with custom path and workshop file
  educates docker workshop delete --path ./workshop --workshop-file custom-workshop.yaml
`

type DockerWorkshopDeleteOptions struct {
	Name            string
	Path            string
	WorkshopFile    string
	WorkshopVersion string
	DataValuesFlags yttcmd.DataValuesFlags
}

func (o *DockerWorkshopDeleteOptions) Run(cmd *cobra.Command) error {
	var name = o.Name

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
		workshop, err := educates.LoadWorkshopDefinition(&educates.WorkshopDefinitionConfig{
			Name: o.Name,
			Path: path,
			Portal: constants.DefaultPortalName,
			WorkshopFile: o.WorkshopFile,
			WorkshopVersion: o.WorkshopVersion,
			DataValueFlags: o.DataValuesFlags,
		})
		if err != nil {
			return err
		}

		name = workshop.GetName()
	}

	dockerWorkshopsManager := docker.NewDockerWorkshopsManager()

	return dockerWorkshopsManager.DeleteWorkshop(name, cmd.OutOrStdout(), cmd.OutOrStderr())
}

func (p *ProjectInfo) NewDockerWorkshopDeleteCmd() *cobra.Command {
	var o DockerWorkshopDeleteOptions

	var c = &cobra.Command{
		Args:  cobra.NoArgs,
		Use:   "delete",
		Short: "Delete workshop from Docker",
		RunE:  func(cmd *cobra.Command, _ []string) error { return o.Run(cmd) },
		Example: dockerWorkshopDeleteExample,
	}

	c.Flags().StringVarP(
		&o.Name,
		"name",
		"n",
		"",
		"name to be used for the workshop definition, generated if not set",
	)

	// TODO: Move "." to a constant
	c.Flags().StringVarP(
		&o.Path,
		"file",
		"f",
		".",
		"path to local workshop directory, definition file, or URL for workshop definition file",
	)

	// TODO: Move "resources/workshop.yaml" to a constant
	c.Flags().StringVar(
		&o.WorkshopFile,
		"workshop-file",
		"resources/workshop.yaml",
		"location of the workshop definition file",
	)

	// TODO: Move "latest" to a constant
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
