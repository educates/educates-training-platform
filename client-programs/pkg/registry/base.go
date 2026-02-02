package registry

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"os"
	"path"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/client"
	"github.com/docker/go-connections/nat"
	"github.com/educates/educates-training-platform/client-programs/pkg/config"
	"github.com/educates/educates-training-platform/client-programs/pkg/constants"
	"github.com/educates/educates-training-platform/client-programs/pkg/utils"
	"github.com/pkg/errors"
)

// baseContainer contains common configuration and methods for registry and mirror containers.
type baseContainer struct {
	containerName string
	bindIP        string
	labels        map[string]string
	envVars       []string
	hostPort      string
}

// ensureNetwork creates the specified docker network if it doesn't exist.
func (b *baseContainer) ensureNetwork(cli *client.Client, networkName string) error {
	ctx := context.Background()

	_, err := cli.NetworkInspect(ctx, networkName, network.InspectOptions{})
	if err != nil {
		_, err = cli.NetworkCreate(ctx, networkName, network.CreateOptions{})
		if err != nil {
			return errors.Wrap(err, fmt.Sprintf("cannot create %s network", networkName))
		}
	}
	return nil
}

// containerExists checks if the container already exists.
func (b *baseContainer) containerExists(cli *client.Client) (bool, string, error) {
	ctx := context.Background()
	response, err := cli.ContainerInspect(ctx, b.containerName)
	if err == nil {
		return true, response.ID, nil
	}
	return false, "", err
}

// pullRegistryImage pulls the registry image.
func (b *baseContainer) pullRegistryImage(cli *client.Client) error {
	ctx := context.Background()

	reader, err := cli.ImagePull(ctx, constants.RegistryImageV3, image.PullOptions{})
	if err != nil {
		return errors.Wrap(err, "cannot pull registry image")
	}

	defer reader.Close()
	io.Copy(os.Stdout, reader)
	return nil
}

// createAndStartContainer creates and starts the container with the given configuration.
func (b *baseContainer) createAndStartContainer(cli *client.Client) (string, error) {
	ctx := context.Background()

	containerID, err := b.createContainer(cli, ctx)
	if err != nil {
		return "", errors.Wrap(err, "cannot create container")
	}

	if err := b.startContainer(cli, ctx, containerID); err != nil {
		return "", errors.Wrap(err, "unable to start container")
	}

	return containerID, nil
}

func (b *baseContainer) createContainer(cli *client.Client, ctx context.Context) (string, error) {
	hostConfig := &container.HostConfig{
		PortBindings: nat.PortMap{
			"5000/tcp": []nat.PortBinding{
				{
					HostIP:   b.bindIP,
					HostPort: b.hostPort,
				},
			},
		},
		RestartPolicy: container.RestartPolicy{
			Name: "always",
		},
	}

	containerConfig := &container.Config{
		Image: constants.RegistryImageV3,
		Tty:   false,
		ExposedPorts: nat.PortSet{
			"5000/tcp": struct{}{},
		},
		Labels: b.labels,
		Env:    b.envVars,
	}

	resp, err := cli.ContainerCreate(ctx, containerConfig, hostConfig, nil, nil, b.containerName)
	if err != nil {
		return "", err
	}

	return resp.ID, nil
}

func (b *baseContainer) startContainer(cli *client.Client, ctx context.Context, containerID string) error {
	if err := cli.ContainerStart(ctx, containerID, container.StartOptions{}); err != nil {
		return err
	}

	return nil
}

// connectToNetwork connects the container to the specified network.
func (b *baseContainer) connectToNetwork(cli *client.Client, networkName string, fixedIP string) error {
	ctx := context.Background()

	containerInfo, err := cli.ContainerInspect(ctx, b.containerName)
	if err != nil {
		return errors.Wrap(err, "unable to inspect container")
	}

	if network, exists := containerInfo.NetworkSettings.Networks[networkName]; exists {
		if fixedIP == "" || network.IPAddress == fixedIP {
			return nil
		}
	}

	cli.NetworkDisconnect(ctx, networkName, b.containerName, false)

	endpointSettings := &network.EndpointSettings{}
	if fixedIP != "" {
		endpointSettings.IPAddress = fixedIP
		endpointSettings.IPAMConfig = &network.EndpointIPAMConfig{
			IPv4Address: fixedIP,
		}
	}

	if err := cli.NetworkConnect(ctx, networkName, b.containerName, endpointSettings); err != nil {
		return errors.Wrap(err, fmt.Sprintf("unable to connect container to %s network", networkName))
	}

	return nil
}

