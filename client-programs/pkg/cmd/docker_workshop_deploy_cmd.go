package cmd

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"runtime"
	"strings"
	"text/template"
	"time"

	yttcmd "carvel.dev/ytt/pkg/cmd/template"
	composeloader "github.com/compose-spec/compose-go/loader"
	composetypes "github.com/compose-spec/compose-go/types"
	"github.com/educates/educates-training-platform/client-programs/pkg/docker"
	"github.com/educates/educates-training-platform/client-programs/pkg/utils"
	"github.com/pkg/errors"
	"github.com/spf13/cobra"
	"golang.org/x/exp/slices"
	"gopkg.in/yaml.v2"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/kind/pkg/cluster"
	"sigs.k8s.io/kind/pkg/cmd"
)

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

const containerScript = `exec bash -s << "EOF"
mkdir -p /opt/eduk8s/config
cat > /opt/eduk8s/config/workshop.yaml << "EOS"
{{ .WorkshopConfig -}}
EOS
{{ if .Assets -}}
cat > /opt/eduk8s/config/vendir-assets-01.yaml << "EOS"
apiVersion: vendir.k14s.io/v1alpha1
kind: Config
directories:
- path: /opt/assets/files
  contents:
  - directory:
      path: /opt/eduk8s/mnt/assets
    path: .
EOS
{{ else -}}
{{ range $k, $v := .VendirFilesConfig -}}
{{ $off := inc $k -}}
cat > /opt/eduk8s/config/vendir-assets-{{ printf "%02d" $off }}.yaml << "EOS"
{{ $v -}}
EOS
{{ end -}}
{{ end -}}
{{ if .VendirPackagesConfig -}}
cat > /opt/eduk8s/config/vendir-packages.yaml << "EOS"
{{ .VendirPackagesConfig -}}
EOS
{{ end -}}
{{ if .KubeConfig -}}
mkdir -p /opt/kubeconfig
cat > /opt/kubeconfig/config << "EOS"
{{ .KubeConfig -}}
EOS
{{ end -}}
exec start-container
EOF
`

