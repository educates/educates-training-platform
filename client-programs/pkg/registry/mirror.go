package registry

import (
	"context"
	_ "embed"
	"fmt"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"
	"github.com/educates/educates-training-platform/client-programs/pkg/config"
	"github.com/educates/educates-training-platform/client-programs/pkg/constants"
	"github.com/educates/educates-training-platform/client-programs/pkg/utils"
	"github.com/pkg/errors"
)

const hostMirrorTomlTemplate = `[host."http://%s:5000"]
  capabilities = ["pull", "resolve"]
`

// Mirror represents a registry mirror container.
type Mirror struct {
	baseContainer
	config *config.RegistryMirrorConfig
}

// NewMirror creates a new Mirror instance.
func NewMirror(mirrorConfig *config.RegistryMirrorConfig) *Mirror {
	return &Mirror{
		baseContainer: baseContainer{
			containerName: fmt.Sprintf("%s-mirror-%s", constants.EducatesRegistryContainer, mirrorConfig.Mirror),
			bindIP:        "127.0.0.1",
			hostPort:      "", // dynamic port
			labels: newMirrorContainerLabels(mirrorConfig),
			envVars: buildMirrorEnvVars(mirrorConfig),
		},
		config: mirrorConfig,
	}
}

// buildMirrorEnvVars creates the environment variables for a mirror container.
func buildMirrorEnvVars(mirrorConfig *config.RegistryMirrorConfig) []string {
	envs := []string{}
	mirrorURL := mirrorConfig.URL
	if mirrorURL == "" {
		mirrorURL = mirrorConfig.Mirror
	}
	envs = append(envs, fmt.Sprintf("REGISTRY_PROXY_REMOTEURL=https://%s", mirrorURL))
	if mirrorConfig.Username != "" {
		envs = append(envs, fmt.Sprintf("REGISTRY_PROXY_USERNAME=%s", mirrorConfig.Username))
	}
	if mirrorConfig.Password != "" {
		envs = append(envs, fmt.Sprintf("REGISTRY_PROXY_PASSWORD=%s", mirrorConfig.Password))
	}
	return envs
}

// DeployAndLinkToCluster deploys a registry mirror and links it to the cluster.
func (m *Mirror) DeployAndLinkToCluster() error {
	err := m.Deploy()
	if err != nil {
		return errors.Wrap(err, "failed to deploy registry mirror "+m.config.Mirror)
	}

	content := fmt.Sprintf(hostMirrorTomlTemplate, m.containerName)
	err = addRegistryConfigToKindNodes(m.config.Mirror, content)
	if err != nil {
		fmt.Println("Warning: Mirror not added to Kind nodes")
	}

	return nil
}

// Deploy creates the mirror container.
func (m *Mirror) Deploy() error {
	fmt.Printf("Deploying local image registry mirror %s\n", m.config.Mirror)

	cli, err := client.NewClientWithOpts(client.FromEnv)
	if err != nil {
		return errors.Wrap(err, "unable to create docker client")
	}

	exists, _ := m.containerExists(cli)
	if exists {
		// If we can retrieve a container of required name we assume it is
		// running okay. Technically it could be restarting, stopping or
		// have exited and container was not removed, but if that is the case
		// then leave it up to the user to sort out.
		fmt.Printf("Registry mirror %s already exists\n", m.config.Mirror)
		return nil
	}

	if err = m.ensureNetwork(cli, constants.EducatesNetworkName); err != nil {
		return err
	}

	if _, err = m.createAndStartContainer(cli); err != nil {
		return errors.Wrap(err, "cannot create local registry mirror container")
	}

	if err = m.connectToNetwork(cli, constants.EducatesNetworkName); err != nil {
		return errors.Wrap(err, fmt.Sprintf("unable to connect local registry mirror to %s network", constants.EducatesNetworkName))
	}

	if err = m.linkToNetwork(cli, constants.ClusterNetworkName); err != nil {
		return errors.Wrap(err, fmt.Sprintf("failed to link local registry mirror to %s network", constants.ClusterNetworkName))
	}

	return nil
}

// DeleteAndUnlinkFromCluster deletes a local registry mirror and unlinks it from the cluster.
func (m *Mirror) DeleteAndUnlinkFromCluster() error {
	fmt.Printf("Deleting local image registry mirror %s\n", m.config.Mirror)

	cli, err := client.NewClientWithOpts(client.FromEnv)
	if err != nil {
		return errors.Wrap(err, "unable to create docker client")
	}

	exists, _ := m.containerExists(cli)
	if !exists {
		fmt.Printf("Registry mirror %s does not exist\n", m.config.Mirror)
		return nil
	}

	err = m.stopAndRemoveContainer(cli)
	if err != nil {
		return err
	}

	// Remove the registry config from the kind nodes
	err = removeRegistryConfigFromKindNodes(m.config.Mirror)
	if err != nil {
		return errors.Wrap(err, "unable to remove registry config from kind nodes")
	}

	return nil
}

// Delete removes the mirror container.
func (m *Mirror) Delete() error {
	fmt.Printf("Deleting local image registry mirror %s\n", m.config.Mirror)

	cli, err := client.NewClientWithOpts(client.FromEnv)
	if err != nil {
		return errors.Wrap(err, "unable to create docker client")
	}

	return m.stopAndRemoveContainer(cli)
}

// DeleteRegistryMirrors deletes all local image registry mirrors.
func DeleteRegistryMirrors() error {
	ctx := context.Background()

	fmt.Println("Deleting local image registry mirrors")

	cli, err := client.NewClientWithOpts(client.FromEnv)
	if err != nil {
		return errors.Wrap(err, "unable to create docker client")
	}

	mirrors, err := cli.ContainerList(ctx, container.ListOptions{
		Filters: getRegistryMirrorLabelFilters(),
	})
	if err != nil {
		return errors.Wrap(err, "unable to list registry mirrors")
	}

	for _, mirror := range mirrors {
		timeout := 30

		err = cli.ContainerStop(ctx, mirror.ID, container.StopOptions{Timeout: &timeout})
		if err != nil {
			return errors.Wrap(err, "unable to stop registry mirror container "+mirror.ID)
		}

		err = cli.ContainerRemove(ctx, mirror.ID, container.RemoveOptions{})
		if err != nil {
			return errors.Wrap(err, "unable to delete registry mirror container "+mirror.ID)
		}
	}

	return nil
}

// ListRegistryMirrors lists all local image registry mirrors.
func ListRegistryMirrors() (string, error) {
	ctx := context.Background()

	cli, err := client.NewClientWithOpts(client.FromEnv)
	if err != nil {
		return "", errors.Wrap(err, "unable to create docker client")
	}

	mirrors, err := cli.ContainerList(ctx, container.ListOptions{
		Filters: getRegistryMirrorLabelFilters(),
	})
	if err != nil {
		return "", errors.Wrap(err, "unable to list registry mirrors")
	}

	var data [][]string
	for _, item := range mirrors {
		name := item.Labels["mirror"]
		url := item.Labels["url"]
		if url == "" {
			url = item.Labels["mirror"]
		}
		username := item.Labels["username"]
		status := item.Status
		containerName := utils.GetContainerName(item)
		data = append(data, []string{name, url, username, status, containerName})
	}
	return utils.PrintTable([]string{"NAME", "URL", "USERNAME", "STATUS", "CONTAINER_NAME"}, data), nil
}

// Compile-time check that Mirror implements ContainerManager
var _ ContainerManager = (*Mirror)(nil)
