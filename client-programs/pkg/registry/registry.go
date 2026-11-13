package registry

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	_ "embed"
	"fmt"
	"io"
	"net/netip"
	"path"
	"sort"
	"strings"

	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/network"
	"github.com/moby/moby/client"

	"github.com/educates/educates-training-platform/client-programs/pkg/constants"
	"github.com/educates/educates-training-platform/client-programs/pkg/docker"
	"github.com/educates/educates-training-platform/client-programs/pkg/utils"
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

// Progress is the minimal surface the registry package uses to surface
// sub-operation detail on the caller's current step line. The caller
// (cmd) owns the step lifecycle (Start/Done/Fail); these functions only
// report intermediate phase text via Update, and warnings via Warn. A nil
// Progress is valid and silences detail — used by callers that don't
// render progress. progress.Step satisfies this interface.
type Progress interface {
	Update(phase string)
	Warn(msg string)
}

// report surfaces a phase line on the caller's step, tolerating a nil
// Progress so every call site stays a one-liner.
func report(p Progress, phase string) {
	if p != nil {
		p.Update(phase)
	}
}

// warn surfaces a warning on the caller's step, on a line of its own that
// stays on screen after the step closes, tolerating a nil Progress like
// report.
func warn(p Progress, msg string) {
	if p != nil {
		p.Warn(msg)
	}
}

const (
	RegistryImageV3           = "docker.io/library/registry:3"
	RegistryConfigTargetPath  = "/etc/distribution/config.yml"
	EducatesNetworkName       = "educates"
	ClusterNetworkName        = "kind"
	EducatesRegistryContainer = "educates-registry"
)

/**
 * This function is used to deploy the registry and link it to the cluster.
 * It is used when creating a new local cluster. nodes names the cluster's
 * node containers that get the registry's containerd host config.
 */
func DeployRegistryAndLinkToCluster(bindIP string, nodes []string, client *kubernetes.Clientset, p Progress) error {

	err := createRegistryContainer(bindIP, p)
	if err != nil {
		return errors.Wrap(err, "failed to deploy registry")
	}

	// This is needed to make containerd use the local registry

	if err = LinkRegistryToKindNodes(nodes, p); err != nil {
		return err
	}

	// This is needed so that kubernetes nodes can pull images from the local registry
	if err = documentLocalRegistry(client); err != nil {
		return errors.Wrap(err, "failed to document registry config in cluster")
	}

	return nil
}

/**
 * This function writes the local registry's containerd host config, for
 * localhost:5001 and registry.default.svc.cluster.local, into the node
 * containers nodes, so that containerd on each node pulls those names from
 * the registry container. Each node must be running.
 */
func LinkRegistryToKindNodes(nodes []string, p Progress) error {
	content := fmt.Sprintf(hostRegistryTomlTemplate, EducatesRegistryContainer)

	for _, repositoryName := range []string{"localhost:5001", "registry.default.svc.cluster.local"} {
		if err := addRegistryConfigToKindNodes(repositoryName, content, nodes, p); err != nil {
			return errors.Wrap(err, "failed to add registry config to kind nodes")
		}
	}

	return nil
}

/**
 * This function is used to deploy a registry.
 * It is used when creating a new local registry.
 * It will not link the registry to the cluster.
 */
func DeployRegistry(bindIP string, p Progress) error {
	err := createRegistryContainer(bindIP, p)
	if err != nil {
		return errors.Wrap(err, "failed to deploy registry")
	}

	return nil
}

/**
 * This private function only creates the registry container.
 */
