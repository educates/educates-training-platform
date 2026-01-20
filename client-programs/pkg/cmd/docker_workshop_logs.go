package cmd

import (
	"os/exec"

	yttcmd "carvel.dev/ytt/pkg/cmd/template"
	"github.com/educates/educates-training-platform/client-programs/pkg/constants"
	"github.com/educates/educates-training-platform/client-programs/pkg/educates/resources/workshops"
	"github.com/pkg/errors"
	"github.com/spf13/cobra"
)

const dockerWorkshopLogsExample = `
  # Display logs for Educates workshop in current workshop directory
  educates docker workshop logs

  # Display logs for Educates workshop with provided name
  educates docker workshop logs --name my-workshop

  # Display logs for Educates workshop from specific path and using custom workshop file
  educates docker workshop logs --path ./workshop --workshop-file custom-workshop.yaml

  # Display logs for Educates workshop in current folder and follow the logs
  educates docker workshop logs --follow
`

type DockerWorkshopLogsOptions struct {
	Name            string
	Path            string
	Follow          bool
	WorkshopFile    string
	WorkshopVersion string
	DataValuesFlags yttcmd.DataValuesFlags
}

func (o *DockerWorkshopLogsOptions) Run(cmd *cobra.Command) error {
	var err error

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
		definitionConfig := workshops.WorkshopDefinitionConfig{
			Name: o.Name,
			Path: path,
			Portal: constants.DefaultPortalName,
			WorkshopFile: o.WorkshopFile,
			WorkshopVersion: o.WorkshopVersion,
			DataValueFlags: o.DataValuesFlags,
		}
		workshop, err := workshops.LoadWorkshopDefinition(&definitionConfig)
		if err != nil {
			return err
		}

		name = workshop.GetName()
	}

	commandArgs := []string{
		"compose",
		"--project-name",
		name,
		"logs",
	}

	if o.Follow {
		commandArgs = append(commandArgs, "--follow")
	}

	dockerCommand := exec.Command("docker", commandArgs...)

	dockerCommand.Stdout = cmd.OutOrStdout()
	dockerCommand.Stderr = cmd.OutOrStderr()

	err = dockerCommand.Run()

	if err != nil {
		return errors.Wrap(err, "unable to obtain logs for workshop")
	}

	return nil
}

func (p *ProjectInfo) NewDockerWorkshopLogsCmd() *cobra.Command {
	var o DockerWorkshopLogsOptions

	var c = &cobra.Command{
		Args:  cobra.NoArgs,
		Use:   "logs",
		Short: "Display logs for workshop",
		RunE:  func(cmd *cobra.Command, _ []string) error { return o.Run(cmd) },
		Example: dockerWorkshopLogsExample,
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
	c.Flags().BoolVar(
		&o.Follow,
		"follow",
		false,
		"specify if the logs should be streamed",
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
