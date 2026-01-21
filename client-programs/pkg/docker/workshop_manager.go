package docker

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"text/template"

	yttcmd "carvel.dev/ytt/pkg/cmd/template"
	composeloader "github.com/compose-spec/compose-go/v2/loader"
	composetypes "github.com/compose-spec/compose-go/v2/types"
	"github.com/docker/cli/cli/command"
	"github.com/docker/cli/cli/flags"
	"github.com/docker/compose/v5/pkg/api"
	"github.com/docker/compose/v5/pkg/compose"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"
	"github.com/educates/educates-training-platform/client-programs/pkg/constants"
	eduk8sWorkshops "github.com/educates/educates-training-platform/client-programs/pkg/educates/resources/workshops"
	"github.com/educates/educates-training-platform/client-programs/pkg/utils"
	"github.com/pkg/errors"
	"go.yaml.in/yaml/v2"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/kind/pkg/cluster"
	"sigs.k8s.io/kind/pkg/cmd"
)

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

type DockerWorkshopsManager struct {
	Statuses         map[string]DockerWorkshopDetails
	StatusesMutex    sync.Mutex
	composeService   api.Compose
	composeServiceMu sync.Mutex
}

func NewDockerWorkshopsManager() DockerWorkshopsManager {
	return DockerWorkshopsManager{
		Statuses:      map[string]DockerWorkshopDetails{},
		StatusesMutex: sync.Mutex{},
	}
}

type DockerWorkshopDetails struct {
	Name   string `json:"name"`
	Url    string `json:"url,omitempty"`
	Source string `json:"source,omitempty"`
	Status string `json:"status"`
}

