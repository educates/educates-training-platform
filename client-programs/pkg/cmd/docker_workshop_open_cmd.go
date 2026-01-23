package cmd

import (
	"context"
	"io"
	"net/http"
	"time"

	yttcmd "carvel.dev/ytt/pkg/cmd/template"
	"github.com/educates/educates-training-platform/client-programs/pkg/constants"
	"github.com/educates/educates-training-platform/client-programs/pkg/docker"
	"github.com/educates/educates-training-platform/client-programs/pkg/educates/resources/workshops"
	"github.com/educates/educates-training-platform/client-programs/pkg/utils"
	"github.com/pkg/errors"
	"github.com/spf13/cobra"
)

const dockerWorkshopOpenExample = `
  # Open Educates workshop in browser in current workshop directory
  educates docker workshop open

  # Open Educates workshop in browser with provided name
  educates docker workshop open --name my-workshop

  # Open Educates workshop in browser from specific path and using custom workshop file
  educates docker workshop open --path ./workshop --workshop-file custom-workshop.yaml
`

type DockerWorkshopOpenOptions struct {
	Name            string
	Path            string
	WorkshopFile    string
	WorkshopVersion string
	DataValuesFlags yttcmd.DataValuesFlags
}

func (o *DockerWorkshopOpenOptions) Run() error {
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

	name = name + "-workshop-1"

	ctx := context.Background()

	cli, err := docker.NewDockerClient()

	if err != nil {
		return errors.Wrap(err, "unable to create docker client")
	}

	container, err := cli.ContainerInspect(ctx, name)

	if err != nil {
		return errors.New("unable to find workshop")
	}

	url, found := container.Config.Labels["training.educates.dev/url"]

	if !found || url == "" {
		return errors.New("can't determine URL for workshop")
	}

	// TODO: XXX Need a better way of handling very long startup times for container
	// due to workshop content or package downloads.
	for i := 1; i < 120; i++ {
		time.Sleep(time.Second)

		resp, err := http.Get(url)

		if err != nil {
			continue
		}

		defer resp.Body.Close()
		io.ReadAll(resp.Body)

		break
	}

	return utils.OpenBrowser(url)
}

func (p *ProjectInfo) NewDockerWorkshopOpenCmd() *cobra.Command {
	var o DockerWorkshopOpenOptions

	var c = &cobra.Command{
		Args:  cobra.NoArgs,
		Use:   "open",
		Short: "Open workshop in browser",
		RunE:  func(_ *cobra.Command, _ []string) error { return o.Run() },
		Example: dockerWorkshopOpenExample,
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
