package registry

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	_ "embed"
	"fmt"
	"io"
	"os"
	"path"
	"strings"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/client"
	"github.com/docker/go-connections/nat"
	"github.com/educates/educates-training-platform/client-programs/pkg/config"
	"github.com/pkg/errors"
	yaml "gopkg.in/yaml.v2"
	v1 "k8s.io/api/core/v1"
	discoveryv1 "k8s.io/api/discovery/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/kubernetes"
)

const hostMirrorTomlTemplate = `[host."http://%s:5000"]
  capabilities = ["pull", "resolve"]
`

const hostRegistryTomlTemplate = `[host."http://%s:5000"]`

const (
	RegistryImageV3               = "docker.io/library/registry:3"
	RegistryConfigTargetPath      = "/etc/distribution/config.yml"
	EducatesNetworkName           = "educates"
	EducatesRegistryContainer     = "educates-registry"
	EducatesControlPlaneContainer = "educates-control-plane"
	EducatesRegistryRoleLabel     = "registry"
	EducatesMirrorRoleLabel       = "mirror"
	EducatesAppLabel              = "educates"
)

/**
 * This function is used to deploy the registry and link it to the cluster.
 * It is used when creating a new local cluster.
 */
func DeployRegistryAndLinkToCluster(bindIP string, client *kubernetes.Clientset) error {

	err := createRegistryContainer(bindIP)
	if err != nil {
		return errors.Wrap(err, "failed to deploy registry")
	}

	// This is needed to make containerd use the local registry

	if err = addRegistryConfigToKindNodes("localhost:5001", fmt.Sprintf(hostRegistryTomlTemplate, EducatesRegistryContainer)); err != nil {
		return errors.Wrap(err, "failed to add registry config to kind nodes")
	}
	if err = addRegistryConfigToKindNodes("registry.default.svc.cluster.local", fmt.Sprintf(hostRegistryTomlTemplate, EducatesRegistryContainer)); err != nil {
		return errors.Wrap(err, "failed to add registry config to kind nodes")
	}

	// This is needed so that kubernetes nodes can pull images from the local registry
	if err = documentLocalRegistry(client); err != nil {
		return errors.Wrap(err, "failed to document registry config in cluster")
	}

	return nil
}

/**
 * This function is used to deploy a registry.
 * It is used when creating a new local registry.
 * It will not link the registry to the cluster.
 */
func DeployRegistry(bindIP string) error {
	err := createRegistryContainer(bindIP)
	if err != nil {
		return errors.Wrap(err, "failed to deploy registry")
	}

	return nil
}

/**
 * This private function only creates the registry container.
 */