type DockerWorkshopDeployConfig struct {
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


func (m *DockerWorkshopsManager) WorkshopStatus(name string) (DockerWorkshopDetails, bool) {
	workshops, err := m.ListWorkshops()

	if err != nil {
		return DockerWorkshopDetails{}, false
	}

	for _, workshop := range workshops {
		if workshop.Name == name {
			return workshop, true
		}
	}

	return DockerWorkshopDetails{}, false
}

func (m *DockerWorkshopsManager) SetWorkshopStatus(name string, url string, source string, status string) {
	m.StatusesMutex.Lock()

	m.Statuses[name] = DockerWorkshopDetails{
		Name:   name,
		Url:    url,
		Source: source,
		Status: status,
	}

	m.StatusesMutex.Unlock()
}

func (m *DockerWorkshopsManager) ClearWorkshopStatus(name string) {
	m.StatusesMutex.Lock()

	delete(m.Statuses, name)

	m.StatusesMutex.Unlock()
}

func (m *DockerWorkshopsManager) ListWorkshops() ([]DockerWorkshopDetails, error) {
	setOfWorkshops := map[string]DockerWorkshopDetails{}
	workshopsList := []DockerWorkshopDetails{}

	ctx := context.Background()

	cli, err := client.NewClientWithOpts(client.FromEnv)

	if err != nil {
		return nil, errors.Wrap(err, "unable to create docker client")
	}

	containers, err := cli.ContainerList(ctx, container.ListOptions{})

	if err != nil {
		return nil, errors.Wrap(err, "unable to list containers")
	}

	m.StatusesMutex.Lock()

	for _, details := range m.Statuses {
		if details.Status == "Starting" {
			setOfWorkshops[details.Name] = details
		}
	}

	defer m.StatusesMutex.Unlock()

	for _, container := range containers {
		url, found := container.Labels["training.educates.dev/url"]
		source := container.Labels["training.educates.dev/source"]
		instance := container.Labels["training.educates.dev/session"]

		details, statusFound := m.Statuses[instance]

		status := "Running"

		if statusFound {
			status = details.Status
		}

		if found && url != "" && len(container.Names) != 0 {
			setOfWorkshops[instance] = DockerWorkshopDetails{
				Name:   instance,
				Url:    url,
				Source: source,
				Status: status,
			}
		}
	}

	for _, details := range setOfWorkshops {
		workshopsList = append(workshopsList, details)
	}

	return workshopsList, nil
}

// GetComposeService returns a ComposeService instance, initializing it if necessary.
// It uses a singleton pattern to reuse the same service instance across operations.
func (m *DockerWorkshopsManager) GetComposeService(stdout io.Writer, stderr io.Writer) (api.Compose, error) {
	m.composeServiceMu.Lock()
	defer m.composeServiceMu.Unlock()

	if m.composeService != nil {
		return m.composeService, nil
	}

	dockerCLI, err := command.NewDockerCli()
	if err != nil {
		return nil, errors.Wrap(err, "unable to create docker CLI")
	}

	err = dockerCLI.Initialize(&flags.ClientOptions{})
	if err != nil {
		return nil, errors.Wrap(err, "unable to initialize docker CLI")
	}

	// Create ComposeService with options for I/O redirection and non-interactive mode
	service, err := compose.NewComposeService(
		dockerCLI,
		compose.WithOutputStream(stdout),
		compose.WithErrorStream(stderr),
		compose.WithPrompt(compose.AlwaysOkPrompt()),
		compose.WithMaxConcurrency(4),
	)
	if err != nil {
		return nil, errors.Wrap(err, "unable to create compose service")
	}

	m.composeService = service
	return service, nil
}


func (m *DockerWorkshopsManager) DeployWorkshop(o *DockerWorkshopDeployConfig, stdout io.Writer, stderr io.Writer) (string, error) {
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

	definitionConfig := eduk8sWorkshops.WorkshopDefinitionConfig{
		Name: "",
		Path: o.Path,
		Portal: constants.DefaultPortalName,
		WorkshopFile: o.WorkshopFile,
		WorkshopVersion: o.WorkshopVersion,
		DataValueFlags: o.DataValuesFlags,
	}
	if workshop, err = eduk8sWorkshops.LoadWorkshopDefinition(&definitionConfig); err != nil {
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

	cli, err := client.NewClientWithOpts(client.FromEnv)

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

	registryInfo, err := cli.ContainerInspect(ctx, constants.EducatesRegistryContainer)

	if err == nil {
		educatesNetwork, exists := registryInfo.NetworkSettings.Networks[constants.EducatesNetworkName]

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
		networks[constants.EducatesNetworkName] = &composetypes.ServiceNetworkConfig{}
	}

	var extraHostsList composetypes.HostsList
	if len(workshopExtraHosts) > 0 {
		extraHostsList = make(composetypes.HostsList, len(workshopExtraHosts))
		for hostname, ip := range workshopExtraHosts {
			extraHostsList[hostname] = []string{ip}
		}
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
		ExtraHosts:  extraHostsList,
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

	workshopServices := composetypes.Services{
		"workshop": workshopServiceConfig,
	}

	composeConfig := composetypes.Project{
		Name:     originalName,
		Services: workshopServices,
		Networks: composetypes.Networks{
			"educates": composetypes.NetworkConfig{Name: constants.EducatesNetworkName, External: true},
		},
		Volumes: composetypes.Volumes{
			"workshop": composetypes.VolumeConfig{},
		},
	}

	if workshopComposeProject != nil {
		for serviceName, extraService := range workshopComposeProject.Services {
			extraService.Ports = []composetypes.ServicePortConfig{}

			composeConfig.Services[serviceName] = extraService

			workshopServiceConfig.DependsOn[serviceName] = composetypes.ServiceDependency{
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
		composeConfig.Networks["kind"] = composetypes.NetworkConfig{Name: "kind", External: true}
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

	// Get ComposeService instance
	service, err := m.GetComposeService(stdout, stderr)
	if err != nil {
		return name, errors.Wrap(err, "unable to get compose service")
	}

	// Load the project from the compose file
	project, err := service.LoadProject(ctx, api.ProjectLoadOptions{
		ConfigPaths: []string{composeConfigFilePath},
		ProjectName: name,
	})
	if err != nil {
		return name, errors.Wrap(err, "failed to load project")
	}

	// Start the services using SDK
	err = service.Up(ctx, project, api.UpOptions{
		Create: api.CreateOptions{
			Recreate:             api.RecreateDiverged,
			RecreateDependencies: api.RecreateDiverged,
			RemoveOrphans:       false,
		},
		Start: api.StartOptions{},
	})
	if err != nil {
		return name, errors.Wrap(err, "unable to start workshop")
	}

	return name, nil
}

func (m *DockerWorkshopsManager) DeleteWorkshop(name string, stdout io.Writer, stderr io.Writer) error {
	m.SetWorkshopStatus(name, "", "", "Stopping")

	defer m.ClearWorkshopStatus(name)

	ctx := context.Background()

	// Get ComposeService instance
	service, err := m.GetComposeService(stdout, stderr)
	if err != nil {
		return errors.Wrap(err, "unable to get compose service")
	}

	// Load the project to get the project name
	configFileDir := utils.GetEducatesHomeDir()
	composeConfigDir := path.Join(configFileDir, "compose", name)
	composeConfigFilePath := path.Join(composeConfigDir, "docker-compose.yaml")

	// Try to load project, but if file doesn't exist, just use the name
	project, err := service.LoadProject(ctx, api.ProjectLoadOptions{
		ConfigPaths: []string{composeConfigFilePath},
		ProjectName: name,
	})
	if err != nil {
		// If project can't be loaded, still try to remove by name
		project = nil
	}

	projectName := name
	if project != nil {
		projectName = project.Name
	}

	// Stop and remove services using SDK
	err = service.Down(ctx, projectName, api.DownOptions{
		RemoveOrphans: true,
		Volumes:       true,
	})
	if err != nil {
		return errors.Wrap(err, "unable to stop workshop")
	}

	cli, err2 := client.NewClientWithOpts(client.FromEnv)

	if err2 != nil {
		return errors.Wrap(err2, "unable to create docker client")
	}

	err2 = cli.VolumeRemove(ctx, fmt.Sprintf("%s_workshop", name), false)

	if err2 != nil {
		return errors.Wrap(err2, "unable to delete workshop volume")
	}

	workshopConfigDir := path.Join(configFileDir, "workshops", name)

	os.RemoveAll(workshopConfigDir)
	os.RemoveAll(composeConfigDir)

	return nil
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

		return composeloader.LoadWithContext(context.Background(), composeConfigDetails, func(options *composeloader.Options) {
			options.SkipConsistencyCheck = true
			options.SkipNormalization = true
			options.ResolvePaths = false
			options.SkipValidation = true
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