// stopAndRemoveContainer stops and removes the container.
func (b *baseContainer) stopAndRemoveContainer(cli *client.Client) error {
	ctx := context.Background()

	exists, _, _ := b.containerExists(cli)
	if !exists {
		return nil
	}

	timeout := 30
	err := cli.ContainerStop(ctx, b.containerName, container.StopOptions{
		Timeout: &timeout,
	})
	if err != nil {
		return errors.Wrap(err, "unable to stop container")
	}

	err = cli.ContainerRemove(ctx, b.containerName, container.RemoveOptions{})
	if err != nil {
		return errors.Wrap(err, "unable to delete container")
	}

	return nil
}

// getEducatesKindNodeContainers returns all kind node containers for the educates cluster
func getEducatesKindNodeContainers(cli *client.Client) ([]string, error) {
	ctx := context.Background()

	// Kind labels all node containers with io.x-k8s.kind.cluster=<cluster-name>
	nodeFilters := filters.NewArgs()
	nodeFilters.Add("label", fmt.Sprintf("io.x-k8s.kind.cluster=%s", constants.EducatesClusterName))

	containers, err := cli.ContainerList(ctx, container.ListOptions{
		All:     true,
		Filters: nodeFilters,
	})
	if err != nil {
		return nil, errors.Wrap(err, "failed to list kind node containers")
	}

	if len(containers) == 0 {
		return nil, errors.New("no kind node containers found for educates cluster")
	}

	containerIDs := make([]string, len(containers))
	for i, c := range containers {
		containerIDs[i] = c.ID
	}

	return containerIDs, nil
}

// addRegistryConfigToNode adds the registry config to a single kind node container
func addRegistryConfigToNode(cli *client.Client, containerID, repositoryName, content string) error {
	ctx := context.Background()

	registryDir := "/etc/containerd/certs.d/" + repositoryName

	cmdStatement := []string{"mkdir", "-p", registryDir}

	optionsCreateExecuteScript := container.ExecOptions{
		AttachStdout: true,
		AttachStderr: true,
		Cmd:          cmdStatement,
	}

	response, err := cli.ContainerExecCreate(ctx, containerID, optionsCreateExecuteScript)
	if err != nil {
		return errors.Wrap(err, "unable to create exec command")
	}
	hijackedResponse, err := cli.ContainerExecAttach(ctx, response.ID, container.ExecAttachOptions{})
	if err != nil {
		return errors.Wrap(err, "unable to attach exec command")
	}

	hijackedResponse.Close()

	buffer, err := tarFile([]byte(content), path.Join("/etc/containerd/certs.d/"+repositoryName, "hosts.toml"), 0x644)
	if err != nil {
		return err
	}
	err = cli.CopyToContainer(context.Background(),
		containerID, "/",
		buffer,
		container.CopyToContainerOptions{
			AllowOverwriteDirWithFile: true,
		})
	if err != nil {
		return errors.Wrap(err, "unable to copy file to container")
	}

	return nil
}

// addRegistryConfigToKindNodes adds the registry config to all kind nodes.
// It is used when creating a new local registry or registry mirror.
func addRegistryConfigToKindNodes(repositoryName string, content string) error {
	fmt.Printf("Adding local image registry config (%s) to Kind nodes\n", repositoryName)

	cli, err := utils.NewDockerClient()
	if err != nil {
		return errors.Wrap(err, "unable to create docker client")
	}

	containerIDs, err := getEducatesKindNodeContainers(cli)
	if err != nil {
		return err
	}

	// Apply config to all nodes (control-plane and workers)
	for _, containerID := range containerIDs {
		if err := addRegistryConfigToNode(cli, containerID, repositoryName, content); err != nil {
			return errors.Wrapf(err, "failed to add registry config to node %s", containerID)
		}
	}

	return nil
}

