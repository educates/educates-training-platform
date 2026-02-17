package registry

import (
	"context"
	_ "embed"
	"fmt"

	"github.com/docker/docker/api/types/container"
	"github.com/educates/educates-training-platform/client-programs/pkg/constants"
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

const hostRegistryTomlTemplate = `[host."http://%s:5000"]`

// Registry represents the educates image registry container.
type Registry struct {
	baseContainer
	k8sClient *kubernetes.Clientset
}

// NewRegistry creates a new Registry instance.
func NewRegistry(bindIP string, k8sClient *kubernetes.Clientset) *Registry {
	return &Registry{
		baseContainer: baseContainer{
			containerName: constants.EducatesRegistryContainer,
			bindIP:        bindIP,
			hostPort:      "5001",
			labels: newRegistryContainerLabels(),
		},
		k8sClient: k8sClient,
	}
}

// DeployAndLinkToCluster deploys the registry and links it to the cluster.
// It is used when creating a new local cluster.
func (r *Registry) DeployAndLinkToCluster() error {
	err := r.Deploy()
	if err != nil {
		return err
	}

	// This is needed to make containerd use the local registry
	if err = addRegistryConfigToKindNodes("localhost:5001", fmt.Sprintf(hostRegistryTomlTemplate, constants.EducatesRegistryContainer)); err != nil {
		return err
	}
	if err = addRegistryConfigToKindNodes("registry.default.svc.cluster.local", fmt.Sprintf(hostRegistryTomlTemplate, constants.EducatesRegistryContainer)); err != nil {
		return err
	}

	// This is needed so that kubernetes nodes can pull images from the local registry
	if err = r.documentLocalRegistry(); err != nil {
		return err
	}

	return nil
}

// Deploy creates the registry container without linking to cluster.
// It is used when creating a new local registry standalone.
func (r *Registry) Deploy() error {
	fmt.Println("Deploying local image registry")

	cli, err := utils.NewDockerClient()
	if err != nil {
		return errors.Wrap(err, "unable to create docker client")
	}

	exists, containerID, _ := r.containerExists(cli)
	if !exists {
		if err = r.pullRegistryImage(cli); err != nil {
			return err
		}

		containerID, err = r.createContainer(cli, context.Background())
		if err != nil {
			return errors.Wrap(err, "cannot create registry container")
		}
	}


	if err = r.ensureNetwork(cli, constants.EducatesNetworkName); err != nil {
		return err
	}

	if err = r.startContainer(cli, context.Background(), containerID); err != nil {
		return errors.Wrap(err, "unable to start registry container")
	}

	if err = r.connectToNetwork(cli, constants.EducatesNetworkName, ""); err != nil {
		return errors.Wrap(err, fmt.Sprintf("unable to connect registry to %s network", constants.EducatesNetworkName))
	}

	registryIP, err := resolveLocalRegistryIP(cli)

	if err != nil {
		return errors.Wrap(err, "failed to resolve local registry IP")
	}

	if err = r.connectToNetwork(cli, constants.ClusterNetworkName, registryIP); err != nil {
		return errors.Wrap(err, fmt.Sprintf("failed to connect registry to %s network at IP:%s", constants.ClusterNetworkName, registryIP	))
	}

	return nil
}

// DeleteAndUnlinkFromCluster removes the registry and cleans up cluster configuration.
// For the registry, this is the same as Delete since the cluster config is tied to the cluster lifecycle.
func (r *Registry) DeleteAndUnlinkFromCluster() error {
	return r.Delete()
}

// Delete removes the registry container.
func (r *Registry) Delete() error {
	fmt.Println("Deleting local image registry")

	cli, err := utils.NewDockerClient()
	if err != nil {
		return errors.Wrap(err, "unable to create docker client")
	}

	return r.stopAndRemoveContainer(cli)
}

// documentLocalRegistry creates the ConfigMap that documents the local registry in the cluster.
func (r *Registry) documentLocalRegistry() error {
	if r.k8sClient == nil {
		return nil
	}

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

	if _, err := r.k8sClient.CoreV1().ConfigMaps("kube-public").Get(context.TODO(), "local-registry-hosting", metav1.GetOptions{}); k8serrors.IsNotFound(err) {
		_, err = r.k8sClient.CoreV1().ConfigMaps("kube-public").Create(context.TODO(), configMap, metav1.CreateOptions{})
		if err != nil {
			return errors.Wrap(err, "document local registry: unable to create local registry hosting config map")
		}
	} else {
		_, err = r.k8sClient.CoreV1().ConfigMaps("kube-public").Update(context.TODO(), configMap, metav1.UpdateOptions{})
		if err != nil {
			return errors.Wrap(err, "document local registry: unable to update local registry hosting config map")
		}
	}

	return nil
}

// UpdateK8SService updates the registry k8s service.
// It is used when creating a cluster or a registry in order to update the k8s service to point to the new registry.
func (r *Registry) UpdateK8SService() error {
	if r.k8sClient == nil {
		return errors.New("kubernetes client is required for UpdateK8SService")
	}

	ctx := context.Background()

	cli, err := utils.NewDockerClient()
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

	registryInfo, err := cli.ContainerInspect(ctx, constants.EducatesRegistryContainer)
	if err != nil {
		return errors.Wrapf(err, "unable to inspect container for registry")
	}

	network, exists := registryInfo.NetworkSettings.Networks[constants.ClusterNetworkName]
	if !exists {
		return errors.New(fmt.Sprintf("registry is not attached to %s network", constants.ClusterNetworkName))
	}

	endpointAddresses := []string{network.IPAddress}

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

	endpointSliceClient := r.k8sClient.DiscoveryV1().EndpointSlices("default")

	endpointSliceClient.Delete(context.TODO(), "registry-1", *metav1.NewDeleteOptions(0))

	servicesClient := r.k8sClient.CoreV1().Services("default")

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

// Prune runs garbage collection on the registry.
func (r *Registry) Prune() error {
	ctx := context.Background()

	fmt.Println("Pruning local image registry")

	cli, err := utils.NewDockerClient()
	if err != nil {
		return errors.Wrap(err, "unable to create docker client")
	}

	exists, containerID, _ := r.containerExists(cli)
	if !exists || containerID == "" {
		return nil
	}

	cmdStatement := []string{"registry", "garbage-collect", constants.RegistryConfigTargetPath, "--delete-untagged=true"}

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

// Compile-time check that Registry implements ContainerManager
var _ ContainerManager = (*Registry)(nil)
