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
func (b *baseContainer) containerExists(cli *client.Client) (bool, error) {
	ctx := context.Background()
	_, err := cli.ContainerInspect(ctx, b.containerName)
	if err == nil {
		return true, nil
	}
	return false, nil
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
		return "", errors.Wrap(err, "cannot create container")
	}

	if err := cli.ContainerStart(ctx, resp.ID, container.StartOptions{}); err != nil {
		return "", errors.Wrap(err, "unable to start container")
	}

	return resp.ID, nil
}

// connectToNetwork connects the container to the specified network.
func (b *baseContainer) connectToNetwork(cli *client.Client, networkName string) error {
	ctx := context.Background()

	cli.NetworkDisconnect(ctx, networkName, b.containerName, false)

	err := cli.NetworkConnect(ctx, networkName, b.containerName, &network.EndpointSettings{})
	if err != nil {
		return errors.Wrap(err, fmt.Sprintf("unable to connect container to %s network", networkName))
	}

	return nil
}

// linkToNetwork connects the container to the specified network.
func (b *baseContainer) linkToNetwork(cli *client.Client, networkName string) error {
	ctx := context.Background()

	fmt.Println("Linking local image registry to cluster")

	cli.NetworkDisconnect(ctx, networkName, b.containerName, false)

	err := cli.NetworkConnect(ctx, networkName, b.containerName, &network.EndpointSettings{})
	if err != nil {
		return errors.Wrap(err, fmt.Sprintf("unable to connect container to %s network", networkName))
	}

	return nil
}

// stopAndRemoveContainer stops and removes the container.
func (b *baseContainer) stopAndRemoveContainer(cli *client.Client) error {
	ctx := context.Background()

	exists, _ := b.containerExists(cli)
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

// addRegistryConfigToKindNodes adds the registry config to the kind nodes.
// It is used when creating a new local registry or registry mirror.
func addRegistryConfigToKindNodes(repositoryName string, content string) error {
	ctx := context.Background()

	fmt.Printf("Adding local image registry config (%s) to Kind nodes\n", repositoryName)

	cli, err := client.NewClientWithOpts(client.FromEnv)
	if err != nil {
		return errors.Wrap(err, "unable to create docker client")
	}

	containerID, _ := utils.GetContainerInfo(constants.EducatesControlPlaneContainer)
	if containerID == "" {
		return errors.New(fmt.Sprintf("%s container not found", constants.EducatesControlPlaneContainer))
	}

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

// removeRegistryConfigFromKindNodes removes the registry config from the kind nodes.
// It is used when deleting a local registry mirror.
func removeRegistryConfigFromKindNodes(repositoryName string) error {
	ctx := context.Background()

	fmt.Printf("Removing local image registry config (%s) from Kind nodes\n", repositoryName)

	cli, err := client.NewClientWithOpts(client.FromEnv)
	if err != nil {
		return errors.Wrap(err, "unable to create docker client")
	}

	containerID, _ := utils.GetContainerInfo(constants.EducatesControlPlaneContainer)
	if containerID == "" {
		return nil
	}

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