func createRegistryContainer(bindIP string, p Progress) error {
	ctx := context.Background()

	report(p, "deploying registry container")

	cli, err := docker.NewDockerClient()

	if err != nil {
		return errors.Wrap(err, "unable to create docker client")
	}

	_, err = cli.ContainerInspect(ctx, EducatesRegistryContainer, client.ContainerInspectOptions{})

	if err == nil {
		// If we can retrieve a container of required name we assume it is
		// running okay. Technically it could be restarting, stopping or
		// have exited and container was not removed, but if that is the case
		// then leave it up to the user to sort out. It is still linked to the
		// cluster network, which moves a registry linked without a fixed
		// address onto its fixed address.

		if err = linkToClusterNetwork(EducatesRegistryContainer, registryFixedAddress, p); err != nil {
			return errors.Wrap(err, "failed to link registry to cluster")
		}

		return nil
	}

	report(p, "pulling registry image")

	reader, err := cli.ImagePull(ctx, RegistryImageV3, client.ImagePullOptions{})
	if err != nil {
		return errors.Wrap(err, "cannot pull registry image")
	}

	defer reader.Close()
	// Drain the pull stream so the image fully downloads. The raw layer
	// progress JSON is discarded rather than dumped to stdout, where it
	// would corrupt the caller's progress line; surfacing pull detail
	// under --verbose is a follow-up.
	io.Copy(io.Discard, reader)

	_, err = cli.NetworkInspect(ctx, EducatesNetworkName, client.NetworkInspectOptions{})

	if err != nil {
		_, err = cli.NetworkCreate(ctx, EducatesNetworkName, client.NetworkCreateOptions{})

		if err != nil {
			return errors.Wrap(err, "cannot create educates network")
		}
	}

	// An empty bind address keeps the zero netip.Addr, which the daemon
	// treats as binding to all interfaces, matching the previous string
	// based behaviour.
	var hostIP netip.Addr

	if bindIP != "" {
		hostIP, err = netip.ParseAddr(bindIP)

		if err != nil {
			return errors.Wrapf(err, "invalid registry bind address %q", bindIP)
		}
	}

	hostConfig := &container.HostConfig{
		PortBindings: network.PortMap{
			network.MustParsePort("5000/tcp"): []network.PortBinding{
				{
					HostIP:   hostIP,
					HostPort: "5001",
				},
			},
		},
		RestartPolicy: container.RestartPolicy{
			Name: "always",
		},
	}

	labels := map[string]string{
		constants.EducatesContainersAppLabelKey:  constants.EducatesContainersAppLabel,
		constants.EducatesContainersRoleLabelKey: constants.EducatesContainersRegistryRoleLabel,
	}

	resp, err := cli.ContainerCreate(ctx, client.ContainerCreateOptions{
		Config: &container.Config{
			Image: RegistryImageV3,
			Tty:   false,
			ExposedPorts: network.PortSet{
				network.MustParsePort("5000/tcp"): struct{}{},
			},
			Labels: labels,
		},
		HostConfig: hostConfig,
		Name:       EducatesRegistryContainer,
	})

	if err != nil {
		return errors.Wrap(err, "cannot create registry container")
	}

	if _, err := cli.ContainerStart(ctx, resp.ID, client.ContainerStartOptions{}); err != nil {
		return errors.Wrap(err, "unable to start registry")
	}

	cli.NetworkDisconnect(ctx, EducatesNetworkName, client.NetworkDisconnectOptions{Container: EducatesRegistryContainer})

	_, err = cli.NetworkConnect(ctx, EducatesNetworkName, client.NetworkConnectOptions{
		Container:      EducatesRegistryContainer,
		EndpointConfig: &network.EndpointSettings{},
	})

	if err != nil {
		return errors.Wrap(err, "unable to connect registry to educates network")
	}

	if err = linkToClusterNetwork(EducatesRegistryContainer, registryFixedAddress, p); err != nil {
		return errors.Wrap(err, "failed to link registry to cluster")
	}

	return nil
}

/**
 * This function is used to deploy a registry mirror and link it to the cluster.
 * It is used when creating a new local registry mirror. nodes names the
 * cluster's node containers that get the mirror's containerd host config.
 */