// removeRegistryConfigFromNode removes the registry config from a single kind node container
func removeRegistryConfigFromNode(cli *client.Client, containerID, repositoryName string) error {
	ctx := context.Background()

	registryDir := "/etc/containerd/certs.d/" + repositoryName

	cmdStatement := []string{"rm", "-rf", registryDir}

	optionsCreateExecuteScript := container.ExecOptions{
		AttachStdout: true,
		AttachStderr: true,
		Cmd:          cmdStatement,
	}

	response, err := cli.ContainerExecCreate(ctx, containerID, optionsCreateExecuteScript)
	if err != nil {
		return errors.Wrap(err, "unable to create exec command")
	}

	hijackedResponse, err := cli.ContainerExecAttach(ctx, response.ID, container.ExecAttachOptions{})
	if err != nil {
		return errors.Wrap(err, "unable to attach exec command")
	}

	hijackedResponse.Close()

	return nil
}

// removeRegistryConfigFromKindNodes removes the registry config from all kind nodes.
// It is used when deleting a local registry mirror.
func removeRegistryConfigFromKindNodes(repositoryName string) error {
	fmt.Printf("Removing local image registry config (%s) from Kind nodes\n", repositoryName)

	cli, err := utils.NewDockerClient()
	if err != nil {
		return errors.Wrap(err, "unable to create docker client")
	}

	containerIDs, err := getEducatesKindNodeContainers(cli)
	if err != nil {
		// If nodes don't exist, nothing to remove
		return nil
	}

	// Remove config from all nodes (control-plane and workers)
	for _, containerID := range containerIDs {
		if err := removeRegistryConfigFromNode(cli, containerID, repositoryName); err != nil {
			return errors.Wrapf(err, "failed to remove registry config from node %s", containerID)
		}
	}

	return nil
}

// tarFile creates a tar archive with a single file.
func tarFile(fileContent []byte, basePath string, fileMode int64) (*bytes.Buffer, error) {
	buffer := &bytes.Buffer{}

	zr := gzip.NewWriter(buffer)
	tw := tar.NewWriter(zr)

	hdr := &tar.Header{
		Name: basePath,
		Mode: fileMode,
		Size: int64(len(fileContent)),
	}
	if err := tw.WriteHeader(hdr); err != nil {
		return buffer, err
	}
	if _, err := tw.Write(fileContent); err != nil {
		return buffer, err
	}

	// produce tar
	if err := tw.Close(); err != nil {
		return buffer, fmt.Errorf("error closing tar file: %w", err)
	}
	// produce gzip
	if err := zr.Close(); err != nil {
		return buffer, fmt.Errorf("error closing gzip file: %w", err)
	}

	return buffer, nil
}

func getRegistryMirrorLabelFilters() filters.Args {
	return filters.NewArgs(
		filters.Arg("label", constants.EducatesContainersRoleLabelKey+"="+constants.EducatesContainersMirrorRoleLabel),
		filters.Arg("label", constants.EducatesContainersAppLabelKey+"="+constants.EducatesContainersAppLabel),
	)
}

func newRegistryContainerLabels() map[string]string {
	return map[string]string{
		constants.EducatesContainersRoleLabelKey: constants.EducatesContainersRegistryRoleLabel,
		constants.EducatesContainersAppLabelKey: constants.EducatesContainersAppLabel,
	}
}

func newMirrorContainerLabels(mirrorConfig *config.RegistryMirrorConfig) map[string]string {
	return map[string]string{
		constants.EducatesContainersRoleLabelKey: constants.EducatesContainersMirrorRoleLabel,
		constants.EducatesContainersAppLabelKey: constants.EducatesContainersAppLabel,
		constants.EducatesContainersMirrorLabelKey: mirrorConfig.Mirror,
		constants.EducatesContainersURLLabelKey: mirrorConfig.URL,
		constants.EducatesContainersUsernameLabelKey: mirrorConfig.Username,
	}
}