func (m *DockerWorkshopsManager) DeployWorkshop(o *DockerWorkshopDeployOptions, stdout io.Writer, stderr io.Writer) (string, error) {
	var err error

	// If path not provided assume the current working directory. When loading
	// the workshop will then expect the workshop definition to reside in the
	// resources/workshop.yaml file under the directory, the same as if a
	// directory path was provided explicitly.

	if o.Path == "" {
		o.Path = "."
	}

	// Load the workshop definition. The path can be a HTTP/HTTPS URL for a
	// local file system path for a directory or file.

	var workshop *unstructured.Unstructured

	if workshop, err = loadWorkshopDefinition("", o.Path, "educates-cli", o.WorkshopFile, o.WorkshopVersion, o.DataValuesFlags); err != nil {
		return "", err
	}

	name := workshop.GetName()

	m.SetWorkshopStatus(name, "", o.Path, "Starting")

	defer m.ClearWorkshopStatus(name)

	originalName := workshop.GetAnnotations()["training.educates.dev/workshop"]

	configFileDir := utils.GetEducatesHomeDir()
	composeConfigDir := path.Join(configFileDir, "compose", name)

	err = os.MkdirAll(composeConfigDir, os.ModePerm)

	if err != nil {
		return name, errors.Wrapf(err, "unable to create workshops compose directory")
	}

	ctx := context.Background()

	cli, err := docker.NewDockerClient()

	if err != nil {
		return name, errors.Wrap(err, "unable to create docker client")
	}

	_, err = cli.ContainerInspect(ctx, name)

	if err == nil {
		return name, errors.New("this workshop is already running")
	}

	registryNetwork := false

	if o.LocalRepository == "localhost:5001" {
		o.LocalRepository = "registry.docker.local:5000"
	}

	var registryIP string

	registryInfo, err := cli.ContainerInspect(ctx, "educates-registry")

	if err == nil {
		educatesNetwork, exists := registryInfo.NetworkSettings.Networks["educates"]

		if !exists {
			return name, errors.New("registry is not attached to educates network")
		}

		registryNetwork = true
		registryIP = educatesNetwork.IPAddress
	} else {
		o.LocalRepository = ""
	}

	var kubeConfigData string

	if o.KubeConfig != "" {
		kubeConfigBytes, err := os.ReadFile(o.KubeConfig)

		if err != nil {
			return name, errors.Wrap(err, "unable to read kubeconfig file")
		}

		kubeConfigData = string(kubeConfigBytes)
	}

	if o.Cluster != "" {
		kubeConfigData, err = generateClusterKubeconfig(o.Cluster)

		if err != nil {
			return name, err
		}
	}

	var workshopConfigData string
	var vendirFilesConfigData []string
	var vendirPackagesConfigData string
	var workshopImageName string

	var workshopPortsConfig []composetypes.ServicePortConfig
	var workshopVolumesConfig []composetypes.ServiceVolumeConfig

	var workshopEnvironment []string
	var workshopLabels map[string]string
	var workshopExtraHosts map[string]string

	var workshopComposeProject *composetypes.Project

	if workshopConfigData, err = generateWorkshopConfig(workshop); err != nil {
		return name, err
	}

	if vendirFilesConfigData, err = generateVendirFilesConfig(workshop, originalName, o.LocalRepository, o.WorkshopVersion); err != nil {
		return name, err
	}

	if vendirPackagesConfigData, err = generateVendirPackagesConfig(workshop, originalName, o.LocalRepository, o.WorkshopVersion); err != nil {
		return name, err
	}

	if workshopImageName, err = generateWorkshopImageName(workshop, o.LocalRepository, o.ImageRepository, o.ImageVersion, o.WorkshopImage, o.WorkshopVersion); err != nil {
		return name, err
	}

	if workshopPortsConfig, err = composetypes.ParsePortConfig(fmt.Sprintf("%s:%d:10081", o.Host, o.Port)); err != nil {
		return name, errors.Wrap(err, "unable to generate workshop ports config")
	}

	if workshopVolumesConfig, err = generateWorkshopVolumeMounts(workshop, o.Assets); err != nil {
		return name, err
	}

	if workshopEnvironment, err = generateWorkshopEnvironment(workshop, o.LocalRepository, o.Host, o.Port); err != nil {
		return name, err
	}

	if workshopLabels, err = generateWorkshopLabels(workshop, o.Host, o.Port); err != nil {
		return name, err
	}

	if registryIP != "" {
		if workshopExtraHosts, err = generateWorkshopExtraHosts(workshop, registryIP); err != nil {
			return name, err
		}
	}

	if workshopComposeProject, err = extractWorkshopComposeConfig(workshop); err != nil {
		return name, err
	}

	type TemplateInputs struct {
		WorkshopConfig       string
		VendirFilesConfig    []string
		VendirPackagesConfig string
		KubeConfig           string
		Assets               string
	}

	inputs := TemplateInputs{
		WorkshopConfig:       workshopConfigData,
		VendirFilesConfig:    vendirFilesConfigData,
		VendirPackagesConfig: vendirPackagesConfigData,
		KubeConfig:           kubeConfigData,
		Assets:               o.Assets,
	}

	funcMap := template.FuncMap{
		"inc": func(i int) int {
			return i + 1
		},
	}

	containerScriptTemplate, err := template.New("entrypoint").Funcs(funcMap).Parse(containerScript)

	if err != nil {
		return name, errors.Wrap(err, "not able to parse container script template")
	}

	var containerScriptData bytes.Buffer

	err = containerScriptTemplate.Execute(&containerScriptData, inputs)

	if err != nil {
		return name, errors.Wrap(err, "not able to generate container script")
	}

	networks := map[string]*composetypes.ServiceNetworkConfig{
		"default": {},
	}

	if registryNetwork {
		networks["educates"] = &composetypes.ServiceNetworkConfig{}
	}

	workshopServiceConfig := composetypes.ServiceConfig{
		Name:        "workshop",
		Image:       workshopImageName,
		Command:     composetypes.ShellCommand([]string{"bash", "-c", containerScriptData.String()}),
		User:        "1001:0",
		Ports:       workshopPortsConfig,
		Volumes:     workshopVolumesConfig,
		Environment: composetypes.NewMappingWithEquals(workshopEnvironment),
		Labels:      composetypes.Labels(workshopLabels),
		ExtraHosts:  composetypes.HostsList(workshopExtraHosts),
		DependsOn:   composetypes.DependsOnConfig{},
		Networks:    networks,
	}

	if o.Cluster != "" {
		workshopServiceConfig.Networks["kind"] = &composetypes.ServiceNetworkConfig{}
	}

	dockerEnabled, found, _ := unstructured.NestedBool(workshop.Object, "spec", "session", "applications", "docker", "enabled")

	if found && dockerEnabled {
		extraServices, _, _ := unstructured.NestedMap(workshop.Object, "spec", "session", "applications", "docker", "compose")

		socketEnabledDefault := true

		if len(extraServices) != 0 {
			socketEnabledDefault = false
		}

		socketEnabled, found, _ := unstructured.NestedBool(workshop.Object, "spec", "session", "applications", "docker", "socket", "enabled")

		if !found {
			socketEnabled = socketEnabledDefault
		}

		if socketEnabled {
			workshopServiceConfig.GroupAdd = []string{"docker"}
		}
	}

	workshopServices := []composetypes.ServiceConfig{workshopServiceConfig}

	composeConfig := composetypes.Project{
		Name:     originalName,
		Services: workshopServices,
		Networks: composetypes.Networks{
			"educates": {External: composetypes.External{External: true}},
		},
		Volumes: composetypes.Volumes{
			"workshop": composetypes.VolumeConfig{},
		},
	}

	if workshopComposeProject != nil {
		for _, extraService := range workshopComposeProject.Services {
			extraService.Ports = []composetypes.ServicePortConfig{}

			composeConfig.Services = append(composeConfig.Services, extraService)

			workshopServiceConfig.DependsOn[extraService.Name] = composetypes.ServiceDependency{
				Condition: composetypes.ServiceConditionStarted,
			}
		}

		for volumeName, extraVolume := range workshopComposeProject.Volumes {
			if volumeName != "workshop" {
				composeConfig.Volumes[volumeName] = extraVolume
			}
		}
	}

	if o.Cluster != "" {
		composeConfig.Networks["kind"] = composetypes.NetworkConfig{External: composetypes.External{External: true}}
	}

	composeConfigBytes, err := yaml.Marshal(&composeConfig)

	if err != nil {
		return name, errors.Wrap(err, "failed to generate compose config")
	}

	composeConfigFilePath := path.Join(composeConfigDir, "docker-compose.yaml")

	composeConfigFile, err := os.OpenFile(composeConfigFilePath, os.O_RDWR|os.O_CREATE|os.O_TRUNC, os.ModePerm)

	if err != nil {
		return name, errors.Wrapf(err, "unable to create workshop config file %s", composeConfigFilePath)
	}

	if _, err = composeConfigFile.Write(composeConfigBytes); err != nil {
		return name, errors.Wrapf(err, "unable to write workshop config file %s", composeConfigFilePath)
	}

	if err := composeConfigFile.Close(); err != nil {
		return name, errors.Wrapf(err, "unable to close workshop config file %s", composeConfigFilePath)
	}

	dockerCommand := exec.Command(
		"docker",
		"compose",
		"--project-directory",
		composeConfigDir,
		"--file",
		composeConfigFilePath,
		"--project-name",
		name,
		"up",
		"--detach",
		"--renew-anon-volumes",
	)

	dockerCommand.Stdout = stdout
	dockerCommand.Stderr = stderr

	err = dockerCommand.Run()

	if err != nil {
		return name, errors.Wrap(err, "unable to start workshop")
	}

	return name, nil
}