func DeployMirrorAndLinkToCluster(mirrorConfig *MirrorConfig, nodes []string, p Progress) error {
	err := createMirrorContainer(mirrorConfig, p)

	if err != nil {
		return errors.Wrap(err, "failed to deploy registry mirror "+mirrorConfig.Mirror)
	}

	if len(nodes) == 0 {
		warn(p, "mirror not added to kind nodes: no node containers of the local cluster found")
		return nil
	}

	content := fmt.Sprintf(hostMirrorTomlTemplate, registryMirrorContainerName(mirrorConfig))
	err = addRegistryConfigToKindNodes(mirrorConfig.Mirror, content, nodes, p)

	if err != nil {
		warn(p, "mirror not added to kind nodes: "+err.Error())
	}

	return nil
}

/**
 * This private function only creates the registry mirror container.
 */
func createMirrorContainer(mirrorConfig *MirrorConfig, p Progress) error {
	ctx := context.Background()

	report(p, "deploying mirror container")

	cli, err := docker.NewDockerClient()

	if err != nil {
		return errors.Wrap(err, "unable to create docker client")
	}

	mirrorContainerName := registryMirrorContainerName(mirrorConfig)

	mirrorFixedAddressFor := func(nw clusterNetwork) (netip.Addr, error) {
		return mirrorFixedAddress(nw, mirrorContainerName)
	}

	_, err = cli.ContainerInspect(ctx, mirrorContainerName, client.ContainerInspectOptions{})

	if err == nil {
		// If we can retrieve a container of required name we assume it is
		// running okay. Technically it could be restarting, stopping or
		// have exited and container was not removed, but if that is the case
		// then leave it up to the user to sort out. It is still linked to the
		// cluster network, which moves a mirror linked without a fixed
		// address onto its fixed address.
		report(p, "mirror container already exists")

		if err = linkToClusterNetwork(mirrorContainerName, mirrorFixedAddressFor, p); err != nil {
			return errors.Wrap(err, "failed to link local registry mirror to cluster")
		}

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

	_, err = cli.NetworkInspect(ctx, EducatesNetworkName, client.NetworkInspectOptions{})

	if err != nil {
		_, err = cli.NetworkCreate(ctx, EducatesNetworkName, client.NetworkCreateOptions{})

		if err != nil {
			return errors.Wrap(err, "cannot create educates network")
		}
	}

	hostConfig := &container.HostConfig{
		PortBindings: network.PortMap{
			network.MustParsePort("5000/tcp"): []network.PortBinding{
				{
					HostIP: netip.MustParseAddr("127.0.0.1"),
					// HostPort: mirrorConfig.Port,
				},
			},
		},
		RestartPolicy: container.RestartPolicy{
			Name: "always",
		},
	}

	labels := map[string]string{
		constants.EducatesContainersAppLabelKey:      constants.EducatesContainersAppLabel,
		constants.EducatesContainersRoleLabelKey:     constants.EducatesContainersMirrorRoleLabel,
		constants.EducatesContainersMirrorLabelKey:   mirrorConfig.Mirror,
		constants.EducatesContainersURLLabelKey:      mirrorConfig.URL,
		constants.EducatesContainersUsernameLabelKey: mirrorConfig.Username,
	}

	resp, err := cli.ContainerCreate(ctx, client.ContainerCreateOptions{
		Config: &container.Config{
			Image: RegistryImageV3,
			Tty:   false,
			Env:   envs,
			ExposedPorts: network.PortSet{
				network.MustParsePort("5000/tcp"): struct{}{},
			},
			Labels: labels,
		},
		HostConfig: hostConfig,
		Name:       mirrorContainerName,
	})

	if err != nil {
		return errors.Wrap(err, "cannot create local registry mirror container")
	}

	if _, err := cli.ContainerStart(ctx, resp.ID, client.ContainerStartOptions{}); err != nil {
		return errors.Wrap(err, "unable to start local registry mirror")
	}

	cli.NetworkDisconnect(ctx, EducatesNetworkName, client.NetworkDisconnectOptions{Container: mirrorContainerName})

	_, err = cli.NetworkConnect(ctx, EducatesNetworkName, client.NetworkConnectOptions{
		Container:      mirrorContainerName,
		EndpointConfig: &network.EndpointSettings{},
	})

	if err != nil {
		return errors.Wrap(err, "unable to connect local registry mirror to educates network")
	}

	if err = linkToClusterNetwork(mirrorContainerName, mirrorFixedAddressFor, p); err != nil {
		return errors.Wrap(err, "failed to link local registry mirror to cluster")
	}

	return nil
}

/**
 * This function is used to add the registry config to the kind nodes.
 * It is used when creating a new local registry or registry mirror.
 * nodes names the node containers to add it to. A node that is not running
 * can't take it: every other node still gets it, and the error names each
 * node that didn't.
 */
func addRegistryConfigToKindNodes(repositoryName string, content string, nodes []string, p Progress) error {
	ctx := context.Background()

	report(p, fmt.Sprintf("adding registry config (%s) to kind nodes", repositoryName))

	cli, err := docker.NewDockerClient()

	if err != nil {
		return errors.Wrap(err, "unable to create docker client")
	}

	var failed []string
	for _, node := range nodes {
		containerID := runningContainerID(ctx, cli, node)
		if containerID == "" {
			failed = append(failed, node+" is not running")
			continue
		}
		if err := addRegistryConfigToKindNode(ctx, cli, containerID, repositoryName, content); err != nil {
			failed = append(failed, fmt.Sprintf("%s: %v", node, err))
		}
	}

	if len(failed) > 0 {
		return errors.Errorf("node containers not configured: %s", strings.Join(failed, "; "))
	}

	return nil
}

/**
 * This function writes the registry config into the node container
 * containerID.
 */
func addRegistryConfigToKindNode(ctx context.Context, cli *client.Client, containerID, repositoryName, content string) error {
	registryDir := "/etc/containerd/certs.d/" + repositoryName

	cmdStatement := []string{"mkdir", "-p", registryDir}

	optionsCreateExecuteScript := client.ExecCreateOptions{
		AttachStdout: true,
		AttachStderr: true,
		Cmd:          cmdStatement,
	}

	response, err := cli.ExecCreate(ctx, containerID, optionsCreateExecuteScript)
	if err != nil {
		return errors.Wrap(err, "unable to create exec command")
	}
	hijackedResponse, err := cli.ExecAttach(ctx, response.ID, client.ExecAttachOptions{})
	if err != nil {
		return errors.Wrap(err, "unable to attach exec command")
	}

	hijackedResponse.Close()

	buffer, err := tarFile([]byte(content), path.Join("/etc/containerd/certs.d/"+repositoryName, "hosts.toml"), 0x644)
	if err != nil {
		return err
	}
	_, err = cli.CopyToContainer(ctx,
		containerID,
		client.CopyToContainerOptions{
			DestinationPath:           "/",
			Content:                   buffer,
			AllowOverwriteDirWithFile: true,
		})
	if err != nil {
		return errors.Wrap(err, "unable to copy file to container")
	}

	return nil
}

/**
 * This function is used to remove the registry config from the kind nodes.
 * It is used when deleting a local registry mirror. nodes names the node
 * containers to remove it from; one that is not running is skipped.
 */
func removeRegistryConfigFromKindNodes(repositoryName string, nodes []string, p Progress) error {
	ctx := context.Background()

	report(p, fmt.Sprintf("removing registry config (%s) from kind nodes", repositoryName))

	cli, err := docker.NewDockerClient()

	if err != nil {
		return errors.Wrap(err, "unable to create docker client")
	}

	for _, node := range nodes {
		containerID := runningContainerID(ctx, cli, node)
		if containerID == "" {
			continue
		}
		if err := removeRegistryConfigFromKindNode(ctx, cli, containerID, repositoryName); err != nil {
			return err
		}
	}

	return nil
}

/**
 * This function removes the registry config from the node container
 * containerID.
 */
func removeRegistryConfigFromKindNode(ctx context.Context, cli *client.Client, containerID, repositoryName string) error {
	registryDir := "/etc/containerd/certs.d/" + repositoryName

	cmdStatement := []string{"rm", "-rf", registryDir}

	optionsCreateExecuteScript := client.ExecCreateOptions{
		AttachStdout: true,
		AttachStderr: true,
		Cmd:          cmdStatement,
	}

	response, err := cli.ExecCreate(ctx, containerID, optionsCreateExecuteScript)
	if err != nil {
		return errors.Wrap(err, "unable to create exec command")
	}

	hijackedResponse, err := cli.ExecAttach(ctx, response.ID, client.ExecAttachOptions{})
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
 * This function is used to delete the local registry.
 * It is used when deleting a local registry or deleting all components of the local cluster.
 */
func DeleteRegistry(p Progress) error {
	ctx := context.Background()

	report(p, "deleting registry container")

	cli, err := docker.NewDockerClient()

	if err != nil {
		return errors.Wrap(err, "unable to create docker client")
	}

	_, err = cli.ContainerInspect(ctx, EducatesRegistryContainer, client.ContainerInspectOptions{})

	if err != nil {
		// If we can't retrieve a container of required name we assume it does
		// not actually exist.

		return nil
	}

	timeout := 30

	_, err = cli.ContainerStop(ctx, EducatesRegistryContainer, client.ContainerStopOptions{Timeout: &timeout})

	// timeout := time.Duration(30) * time.Second

	// err = cli.ContainerStop(ctx, EducatesRegistryContainer, &timeout)

	if err != nil {
		return errors.Wrap(err, "unable to stop registry container")
	}

	_, err = cli.ContainerRemove(ctx, EducatesRegistryContainer, client.ContainerRemoveOptions{})

	if err != nil {
		return errors.Wrap(err, "unable to delete registry container")
	}

	return nil
}

/**
 * This function is used to delete a local registry mirror and unlink it from the cluster.
 * It is used when deleting a local registry mirror. nodes names the
 * cluster's node containers to remove the mirror's containerd host config from.
 */
func DeleteMirrorAndUnlinkFromCluster(mirrorConfig *MirrorConfig, nodes []string, p Progress) error {
	ctx := context.Background()

	report(p, "deleting mirror container")

	cli, err := docker.NewDockerClient()

	if err != nil {
		return errors.Wrap(err, "unable to create docker client")
	}

	containerName := registryMirrorContainerName(mirrorConfig)
	_, err = cli.ContainerInspect(ctx, containerName, client.ContainerInspectOptions{})

	if err != nil {
		// If we can't retrieve a container of required name we assume it does
		// not actually exist.

		report(p, "mirror container does not exist")
		return nil
	}

	timeout := 30

	_, err = cli.ContainerStop(ctx, containerName, client.ContainerStopOptions{Timeout: &timeout})

	if err != nil {
		return errors.Wrap(err, "unable to stop registry mirror container "+containerName)
	}

	_, err = cli.ContainerRemove(ctx, containerName, client.ContainerRemoveOptions{})

	if err != nil {
		return errors.Wrap(err, "unable to delete registry mirror container "+containerName)
	}

	// Remove the registry config from the kind nodes
	err = removeRegistryConfigFromKindNodes(mirrorConfig.Mirror, nodes, p)

	if err != nil {
		return errors.Wrap(err, "unable to remove registry config from kind nodes")
	}

	return nil
}

func DeleteRegistryMirrors(p Progress) error {
	ctx := context.Background()

	report(p, "deleting mirror containers")

	cli, err := docker.NewDockerClient()

	if err != nil {
		return errors.Wrap(err, "unable to create docker client")
	}

	mirrors, err := cli.ContainerList(ctx, client.ContainerListOptions{
		All:     true,
		Filters: registryMirrorLabelFilters(),
	})

	if err != nil {
		return errors.Wrap(err, "unable to list registry mirrors")
	}

	for _, mirror := range mirrors.Items {

		timeout := 30

		_, err = cli.ContainerStop(ctx, mirror.ID, client.ContainerStopOptions{Timeout: &timeout})

		if err != nil {
			return errors.Wrap(err, "unable to stop registry mirror container "+mirror.ID)
		}

		_, err = cli.ContainerRemove(ctx, mirror.ID, client.ContainerRemoveOptions{})

		if err != nil {
			return errors.Wrap(err, "unable to delete registry mirror container "+mirror.ID)
		}

	}

	return nil
}

// RegistryMirror describes a deployed local registry mirror, read back from
// its container labels.
type RegistryMirror struct {
	Name          string
	URL           string
	Username      string
	Status        string
	ContainerName string
}

// registryMirrorLabelFilters is the label selector identifying registry
// mirror containers. Shared by the list and bulk-delete paths so both
// discover exactly the same set.
func registryMirrorLabelFilters() client.Filters {
	return client.Filters{}.
		Add("label", constants.EducatesContainersRoleLabelKey+"="+constants.EducatesContainersMirrorRoleLabel).
		Add("label", constants.EducatesContainersAppLabelKey+"="+constants.EducatesContainersAppLabel)
}

// ListRegistryMirrors returns the deployed local registry mirrors, sorted
// by name. Each mirror's metadata (name, URL, username) is read back from
// the container labels set at deploy time.
func ListRegistryMirrors() ([]RegistryMirror, error) {
	ctx := context.Background()

	cli, err := docker.NewDockerClient()
	if err != nil {
		return nil, errors.Wrap(err, "unable to create docker client")
	}

	mirrors, err := cli.ContainerList(ctx, client.ContainerListOptions{
		All:     true,
		Filters: registryMirrorLabelFilters(),
	})
	if err != nil {
		return nil, errors.Wrap(err, "unable to list registry mirrors")
	}

	result := make([]RegistryMirror, 0, len(mirrors.Items))
	for _, item := range mirrors.Items {
		name := item.Labels[constants.EducatesContainersMirrorLabelKey]

		url := item.Labels[constants.EducatesContainersURLLabelKey]
		if url == "" {
			url = name
		}

		result = append(result, RegistryMirror{
			Name:          name,
			URL:           url,
			Username:      item.Labels[constants.EducatesContainersUsernameLabelKey],
			Status:        item.Status,
			ContainerName: utils.GetContainerName(item),
		})
	}

	sort.Slice(result, func(i, j int) bool { return result[i].Name < result[j].Name })

	return result, nil
}

// UpdateRegistryK8SService points the registry Service in the default
// namespace at the local registry. Pods reach the local registry as
// registry.default.svc.cluster.local, which this Service, having no
// selector, maps to the registry container's address on the cluster network
// through its EndpointSlice. Nodes do not depend on it, as containerd reaches
// the registry by its container name. It is used when creating a cluster or
// deploying a registry, and updates both objects in place so the Service
// keeps its cluster IP.
func UpdateRegistryK8SService(k8sclient *kubernetes.Clientset) error {
	ctx := context.Background()

	cli, err := docker.NewDockerClient()

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

	registryInfo, err := cli.ContainerInspect(ctx, EducatesRegistryContainer, client.ContainerInspectOptions{})

	if err != nil {
		return errors.Wrapf(err, "unable to inspect container for registry")
	}

	kindNetwork, exists := registryInfo.Container.NetworkSettings.Networks[ClusterNetworkName]

	if !exists || kindNetwork == nil {
		return errors.New("registry is not attached to kind network")
	}

	if !kindNetwork.IPAddress.IsValid() {
		return errors.New("registry has no IPv4 address on kind network")
	}

	endpointAddresses := []string{kindNetwork.IPAddress.String()}

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

	servicesClient := k8sclient.CoreV1().Services("default")

	existingService, err := servicesClient.Get(ctx, service.Name, metav1.GetOptions{})

	if k8serrors.IsNotFound(err) {
		if _, err = servicesClient.Create(ctx, &service, metav1.CreateOptions{}); err != nil {
			return errors.Wrap(err, "unable to create registry service")
		}
	} else if err != nil {
		return errors.Wrap(err, "unable to read registry service")
	} else {
		existingService.Spec.Ports = service.Spec.Ports

		if _, err = servicesClient.Update(ctx, existingService, metav1.UpdateOptions{}); err != nil {
			return errors.Wrap(err, "unable to update registry service")
		}
	}

	endpointSliceClient := k8sclient.DiscoveryV1().EndpointSlices("default")

	existingEndpointSlice, err := endpointSliceClient.Get(ctx, endpointSlice.Name, metav1.GetOptions{})

	if k8serrors.IsNotFound(err) {
		if _, err = endpointSliceClient.Create(ctx, &endpointSlice, metav1.CreateOptions{}); err != nil {
			return errors.Wrap(err, "unable to create registry service endpoint")
		}
	} else if err != nil {
		return errors.Wrap(err, "unable to read registry service endpoint")
	} else {
		existingEndpointSlice.Labels = endpointSlice.Labels
		existingEndpointSlice.Ports = endpointSlice.Ports
		existingEndpointSlice.Endpoints = endpointSlice.Endpoints

		if _, err = endpointSliceClient.Update(ctx, existingEndpointSlice, metav1.UpdateOptions{}); err != nil {
			return errors.Wrap(err, "unable to update registry service endpoint")
		}
	}

	return nil
}

func PruneRegistry(p Progress) error {
	ctx := context.Background()

	report(p, "pruning registry storage")

	cli, err := docker.NewDockerClient()

	if err != nil {
		return errors.Wrap(err, "unable to create docker client")
	}

	containerID, _ := getContainerInfo(EducatesRegistryContainer)

	cmdStatement := []string{"registry", "garbage-collect", RegistryConfigTargetPath, "--delete-untagged=true"}

	optionsCreateExecuteScript := client.ExecCreateOptions{
		AttachStdout: false,
		AttachStderr: false,
		Cmd:          cmdStatement,
	}

	response, err := cli.ExecCreate(ctx, containerID, optionsCreateExecuteScript)
	if err != nil {
		return errors.Wrap(err, "unable to create exec command")
	}
	_, err = cli.ExecStart(ctx, response.ID, client.ExecStartOptions{})
	if err != nil {
		return errors.Wrap(err, "unable to exec command")
	}

	report(p, "registry pruned")

	return nil
}

/**
 * This function is used to get the container name of a registry mirror.
 */
func registryMirrorContainerName(mirrorConfig *MirrorConfig) string {
	return fmt.Sprintf("%s-mirror-%s", EducatesRegistryContainer, mirrorConfig.Mirror)
}

/**
 * This function returns the ID of the running container named exactly
 * name, or an empty string when no such container runs. Unlike a Docker
 * name filter, which matches substrings, it never picks another cluster's
 * node whose name contains this one.
 */
func runningContainerID(ctx context.Context, cli *client.Client, name string) string {
	info, err := cli.ContainerInspect(ctx, name, client.ContainerInspectOptions{})
	if err != nil || info.Container.State == nil || !info.Container.State.Running {
		return ""
	}
	return info.Container.ID
}

/**
 * This function is used to get the container id and status of a container.
 * If the container does not exist, it will return an empty string for the container id and status.
 */
func getContainerInfo(containerName string) (containerID string, status string) {
	ctx := context.Background()

	cli, err := docker.NewDockerClient()
	if err != nil {
		panic(err)
	}

	nameFilters := client.Filters{}.Add("name", containerName)

	resp, err := cli.ContainerList(ctx, client.ContainerListOptions{Filters: nameFilters})
	if err != nil {
		panic(err)
	}

	if len(resp.Items) > 0 {
		containerID = resp.Items[0].ID
		containerStatus := strings.Split(resp.Items[0].Status, " ")
		status = containerStatus[0]
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
