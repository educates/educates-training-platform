package registry

import (
	"context"
	_ "embed"
	"fmt"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/client"
	"github.com/docker/go-connections/nat"
	"github.com/educates/educates-training-platform/client-programs/pkg/config"
	"github.com/educates/educates-training-platform/client-programs/pkg/constants"
	"github.com/educates/educates-training-platform/client-programs/pkg/utils"
	"github.com/pkg/errors"
)

const hostMirrorTomlTemplate = `[host."http://%s:5000"]
  capabilities = ["pull", "resolve"]
`

/**
 * This function is used to deploy a registry mirror and link it to the cluster.
 * It is used when creating a new local registry mirror.
 */
func DeployMirrorAndLinkToCluster(mirrorConfig *config.RegistryMirrorConfig) error {
	err := createMirrorContainer(mirrorConfig)

	if err != nil {
		return errors.Wrap(err, "failed to deploy registry mirror "+mirrorConfig.Mirror)
	}

	content := fmt.Sprintf(hostMirrorTomlTemplate, registryMirrorContainerName(mirrorConfig))
	err = addRegistryConfigToKindNodes(mirrorConfig.Mirror, content)

	if err != nil {
		fmt.Println("Warning: Mirror not added to Kind nodes")
	}

	return nil
}

/**
 * This private function only creates the registry mirror container.
 */
func createMirrorContainer(mirrorConfig *config.RegistryMirrorConfig) error {
	ctx := context.Background()

	fmt.Printf("Deploying local image registry mirror %s\n", mirrorConfig.Mirror)

	cli, err := client.NewClientWithOpts(client.FromEnv)

	if err != nil {
		return errors.Wrap(err, "unable to create docker client")
	}

	mirrorContainerName := registryMirrorContainerName(mirrorConfig)
	_, err = cli.ContainerInspect(ctx, mirrorContainerName)

	if err == nil {
		// If we can retrieve a container of required name we assume it is
		// running okay. Technically it could be restarting, stopping or
		// have exited and container was not removed, but if that is the case
		// then leave it up to the user to sort out.
		fmt.Printf("Registry mirror %s already exists\n", mirrorConfig.Mirror)

		return nil
	}

	// Prepare environment variables for the registry mirror
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

	_, err = cli.NetworkInspect(ctx, constants.EducatesNetworkName, network.InspectOptions{})

	if err != nil {
		_, err = cli.NetworkCreate(ctx, constants.EducatesNetworkName, network.CreateOptions{})

		if err != nil {
			return errors.Wrap(err, "cannot create educates network")
		}
	}

	hostConfig := &container.HostConfig{
		PortBindings: nat.PortMap{
			"5000/tcp": []nat.PortBinding{
				{
					HostIP: "127.0.0.1",
					// HostPort: mirrorConfig.Port,
				},
			},
		},
		RestartPolicy: container.RestartPolicy{
			Name: "always",
		},
	}

	labels := map[string]string{
		"app":    constants.EducatesAppLabel,
		"role":   constants.EducatesMirrorRoleLabel,
		"mirror": mirrorConfig.Mirror,
		"url":    mirrorConfig.URL,
		"username": mirrorConfig.Username,
	}

	resp, err := cli.ContainerCreate(ctx, &container.Config{
		Image: constants.RegistryImageV3,
		Tty:   false,
		Env:   envs,
		ExposedPorts: nat.PortSet{
			"5000/tcp": struct{}{},
		},
		Labels: labels,
	}, hostConfig, nil, nil, mirrorContainerName)

	if err != nil {
		return errors.Wrap(err, "cannot create local registry mirror container")
	}

	if err := cli.ContainerStart(ctx, resp.ID, container.StartOptions{}); err != nil {
		return errors.Wrap(err, "unable to start local registry mirror")
	}

	cli.NetworkDisconnect(ctx, constants.EducatesNetworkName, mirrorContainerName, false)

	err = cli.NetworkConnect(ctx, constants.EducatesNetworkName, mirrorContainerName, &network.EndpointSettings{})

	if err != nil {
		return errors.Wrap(err, "unable to connect local registry mirror to educates network")
	}

	if err = linkRegistryToClusterNetwork(mirrorContainerName); err != nil {
		return errors.Wrap(err, "failed to link local registry mirror to cluster")
	}

	return nil
}

/**
 * This function is used to delete a local registry mirror and unlink it from the cluster.
 * It is used when deleting a local registry mirror.
 */
func DeleteMirrorAndUnlinkFromCluster(mirrorConfig *config.RegistryMirrorConfig) error {
	ctx := context.Background()

	fmt.Printf("Deleting local image registry mirror %s\n", mirrorConfig.Mirror)

	cli, err := client.NewClientWithOpts(client.FromEnv)

	if err != nil {
		return errors.Wrap(err, "unable to create docker client")
	}

	containerName := registryMirrorContainerName(mirrorConfig)
	_, err = cli.ContainerInspect(ctx, containerName)

	if err != nil {
		// If we can't retrieve a container of required name we assume it does
		// not actually exist.

		fmt.Printf("Registry mirror %s does not exist\n", mirrorConfig.Mirror)
		return nil
	}

	timeout := 30

	err = cli.ContainerStop(ctx, containerName, container.StopOptions{Timeout: &timeout})

	if err != nil {
		return errors.Wrap(err, "unable to stop registry mirror container "+containerName)
	}

	err = cli.ContainerRemove(ctx, containerName, container.RemoveOptions{})

	if err != nil {
		return errors.Wrap(err, "unable to delete registry mirror container "+containerName)
	}

	// Remove the registry config from the kind nodes
	err = removeRegistryConfigFromKindNodes(mirrorConfig.Mirror)

	if err != nil {
		return errors.Wrap(err, "unable to remove registry config from kind nodes")
	}

	return nil
}

func DeleteRegistryMirrors() error {
	ctx := context.Background()

	fmt.Println("Deleting local image registry mirrors")

	cli, err := client.NewClientWithOpts(client.FromEnv)

	if err != nil {
		return errors.Wrap(err, "unable to create docker client")
	}

	mirrors, err := cli.ContainerList(ctx, container.ListOptions{
		Filters: filters.NewArgs(
			filters.Arg("label", "role="+constants.EducatesMirrorRoleLabel),
			filters.Arg("label", "app="+constants.EducatesAppLabel),
		),
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

/**
 * This function is used to list all local image registry mirrors.
 */
func ListRegistryMirrors() (string, error) {
	ctx := context.Background()

	cli, err := client.NewClientWithOpts(client.FromEnv)
	if err != nil {
		return "", errors.Wrap(err, "unable to create docker client")
	}

	mirrors, err := cli.ContainerList(ctx, container.ListOptions{Filters: filters.NewArgs(filters.Arg("label", "role="+constants.EducatesMirrorRoleLabel), filters.Arg("label", "app="+constants.EducatesAppLabel))})
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

/**
 * This function is used to get the container name of a registry mirror.
 */
func registryMirrorContainerName(mirrorConfig *config.RegistryMirrorConfig) string {
	return fmt.Sprintf("%s-mirror-%s", constants.EducatesRegistryContainer, mirrorConfig.Mirror)
}
