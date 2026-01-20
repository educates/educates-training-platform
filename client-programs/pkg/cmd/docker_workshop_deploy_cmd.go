package cmd

import (
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	yttcmd "carvel.dev/ytt/pkg/cmd/template"
	"github.com/educates/educates-training-platform/client-programs/pkg/docker"
	"github.com/educates/educates-training-platform/client-programs/pkg/utils"
	"github.com/spf13/cobra"
)

const dockerWorkshopDeployExample = `
  # Deploy Educates workshop to Docker in current workshop directory and using default workshop file
  educates docker workshop deploy

  # Deploy Educates workshop to Docker from specific path and using custom workshop file
  educates docker workshop deploy --path ./workshop --workshop-file custom-workshop.yaml

  # Deploy Educates workshop to Docker with custom host and port
  educates docker workshop deploy --host 192.168.1.100 --port 10081

  # Deploy Educates workshop to Docker with custom local repository
  educates docker workshop deploy --local-repository localhost:5001

  # Deploy Educates workshop adding to the session kubeconfig to specified Kind cluster
  educates docker workshop deploy --cluster my-cluster

  # Deploy Educates workshop adding to the specified kubeconfig to the session
  educates docker workshop deploy --kubeconfig /path/to/kubeconfig

  # Deploy Educates workshop in current folder without opening the browser
  educates docker workshop deploy --disable-open-browser
`

type DockerWorkshopDeployOptions struct {
	Path               string
	Host               string
	Port               uint
	LocalRepository    string
	DisableOpenBrowser bool
	ImageRepository    string
	ImageVersion       string
	Cluster            string
	KubeConfig         string
	Assets             string
	WorkshopFile       string
	WorkshopImage      string
	WorkshopVersion    string
	DataValuesFlags    yttcmd.DataValuesFlags
}

func (o *DockerWorkshopDeployOptions) Run(cmd *cobra.Command) error {
	dockerWorkshopsManager := docker.NewDockerWorkshopsManager()

	config := docker.DockerWorkshopDeployConfig{
		Path: o.Path,
		Host: o.Host,
		Port: o.Port,
		LocalRepository: o.LocalRepository,
		DisableOpenBrowser: o.DisableOpenBrowser,
		ImageRepository: o.ImageRepository,
		ImageVersion: o.ImageVersion,
	}
	_, err := dockerWorkshopsManager.DeployWorkshop(&config, cmd.OutOrStdout(), cmd.OutOrStderr())

	if err != nil {
		return err
	}

	// TODO: XXX Need a better way of handling very long startup times for container
	// due to workshop content or package downloads.
	url := fmt.Sprintf("http://workshop.%s.nip.io:%d", strings.ReplaceAll(o.Host, ".", "-"), o.Port)

	if !o.DisableOpenBrowser {
		for i := 1; i < 300; i++ {
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

	return nil
}

func (p *ProjectInfo) NewDockerWorkshopDeployCmd() *cobra.Command {
	var o DockerWorkshopDeployOptions

	var c = &cobra.Command{
		Args:  cobra.NoArgs,
		Use:   "deploy",
		Short: "Deploy workshop to Docker",
		RunE:  func(cmd *cobra.Command, _ []string) error { return o.Run(cmd) },
		Example: dockerWorkshopDeployExample,
	}

	c.Flags().StringVarP(
		&o.Path,
		"file",
		"f",
		".",
		"path to local workshop directory, definition file, or URL for workshop definition file",
	)
	c.Flags().StringVar(
		&o.Host,
		"host",
		"127.0.0.1",
		"the IP address to host the workshop",
	)
	c.Flags().UintVarP(
		&o.Port,
		"port",
		"p",
		10081,
		"port to host the workshop",
	)
	c.Flags().StringVar(
		&o.LocalRepository,
		"local-repository",
		"localhost:5001",
		"the address of the local image repository",
	)
	c.Flags().BoolVar(
		&o.DisableOpenBrowser,
		"disable-open-browser",
		false,
		"disable automatic launching of the browser",
	)
	c.Flags().StringVar(
		&o.ImageRepository,
		"image-repository",
		p.ImageRepository,
		"image repository hosting workshop base images",
	)
	c.Flags().StringVar(
		&o.ImageVersion,
		"image-version",
		p.Version,
		"version of workshop base images to be used",
	)
	c.Flags().StringVar(
		&o.Cluster,
		"cluster",
		"",
		"name of a Kind cluster to connect to workshop",
	)
	c.Flags().StringVar(
		&o.KubeConfig,
		"kubeconfig",
		"",
		"path to kubeconfig to connect to workshop",
	)
	c.Flags().StringVar(
		&o.Assets,
		"assets",
		"",
		"local directory path to workshop assets",
	)

	c.Flags().StringVar(
		&o.WorkshopFile,
		"workshop-file",
		"resources/workshop.yaml",
		"location of the workshop definition file",
	)

	c.Flags().StringVar(
		&o.WorkshopImage,
		"workshop-image",
		"",
		"workshop base image override",
	)
	c.Flags().StringVar(
		&o.WorkshopVersion,
		"workshop-version",
		"latest",
		"version of the workshop definition",
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