func (o *DockerWorkshopDeployOptions) Run(cmd *cobra.Command) error {
	dockerWorkshopsManager := NewDockerWorkshopsManager()

	_, err := dockerWorkshopsManager.DeployWorkshop(o, cmd.OutOrStdout(), cmd.OutOrStderr())

	if err != nil {
		return err
	}

	// XXX Need a better way of handling very long startup times for container
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

		switch runtime.GOOS {
		case "linux":
			err = exec.Command("xdg-open", url).Start()
		case "windows":
			err = exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Start()
		case "darwin":
			err = exec.Command("open", url).Start()
		default:
			err = fmt.Errorf("unsupported platform")
		}

		if err != nil {
			return errors.Wrap(err, "unable to open web browser")
		}
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

func generateWorkshopConfig(workshop *unstructured.Unstructured) (string, error) {
	workshopTitle, _, _ := unstructured.NestedFieldNoCopy(workshop.Object, "spec", "title")
	workshopDescription, _, _ := unstructured.NestedFieldNoCopy(workshop.Object, "spec", "description")
	applicationsConfig, _, _ := unstructured.NestedFieldNoCopy(workshop.Object, "spec", "session", "applications")
	ingressesConfig, _, _ := unstructured.NestedSlice(workshop.Object, "spec", "session", "ingresses")
	dashboardsConfig, _, _ := unstructured.NestedSlice(workshop.Object, "spec", "session", "dashboards")

	workshopConfig := map[string]interface{}{
		"spec": map[string]interface{}{
			"title":       workshopTitle,
			"description": workshopDescription,
			"session": map[string]interface{}{
				"applications": applicationsConfig,
				"ingresses":    ingressesConfig,
				"dashboards":   dashboardsConfig,
			},
		},
	}

	workshopConfigData, err := yaml.Marshal(&workshopConfig)

	if err != nil {
		return "", errors.Wrap(err, "failed to generate workshop config")
	}

	return string(workshopConfigData), nil
}

func generateVendirFilesConfig(workshop *unstructured.Unstructured, name string, localRepository string, version string) ([]string, error) {
	var vendirConfigs []string

	workshopVersion, found, _ := unstructured.NestedString(workshop.Object, "spec", "version")

	if !found {
		workshopVersion = version
	}

	filesItems, found, _ := unstructured.NestedSlice(workshop.Object, "spec", "workshop", "files")

	if found && len(filesItems) != 0 {
		for _, filesItem := range filesItems {
			directoriesConfig := []map[string]interface{}{}

			tmpPath, found := filesItem.(map[string]interface{})["path"]

			var filesItemPath string

			if found {
				filesItemPath = tmpPath.(string)
			} else {
				filesItemPath = "."
			}

			filesItemPath = filepath.Clean(path.Join("/opt/assets/files", filesItemPath))

			filesItem.(map[string]interface{})["path"] = "."

			directoriesConfig = append(directoriesConfig, map[string]interface{}{
				"path":     filesItemPath,
				"contents": []interface{}{filesItem},
			})

			vendirConfig := map[string]interface{}{
				"apiVersion":  "vendir.k14s.io/v1alpha1",
				"kind":        "Config",
				"directories": directoriesConfig,
			}

			vendirConfigBytes, err := yaml.Marshal(&vendirConfig)

			if err != nil {
				return []string{}, errors.Wrap(err, "failed to generate vendir config")
			}

			vendirConfigString := string(vendirConfigBytes)

			vendirConfigString = strings.ReplaceAll(vendirConfigString, "$(image_repository)", localRepository)
			vendirConfigString = strings.ReplaceAll(vendirConfigString, "$(workshop_name)", name)
			vendirConfigString = strings.ReplaceAll(vendirConfigString, "$(workshop_version)", workshopVersion)
			vendirConfigString = strings.ReplaceAll(vendirConfigString, "$(platform_arch)", runtime.GOARCH)

			vendirConfigs = append(vendirConfigs, vendirConfigString)
		}
	}

	return vendirConfigs, nil
}

func generateVendirPackagesConfig(workshop *unstructured.Unstructured, name string, localRepository string, version string) (string, error) {
	var vendirConfigString string

	workshopVersion, found, _ := unstructured.NestedString(workshop.Object, "spec", "version")

	if !found {
		workshopVersion = version
	}

	packagesItems, found, _ := unstructured.NestedSlice(workshop.Object, "spec", "workshop", "packages")

	if found && len(packagesItems) != 0 {
		directoriesConfig := []map[string]interface{}{}

		for _, packagesItem := range packagesItems {
			tmpPackagesItem := packagesItem.(map[string]interface{})

			tmpName, found := tmpPackagesItem["name"]

			if !found {
				continue
			}

			packagesItemPath := filepath.Clean(path.Join("/opt/packages", tmpName.(string)))

			tmpPackagesFilesItem := tmpPackagesItem["files"]

			packagesFilesItem := tmpPackagesFilesItem.([]interface{})

			for _, tmpEntry := range packagesFilesItem {
				entry := tmpEntry.(map[string]interface{})

				_, found = entry["path"]

				if !found {
					entry["path"] = "."
				}
			}

			directoriesConfig = append(directoriesConfig, map[string]interface{}{
				"path":     packagesItemPath,
				"contents": packagesFilesItem,
			})

		}

		vendirConfig := map[string]interface{}{
			"apiVersion":  "vendir.k14s.io/v1alpha1",
			"kind":        "Config",
			"directories": directoriesConfig,
		}

		vendirConfigBytes, err := yaml.Marshal(&vendirConfig)

		if err != nil {
			return "", errors.Wrap(err, "failed to generate vendir config")
		}

		vendirConfigString = string(vendirConfigBytes)

		vendirConfigString = strings.ReplaceAll(vendirConfigString, "$(image_repository)", localRepository)
		vendirConfigString = strings.ReplaceAll(vendirConfigString, "$(workshop_name)", name)
		vendirConfigString = strings.ReplaceAll(vendirConfigString, "$(workshop_version)", workshopVersion)
	}

	return vendirConfigString, nil
}

func generateWorkshopImageName(workshop *unstructured.Unstructured, localRepository string, imageRepository string, baseImageVersion string, workshopImage string, workshopVersion string) (string, error) {
	_, found, _ := unstructured.NestedString(workshop.Object, "spec", "version")

	if found {
		workshopVersion, _, _ = unstructured.NestedString(workshop.Object, "spec", "version")
	}

	image, found, err := unstructured.NestedString(workshop.Object, "spec", "workshop", "image")

	if err != nil {
		return "", errors.Wrapf(err, "unable to parse workshop definition")
	}

	if !found || image == "" {
		image = "base-environment:*"
	}

	defaultImageVersion := strings.TrimSpace(baseImageVersion)

	if workshopImage != "" {
		image = workshopImage
	} else {
		if defaultImageVersion == "latest" {
			image = strings.ReplaceAll(image, "base-environment:*", fmt.Sprintf("localhost:5001/educates-base-environment:%s", defaultImageVersion))
			image = strings.ReplaceAll(image, "jdk8-environment:*", fmt.Sprintf("localhost:5001/educates-jdk8-environment:%s", defaultImageVersion))
			image = strings.ReplaceAll(image, "jdk11-environment:*", fmt.Sprintf("localhost:5001/educates-jdk11-environment:%s", defaultImageVersion))
			image = strings.ReplaceAll(image, "jdk17-environment:*", fmt.Sprintf("localhost:5001/educates-jdk17-environment:%s", defaultImageVersion))
			image = strings.ReplaceAll(image, "jdk21-environment:*", fmt.Sprintf("localhost:5001/educates-jdk21-environment:%s", defaultImageVersion))
			image = strings.ReplaceAll(image, "conda-environment:*", fmt.Sprintf("localhost:5001/educates-conda-environment:%s", defaultImageVersion))
		} else {
			image = strings.ReplaceAll(image, "base-environment:*", fmt.Sprintf("%s/educates-base-environment:%s", imageRepository, defaultImageVersion))
			image = strings.ReplaceAll(image, "jdk8-environment:*", fmt.Sprintf("%s/educates-jdk8-environment:%s", imageRepository, defaultImageVersion))
			image = strings.ReplaceAll(image, "jdk11-environment:*", fmt.Sprintf("%s/educates-jdk11-environment:%s", imageRepository, defaultImageVersion))
			image = strings.ReplaceAll(image, "jdk17-environment:*", fmt.Sprintf("%s/educates-jdk17-environment:%s", imageRepository, defaultImageVersion))
			image = strings.ReplaceAll(image, "jdk21-environment:*", fmt.Sprintf("%s/educates-jdk21-environment:%s", imageRepository, defaultImageVersion))
			image = strings.ReplaceAll(image, "conda-environment:*", fmt.Sprintf("%s/educates-conda-environment:%s", imageRepository, defaultImageVersion))
		}
	}

	image = strings.ReplaceAll(image, "$(image_repository)", localRepository)
	image = strings.ReplaceAll(image, "$(workshop_version)", workshopVersion)

	return image, nil
}

func generateWorkshopVolumeMounts(workshop *unstructured.Unstructured, assets string) ([]composetypes.ServiceVolumeConfig, error) {
	filesMounts := []composetypes.ServiceVolumeConfig{
		{
			Type:   "volume",
			Source: "workshop",
			Target: "/home/eduk8s",
		},
	}

	if assets != "" {
		assets = filepath.Clean(assets)
		assets, err := filepath.Abs(assets)

		if err != nil {
			return []composetypes.ServiceVolumeConfig{}, errors.Wrap(err, "can't resolve local workshop assets path")
		}

		filesMounts = append(filesMounts, composetypes.ServiceVolumeConfig{
			Type:     "bind",
			Source:   assets,
			Target:   "/opt/eduk8s/mnt/assets",
			ReadOnly: true,
		})
	}

	dockerEnabled, found, _ := unstructured.NestedBool(workshop.Object, "spec", "session", "applications", "docker", "enabled")

	if found && dockerEnabled {
		extraServices, _, _ := unstructured.NestedMap(workshop.Object, "spec", "session", "applications", "docker", "compose")

		socketEnabledDefault := true

		if len(extraServices) != 0 {
			socketEnabledDefault = false
		}

		socketEnabled, found, _ := unstructured.NestedBool(workshop.Object, "spec", "session", "applications", "docker", "socket", "enabled")

		if !found {
			socketEnabled = socketEnabledDefault
		}

		if socketEnabled {
			if runtime.GOOS == "linux" {
				filesMounts = append(filesMounts, composetypes.ServiceVolumeConfig{
					Type:     "bind",
					Source:   "/var/run/docker.sock",
					Target:   "/var/run/docker/docker.sock",
					ReadOnly: true,
				})
			} else {
				filesMounts = append(filesMounts, composetypes.ServiceVolumeConfig{
					Type:     "bind",
					Source:   "/var/run/docker.sock.raw",
					Target:   "/var/run/docker/docker.sock",
					ReadOnly: true,
				})
			}
		}
	}

	return filesMounts, nil
}

func generateWorkshopEnvironment(workshop *unstructured.Unstructured, localRepository string, host string, port uint) ([]string, error) {
	domain := fmt.Sprintf("%s.nip.io", strings.ReplaceAll(host, ".", "-"))

	return []string{
		fmt.Sprintf("WORKSHOP_NAME=%s", workshop.GetName()),
		"SESSION_NAME=workshop",
		fmt.Sprintf("SESSION_URL=http://workshop.%s:%d", domain, port),
		"INGRESS_PROTOCOL=http",
		fmt.Sprintf("INGRESS_DOMAIN=%s", domain),
		fmt.Sprintf("INGRESS_PORT_SUFFIX=:%d", port),
		fmt.Sprintf("IMAGE_REPOSITORY=%s", localRepository),
	}, nil
}

func generateWorkshopLabels(workshop *unstructured.Unstructured, host string, port uint) (map[string]string, error) {
	labels := workshop.GetAnnotations()

	domain := fmt.Sprintf("%s.nip.io", strings.ReplaceAll(host, ".", "-"))

	labels["training.educates.dev/url"] = fmt.Sprintf("http://workshop.%s:%d", domain, port)
	labels["training.educates.dev/session"] = workshop.GetName()

	return labels, nil
}

func generateWorkshopExtraHosts(workshop *unstructured.Unstructured, registryIP string) (map[string]string, error) {
	hosts := map[string]string{}

	if registryIP != "" {
		hosts["registry.docker.local"] = registryIP
	}

	return hosts, nil
}

func extractWorkshopComposeConfig(workshop *unstructured.Unstructured) (*composetypes.Project, error) {
	composeConfigObj, found, _ := unstructured.NestedMap(workshop.Object, "spec", "session", "applications", "docker", "compose")

	if found {
		composeConfigObjBytes, err := yaml.Marshal(&composeConfigObj)

		if err != nil {
			return nil, errors.Wrap(err, "unable to parse workshop docker compose config")
		}

		configFiles := composetypes.ConfigFile{
			Content: composeConfigObjBytes,
		}

		composeConfigDetails := composetypes.ConfigDetails{
			ConfigFiles: []composetypes.ConfigFile{configFiles},
		}

		return composeloader.Load(composeConfigDetails, func(options *composeloader.Options) {
			options.SkipConsistencyCheck = true
			options.SkipNormalization = true
			options.ResolvePaths = false
		})
	}

	return nil, nil
}

func generateClusterKubeconfig(name string) (string, error) {
	provider := cluster.NewProvider(
		cluster.ProviderWithLogger(cmd.NewLogger()),
	)

	clusters, err := provider.List()

	if err != nil {
		return "", errors.Wrap(err, "unable to get list of clusters")
	}

	if !slices.Contains(clusters, name) {
		return "", errors.Errorf("cluster %s doesn't exist", name)
	}

	file, err := os.CreateTemp("", "kubeconfig-")

	if err != nil {
		return "", errors.Wrap(err, "unable to generate kubeconfig file")
	}

	defer os.Remove(file.Name())

	err = provider.ExportKubeConfig(name, file.Name(), true)

	if err != nil {
		return "", errors.Wrap(err, "unable to generate kubeconfig file")
	}

	kubeConfigData, err := os.ReadFile(file.Name())

	if err != nil {
		return "", errors.Wrap(err, "unable to generate kubeconfig file")
	}

	return string(kubeConfigData), nil
}
