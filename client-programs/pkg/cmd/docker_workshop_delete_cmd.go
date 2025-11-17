package cmd

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"

	yttcmd "carvel.dev/ytt/pkg/cmd/template"
	"github.com/educates/educates-training-platform/client-programs/pkg/docker"
	"github.com/educates/educates-training-platform/client-programs/pkg/utils"
	"github.com/pkg/errors"
	"github.com/spf13/cobra"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

type DockerWorkshopDeleteOptions struct {
	Name            string
	Path            string
	WorkshopFile    string
	WorkshopVersion string
	DataValuesFlags yttcmd.DataValuesFlags
}

func (m *DockerWorkshopsManager) DeleteWorkshop(name string, stdout io.Writer, stderr io.Writer) error {
	m.SetWorkshopStatus(name, "", "", "Stopping")

	defer m.ClearWorkshopStatus(name)

	dockerCommand := exec.Command(
		"docker",
		"compose",
		"--project-name",
		name,
		"rm",
		"--stop",
		"--force",
		"--volumes",
	)

	dockerCommand.Stdout = stdout
	dockerCommand.Stderr = stderr

	err := dockerCommand.Run()

	if err != nil {
		return errors.Wrap(err, "unable to stop workshop")
	}

	ctx := context.Background()

	cli, err := docker.NewDockerClient()

	if err != nil {
		return errors.Wrap(err, "unable to create docker client")
	}

	err = cli.VolumeRemove(ctx, fmt.Sprintf("%s_workshop", name), false)

	if err != nil {
		return errors.Wrap(err, "unable to delete workshop volume")
	}

	configFileDir := utils.GetEducatesHomeDir()
	workshopConfigDir := path.Join(configFileDir, "workshops", name)
	composeConfigDir := path.Join(configFileDir, "compose", name)

	os.RemoveAll(workshopConfigDir)
	os.RemoveAll(composeConfigDir)

	return nil
}

func (o *DockerWorkshopDeleteOptions) Run(cmd *cobra.Command) error {
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

		var workshop *unstructured.Unstructured

		if workshop, err = loadWorkshopDefinition(o.Name, path, "educates-cli", o.WorkshopFile, o.WorkshopVersion, o.DataValuesFlags); err != nil {
			return err
		}

		name = workshop.GetName()
	}

	dockerWorkshopsManager := NewDockerWorkshopsManager()

	return dockerWorkshopsManager.DeleteWorkshop(name, cmd.OutOrStdout(), cmd.OutOrStderr())
}

func (p *ProjectInfo) NewDockerWorkshopDeleteCmd() *cobra.Command {
	var o DockerWorkshopDeleteOptions

	var c = &cobra.Command{
		Args:  cobra.NoArgs,
		Use:   "delete",
		Short: "Delete workshop from Docker",
		RunE:  func(cmd *cobra.Command, _ []string) error { return o.Run(cmd) },
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