func createRegistryContainer(bindIP string) error {
	ctx := context.Background()

	fmt.Println("Deploying local image registry")

	cli, err := client.NewClientWithOpts(client.FromEnv)

	if err != nil {
		return errors.Wrap(err, "unable to create docker client")
	}

	_, err = cli.ContainerInspect(ctx, EducatesRegistryContainer)

	if err == nil {
		// If we can retrieve a container of required name we assume it is
		// running okay. Technically it could be restarting, stopping or
		// have exited and container was not removed, but if that is the case
		// then leave it up to the user to sort out.

		return nil
	}

	reader, err := cli.ImagePull(ctx, RegistryImageV3, image.PullOptions{})
	if err != nil {
		return errors.Wrap(err, "cannot pull registry image")
	}

	defer reader.Close()
	io.Copy(os.Stdout, reader)

	_, err = cli.NetworkInspect(ctx, EducatesNetworkName, network.InspectOptions{})

	if err != nil {
		_, err = cli.NetworkCreate(ctx, EducatesNetworkName, network.CreateOptions{})

		if err != nil {
			return errors.Wrap(err, "cannot create educates network")
		}
	}

	hostConfig := &container.HostConfig{
		PortBindings: nat.PortMap{
			"5000/tcp": []nat.PortBinding{
				{
					HostIP:   bindIP,
					HostPort: "5001",
				},
			},
		},
		RestartPolicy: container.RestartPolicy{
			Name: "always",
		},
	}

	labels := map[string]string{
		"app":  EducatesAppLabel,
		"role": EducatesRegistryRoleLabel,
	}

	resp, err := cli.ContainerCreate(ctx, &container.Config{
		Image: RegistryImageV3,
		Tty:   false,
		ExposedPorts: nat.PortSet{
			"5000/tcp": struct{}{},
		},
		Labels: labels,
	}, hostConfig, nil, nil, EducatesRegistryContainer)

	if err != nil {
		return errors.Wrap(err, "cannot create registry container")
	}

	if err := cli.ContainerStart(ctx, resp.ID, container.StartOptions{}); err != nil {
		return errors.Wrap(err, "unable to start registry")
	}

	cli.NetworkDisconnect(ctx, EducatesNetworkName, EducatesRegistryContainer, false)

	err = cli.NetworkConnect(ctx, EducatesNetworkName, EducatesRegistryContainer, &network.EndpointSettings{})

	if err != nil {
		return errors.Wrap(err, "unable to connect registry to educates network")
	}

	if err = linkRegistryToClusterNetwork(EducatesRegistryContainer); err != nil {
		return errors.Wrap(err, "failed to link registry to cluster")
	}

	return nil
}

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

	_, err = cli.NetworkInspect(ctx, EducatesNetworkName, network.InspectOptions{})

	if err != nil {
		_, err = cli.NetworkCreate(ctx, EducatesNetworkName, network.CreateOptions{})

		if err != nil {
			return errors.Wrap(err, "cannot create educates network")
		}
	}

	hostConfig := &container.HostConfig{
		PortBindings: nat.PortMap{
			"5000/tcp": []nat.PortBinding{
				{
					// HostIP:   mirrorConfig.BindIP,
					// HostPort: mirrorConfig.Port,
				},
			},
		},
		RestartPolicy: container.RestartPolicy{
			Name: "always",
		},
	}

	labels := map[string]string{
		"app":    EducatesAppLabel,
		"role":   EducatesMirrorRoleLabel,
		"mirror": mirrorConfig.Mirror,
	}

	resp, err := cli.ContainerCreate(ctx, &container.Config{
		Image: RegistryImageV3,
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

	cli.NetworkDisconnect(ctx, EducatesNetworkName, mirrorContainerName, false)

	err = cli.NetworkConnect(ctx, EducatesNetworkName, mirrorContainerName, &network.EndpointSettings{})

	if err != nil {
		return errors.Wrap(err, "unable to connect local registry mirror to educates network")
	}

	if err = linkRegistryToClusterNetwork(mirrorContainerName); err != nil {
		return errors.Wrap(err, "failed to link local registry mirror to cluster")
	}

	return nil
}

/**
 * This function is used to add the registry config to the kind nodes.
 * It is used when creating a new local registry or registry mirror.
 */
func addRegistryConfigToKindNodes(repositoryName string, content string) error {
	ctx := context.Background()

	fmt.Printf("Adding local image registry config (%s) to Kind nodes\n", repositoryName)

	cli, err := client.NewClientWithOpts(client.FromEnv)

	if err != nil {
		return errors.Wrap(err, "unable to create docker client")
	}

	containerID, _ := getContainerInfo(EducatesControlPlaneContainer)

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

/**
 * This function is used to remove the registry config from the kind nodes.
 * It is used when deleting a local registry mirror.
 */
func removeRegistryConfigFromKindNodes(repositoryName string) error {
	ctx := context.Background()

	fmt.Printf("Removing local image registry config (%s) from Kind nodes\n", repositoryName)

	cli, err := client.NewClientWithOpts(client.FromEnv)

	if err != nil {
		return errors.Wrap(err, "unable to create docker client")
	}

	containerID, _ := getContainerInfo(EducatesControlPlaneContainer)

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

/**
 * This function is used to document the local registry in the cluster.
 * It is used when creating a new local registry.
 */
func documentLocalRegistry(client *kubernetes.Clientset) error {
	yamlBytes, err := yaml.Marshal(`host: "localhost:5001"`)
	if err != nil {
		return err
	}

	configMap := &v1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "local-registry-hosting",
			Namespace: "kube-public",
		},
		Data: map[string]string{
			"localRegistryHosting.v1": string(yamlBytes),
		},
	}

	if _, err := client.CoreV1().ConfigMaps("kube-public").Get(context.TODO(), "local-registry-hosting", metav1.GetOptions{}); k8serrors.IsNotFound(err) {
		_, err = client.CoreV1().ConfigMaps("kube-public").Create(context.TODO(), configMap, metav1.CreateOptions{})
		if err != nil {
			return errors.Wrap(err, "unable to create local registry hosting config map")
		}
	} else {
		_, err = client.CoreV1().ConfigMaps("kube-public").Update(context.TODO(), configMap, metav1.UpdateOptions{})
		if err != nil {
			return errors.Wrap(err, "unable to update local registry hosting config map")
		}
	}

	return nil
}

/**
 * This function is used to link the registry to the cluster network, which is the kind network.
 * It is used when creating a new local registry or registry mirror containers.
 */
func linkRegistryToClusterNetwork(containerName string) error {
	ctx := context.Background()

	fmt.Println("Linking local image registry to cluster")

	cli, err := client.NewClientWithOpts(client.FromEnv)

	if err != nil {
		return errors.Wrap(err, "unable to create docker client")
	}

	cli.NetworkDisconnect(ctx, "kind", containerName, false)

	err = cli.NetworkConnect(ctx, "kind", containerName, &network.EndpointSettings{})

	if err != nil {
		return errors.Wrap(err, "unable to connect registry to cluster network")
	}

	return nil
}

/**
 * This function is used to delete the local registry.
 * It is used when deleting a local registry or deleting all components of the local cluster.
 */
func DeleteRegistry() error {
	ctx := context.Background()

	fmt.Println("Deleting local image registry")

	cli, err := client.NewClientWithOpts(client.FromEnv)

	if err != nil {
		return errors.Wrap(err, "unable to create docker client")
	}

	_, err = cli.ContainerInspect(ctx, EducatesRegistryContainer)

	if err != nil {
		// If we can't retrieve a container of required name we assume it does
		// not actually exist.

		return nil
	}

	timeout := 30

	err = cli.ContainerStop(ctx, EducatesRegistryContainer, container.StopOptions{Timeout: &timeout})

	// timeout := time.Duration(30) * time.Second

	// err = cli.ContainerStop(ctx, EducatesRegistryContainer, &timeout)

	if err != nil {
		return errors.Wrap(err, "unable to stop registry container")
	}

	err = cli.ContainerRemove(ctx, EducatesRegistryContainer, container.RemoveOptions{})

	if err != nil {
		return errors.Wrap(err, "unable to delete registry container")
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
			filters.Arg("label", "role="+EducatesMirrorRoleLabel),
			filters.Arg("label", "app="+EducatesAppLabel),
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
 * TODO: Learn whether this is needed or not
 * This function is used to update the registry k8s service.
 * It is used when creating a cluster or a registry in order to update the k8s service to point to the new registry.
 */
func UpdateRegistryK8SService(k8sclient *kubernetes.Clientset) error {
	ctx := context.Background()

	cli, err := client.NewClientWithOpts(client.FromEnv)

	if err != nil {
		return errors.Wrap(err, "unable to create docker client")
	}

	service := v1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name: "registry",
		},
		Spec: v1.ServiceSpec{
			Type: v1.ServiceTypeClusterIP,
			Ports: []v1.ServicePort{
				{
					Port:       80,
					TargetPort: intstr.FromInt(5001),
				},
			},
		},
	}

	endpointPort := int32(5000)
	endpointPortName := ""
	endpointAppProtocol := "http"
	endpointProtocol := v1.ProtocolTCP

	registryInfo, err := cli.ContainerInspect(ctx, EducatesRegistryContainer)

	if err != nil {
		return errors.Wrapf(err, "unable to inspect container for registry")
	}

	kindNetwork, exists := registryInfo.NetworkSettings.Networks["kind"]

	if !exists {
		return errors.New("registry is not attached to kind network")
	}

	endpointAddresses := []string{kindNetwork.IPAddress}

	endpointSlice := discoveryv1.EndpointSlice{
		ObjectMeta: metav1.ObjectMeta{
			Name: "registry-1",
			Labels: map[string]string{
				"kubernetes.io/service-name": "registry",
			},
		},
		AddressType: "IPv4",
		Ports: []discoveryv1.EndpointPort{
			{
				Name:        &endpointPortName,
				AppProtocol: &endpointAppProtocol,
				Protocol:    &endpointProtocol,
				Port:        &endpointPort,
			},
		},
		Endpoints: []discoveryv1.Endpoint{
			{
				Addresses: endpointAddresses,
			},
		},
	}

	endpointSliceClient := k8sclient.DiscoveryV1().EndpointSlices("default")

	endpointSliceClient.Delete(context.TODO(), "registry-1", *metav1.NewDeleteOptions(0))

	servicesClient := k8sclient.CoreV1().Services("default")

	servicesClient.Delete(context.TODO(), "registry", *metav1.NewDeleteOptions(0))

	_, err = endpointSliceClient.Create(context.TODO(), &endpointSlice, metav1.CreateOptions{})

	if err != nil {
		return errors.Wrap(err, "unable to create registry headless service endpoint")
	}

	_, err = servicesClient.Create(context.TODO(), &service, metav1.CreateOptions{})

	if err != nil {
		return errors.Wrap(err, "unable to create registry headless service")
	}

	return nil
}

func PruneRegistry() error {
	ctx := context.Background()

	fmt.Println("Pruning local image registry")

	cli, err := client.NewClientWithOpts(client.FromEnv)

	if err != nil {
		return errors.Wrap(err, "unable to create docker client")
	}

	containerID, _ := getContainerInfo(EducatesRegistryContainer)

	cmdStatement := []string{"registry", "garbage-collect", RegistryConfigTargetPath, "--delete-untagged=true"}

	optionsCreateExecuteScript := container.ExecOptions{
		AttachStdout: false,
		AttachStderr: false,
		Cmd:          cmdStatement,
	}

	response, err := cli.ContainerExecCreate(ctx, containerID, optionsCreateExecuteScript)
	if err != nil {
		return errors.Wrap(err, "unable to create exec command")
	}
	err = cli.ContainerExecStart(ctx, response.ID, container.ExecStartOptions{})
	if err != nil {
		return errors.Wrap(err, "unable to exec command")
	}

	fmt.Println("Registry pruned succesfully")

	return nil
}

/**
 * This function is used to get the container name of a registry mirror.
 */
func registryMirrorContainerName(mirrorConfig *config.RegistryMirrorConfig) string {
	return fmt.Sprintf("%s-mirror-%s", EducatesRegistryContainer, mirrorConfig.Mirror)
}

/**
 * This function is used to get the container id and status of a container.
 * If the container does not exist, it will return an empty string for the container id and status.
 */
func getContainerInfo(containerName string) (containerID string, status string) {
	ctx := context.Background()

	cli, err := client.NewClientWithOpts(client.FromEnv)
	if err != nil {
		panic(err)
	}

	filters := filters.NewArgs()
	filters.Add(
		"name", containerName,
	)

	resp, err := cli.ContainerList(ctx, container.ListOptions{Filters: filters})
	if err != nil {
		panic(err)
	}

	if len(resp) > 0 {
		containerID = resp[0].ID
		containerStatus := strings.Split(resp[0].Status, " ")
		status = containerStatus[0] //fmt.Println(status[0])
	} else {
		fmt.Printf("container '%s' does not exists\n", containerName)
	}

	return
}

/**
 * This function is used to tar a file to be copied into a container.
 */
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
