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
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/volume"
	"github.com/docker/docker/client"
	"github.com/educates/educates-training-platform/client-programs/pkg/constants"
	"github.com/educates/educates-training-platform/client-programs/pkg/educates"
	"github.com/educates/educates-training-platform/client-programs/pkg/utils"
	"github.com/pkg/errors"
	"go.yaml.in/yaml/v2"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/kind/pkg/cluster"
	"sigs.k8s.io/kind/pkg/cmd"
)

const (
	// Workshop status constants
	WorkshopStatusStarting = "Starting"
	WorkshopStatusRunning  = "Running"
	WorkshopStatusStopping = "Stopping"
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

var (
	containerScriptTemplateOnce   sync.Once
	containerScriptTemplateCached *template.Template
	containerScriptTemplateErr    error
)

type DockerWorkshopsManager struct {
	Statuses         map[string]DockerWorkshopDetails
	StatusesMutex    sync.Mutex
	composeService   api.Compose
	composeServiceMu sync.Mutex
	dockerClient     *client.Client
	dockerClientMu   sync.RWMutex
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
	ctx := context.Background()

	cli, err := m.GetDockerClient()
	if err != nil {
		return nil, err
	}

	containers, err := cli.ContainerList(ctx, container.ListOptions{Filters: getWorkshopContainerLabelFilters()})
	if err != nil {
		return nil, errors.Wrap(err, "unable to list Educates workshop containers")
	}

	// Copy statuses while holding lock briefly
	m.StatusesMutex.Lock()
	setOfWorkshops := make(map[string]DockerWorkshopDetails, len(m.Statuses))
	for _, details := range m.Statuses {
		if details.Status == WorkshopStatusStarting {
			setOfWorkshops[details.Name] = details
		}
	}
	statusesCopy := make(map[string]DockerWorkshopDetails, len(m.Statuses))
	for k, v := range m.Statuses {
		statusesCopy[k] = v
	}
	m.StatusesMutex.Unlock()

	for _, ctr := range containers {
		url, found := ctr.Labels[constants.EducatesWorkshopLabelAnnotationURL]
		source := ctr.Labels[constants.EducatesWorkshopLabelAnnotationSource]
		instance := ctr.Labels[constants.EducatesWorkshopLabelAnnotationSession]

		status := WorkshopStatusRunning
		if details, statusFound := statusesCopy[instance]; statusFound {
			status = details.Status
		}

		if found && url != "" && len(ctr.Names) != 0 {
			setOfWorkshops[instance] = DockerWorkshopDetails{
				Name:   instance,
				Url:    url,
				Source: source,
				Status: status,
			}
		}
	}

	workshopsList := make([]DockerWorkshopDetails, 0, len(setOfWorkshops))
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

// GetDockerClient returns a Docker client instance, initializing it if necessary.
// It uses a singleton pattern to reuse the same client instance across operations.
func (m *DockerWorkshopsManager) GetDockerClient() (*client.Client, error) {
	// Try read lock first for fast path
	m.dockerClientMu.RLock()
	if m.dockerClient != nil {
		defer m.dockerClientMu.RUnlock()
		return m.dockerClient, nil
	}
	m.dockerClientMu.RUnlock()

	// Acquire write lock to initialize
	m.dockerClientMu.Lock()
	defer m.dockerClientMu.Unlock()

	// Double-check after acquiring write lock
	if m.dockerClient != nil {
		return m.dockerClient, nil
	}

	cli, err := utils.NewDockerClient()
	if err != nil {
		return nil, errors.Wrap(err, "unable to create docker client")
	}

	m.dockerClient = cli
	return cli, nil
}

// getContainerScriptTemplate returns the cached container script template.
func getContainerScriptTemplate() (*template.Template, error) {
	containerScriptTemplateOnce.Do(func() {
		funcMap := template.FuncMap{
			"inc": func(i int) int { return i + 1 },
		}
		containerScriptTemplateCached, containerScriptTemplateErr =
			template.New("entrypoint").Funcs(funcMap).Parse(containerScript)
	})
	return containerScriptTemplateCached, containerScriptTemplateErr
}


// isDockerSocketEnabled checks if Docker socket is enabled in the workshop spec.
func isDockerSocketEnabled(workshop *unstructured.Unstructured) bool {
	dockerEnabled, found, _ := unstructured.NestedBool(
		workshop.Object, "spec", "session", "applications", "docker", "enabled")
	if !found || !dockerEnabled {
		return false
	}

	extraServices, _, _ := unstructured.NestedMap(
		workshop.Object, "spec", "session", "applications", "docker", "compose")

	socketEnabledDefault := len(extraServices) == 0
	socketEnabled, found, _ := unstructured.NestedBool(
		workshop.Object, "spec", "session", "applications", "docker", "socket", "enabled")

	if !found {
		return socketEnabledDefault
	}
	return socketEnabled
}

// applyWorkshopVariables replaces workshop-related variables in a string efficiently.
func applyWorkshopVariables(content, name, localRepository, version string) string {
	replacer := strings.NewReplacer(
		"$(image_repository)", localRepository,
		"$(workshop_name)", name,
		"$(workshop_version)", version,
		"$(platform_arch)", runtime.GOARCH,
	)
	return replacer.Replace(content)
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

	definitionConfig := educates.WorkshopDefinitionConfig{
		Name: "",
		Path: o.Path,
		Portal: constants.DefaultPortalName,
		WorkshopFile: o.WorkshopFile,
		WorkshopVersion: o.WorkshopVersion,
		DataValueFlags: o.DataValuesFlags,
	}
	if workshop, err = educates.LoadWorkshopDefinition(&definitionConfig); err != nil {
		return "", err
	}

	name := workshop.GetName()

	m.SetWorkshopStatus(name, "", o.Path, WorkshopStatusStarting)

	defer m.ClearWorkshopStatus(name)

	originalName := workshop.GetAnnotations()[constants.EducatesWorkshopLabelAnnotationWorkshop]

	configFileDir := utils.GetEducatesHomeDir()
	composeConfigDir := path.Join(configFileDir, "compose", name)

	err = os.MkdirAll(composeConfigDir, os.ModePerm)

	if err != nil {
		return name, errors.Wrapf(err, "unable to create workshops compose directory")
	}

	ctx := context.Background()

	cli, err := m.GetDockerClient()
	if err != nil {
		return name, err
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

	containerScriptTemplate, err := getContainerScriptTemplate()
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

	if isDockerSocketEnabled(workshop) {
		workshopServiceConfig.GroupAdd = []string{"docker"}
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
			// TODO: Maybe modify extraService.Ports to add the host IP
			composeConfig.Services[serviceName] = extraService

			workshopServiceConfig.DependsOn[serviceName] = composetypes.ServiceDependency{
				Condition: composetypes.ServiceConditionHealthy,
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
	m.SetWorkshopStatus(name, "", "", WorkshopStatusStopping)

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

	cli, err := m.GetDockerClient()
	if err != nil {
		return err
	}

	// List volumes that match the workshop name pattern and remove them
	filters := filters.NewArgs()
	filters.Add("name", fmt.Sprintf("%s_workshop", name))
	volumesListResponse, err := cli.VolumeList(ctx, volume.ListOptions{Filters: filters})
	if err != nil {
		return errors.Wrap(err, "unable to list workshop volumes")
	}

	for _, volume := range volumesListResponse.Volumes {
		if err := cli.VolumeRemove(ctx, volume.Name, false); err != nil {
			return errors.Wrap(err, "unable to delete workshop volume")
		}
	}
	workshopConfigDir := path.Join(configFileDir, "workshops", name)

	if err := os.RemoveAll(workshopConfigDir); err != nil {
		fmt.Fprintf(stderr, "Warning: failed to remove workshop config dir: %v\n", err)
	}
	if err := os.RemoveAll(composeConfigDir); err != nil {
		fmt.Fprintf(stderr, "Warning: failed to remove compose config dir: %v\n", err)
	}

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
	if !found || len(filesItems) == 0 {
		return vendirConfigs, nil
	}

	for _, filesItem := range filesItems {
		filesMap, ok := filesItem.(map[string]interface{})
		if !ok {
			continue
		}

		directoriesConfig := []map[string]interface{}{}

		var filesItemPath string
		if tmpPath, found := filesMap["path"]; found {
			if pathStr, ok := tmpPath.(string); ok {
				filesItemPath = pathStr
			} else {
				filesItemPath = "."
			}
		} else {
			filesItemPath = "."
		}

		filesItemPath = filepath.Clean(path.Join("/opt/assets/files", filesItemPath))
		filesMap["path"] = "."

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

		vendirConfigString := applyWorkshopVariables(string(vendirConfigBytes), name, localRepository, workshopVersion)
		vendirConfigs = append(vendirConfigs, vendirConfigString)
	}

	return vendirConfigs, nil
}

func generateVendirPackagesConfig(workshop *unstructured.Unstructured, name string, localRepository string, version string) (string, error) {
	workshopVersion, found, _ := unstructured.NestedString(workshop.Object, "spec", "version")
	if !found {
		workshopVersion = version
	}

	packagesItems, found, _ := unstructured.NestedSlice(workshop.Object, "spec", "workshop", "packages")
	if !found || len(packagesItems) == 0 {
		return "", nil
	}

	directoriesConfig := []map[string]interface{}{}

	for _, packagesItem := range packagesItems {
		tmpPackagesItem, ok := packagesItem.(map[string]interface{})
		if !ok {
			continue
		}

		tmpName, found := tmpPackagesItem["name"]
		if !found {
			continue
		}

		nameStr, ok := tmpName.(string)
		if !ok {
			continue
		}

		packagesItemPath := filepath.Clean(path.Join("/opt/packages", nameStr))

		tmpPackagesFilesItem, found := tmpPackagesItem["files"]
		if !found {
			continue
		}

		packagesFilesItem, ok := tmpPackagesFilesItem.([]interface{})
		if !ok {
			continue
		}

		for _, tmpEntry := range packagesFilesItem {
			if entry, ok := tmpEntry.(map[string]interface{}); ok {
				if _, found := entry["path"]; !found {
					entry["path"] = "."
				}
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

	return applyWorkshopVariables(string(vendirConfigBytes), name, localRepository, workshopVersion), nil
}

func generateWorkshopImageName(workshop *unstructured.Unstructured, localRepository string, imageRepository string, baseImageVersion string, workshopImage string, workshopVersion string) (string, error) {
	if version, found, _ := unstructured.NestedString(workshop.Object, "spec", "version"); found {
		workshopVersion = version
	}

	image, found, err := unstructured.NestedString(workshop.Object, "spec", "workshop", "image")
	if err != nil {
		return "", errors.Wrapf(err, "unable to parse workshop definition")
	}

	if !found || image == "" {
		image = "base-environment:*"
	}

	if workshopImage != "" {
		return workshopImage, nil
	}

	defaultImageVersion := strings.TrimSpace(baseImageVersion)

	// Map of environment placeholders to their image names
	imageMap := map[string]string{
		"base-environment:*":  "educates-base-environment",
		"jdk8-environment:*":  "educates-jdk8-environment",
		"jdk11-environment:*": "educates-jdk11-environment",
		"jdk17-environment:*": "educates-jdk17-environment",
		"jdk21-environment:*": "educates-jdk21-environment",
		"conda-environment:*": "educates-conda-environment",
	}

	repo := imageRepository
	if defaultImageVersion == "latest" {
		repo = "localhost:5001"
	}

	for placeholder, imageName := range imageMap {
		replacement := fmt.Sprintf("%s/%s:%s", repo, imageName, defaultImageVersion)
		image = strings.ReplaceAll(image, placeholder, replacement)
	}

	return applyWorkshopVariables(image, "", localRepository, workshopVersion), nil
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
		absAssets, err := filepath.Abs(assets)
		if err != nil {
			return []composetypes.ServiceVolumeConfig{}, errors.Wrap(err, "can't resolve local workshop assets path")
		}

		filesMounts = append(filesMounts, composetypes.ServiceVolumeConfig{
			Type:     "bind",
			Source:   absAssets,
			Target:   "/opt/eduk8s/mnt/assets",
			ReadOnly: true,
		})
	}

	if isDockerSocketEnabled(workshop) {
		dockerSocketSource := "/var/run/docker.sock"
		if runtime.GOOS != "linux" {
			dockerSocketSource = "/var/run/docker.sock.raw"
		}

		filesMounts = append(filesMounts, composetypes.ServiceVolumeConfig{
			Type:     "bind",
			Source:   dockerSocketSource,
			Target:   "/var/run/docker/docker.sock",
			ReadOnly: true,
		})
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

	labels[constants.EducatesContainersAppLabelKey] = constants.EducatesContainersAppLabel
	labels[constants.EducatesContainersRoleLabelKey] = constants.EducatesContainersWorkshopRoleLabel
	labels[constants.EducatesWorkshopLabelAnnotationURL] = fmt.Sprintf("http://workshop.%s:%d", domain, port)
	labels[constants.EducatesWorkshopLabelAnnotationSession] = workshop.GetName()

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

func getWorkshopContainerLabelFilters() filters.Args {
	return filters.NewArgs(
		filters.Arg("label", constants.EducatesContainersAppLabelKey+"="+constants.EducatesContainersAppLabel),
		filters.Arg("label", constants.EducatesContainersRoleLabelKey+"="+constants.EducatesContainersWorkshopRoleLabel),
	)
}
