package cluster

import (
	"context"
	"fmt"
	"os"

	"github.com/pkg/errors"
	apiv1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

type ClusterConfig struct {
	Kubeconfig string
	Context    string
}

func NewClusterConfig(kubeconfig string, context string) *ClusterConfig {
	return &ClusterConfig{kubeconfig, context}
}

func NewClusterConfigIfAvailable(kubeconfig string, context string) (*ClusterConfig, error) {
	clusterConfig := NewClusterConfig(kubeconfig, context)

	if err := IsClusterAvailableCheck(clusterConfig); err != nil {
		return nil, err
	}

	return clusterConfig, nil
}

// TODO: Use context and kubeconfig to build a client config.
func GetConfig(kubeconfigPath string, context string) (*rest.Config, error) {
	envVarName := clientcmd.RecommendedConfigPathEnvVar

	// No explicit overrides so attempt to use in cluster config first.
	if kubeconfigPath == "" && os.Getenv(envVarName) == "" {
		kubeconfig, err := rest.InClusterConfig()

		if err == nil {
			return kubeconfig, nil
		}
	}

	if kubeconfigPath != "" {
		if _, err := os.Stat(kubeconfigPath); os.IsNotExist(err) {
			// If kubeconfig is provided but not available, fail
			return nil, fmt.Errorf("kubeconfig file does not exist: %s", kubeconfigPath)
		}
	}

	loadingRules := clientcmd.NewDefaultClientConfigLoadingRules()
	loadingRules.ExplicitPath = kubeconfigPath

	configOverrides := &clientcmd.ConfigOverrides{}
	if context != "" {
		configOverrides.CurrentContext = context
	}

	return clientcmd.NewNonInteractiveDeferredLoadingClientConfig(loadingRules, configOverrides).ClientConfig()
}

func (o *ClusterConfig) GetConfig() (*rest.Config, error) {
	return GetConfig(o.Kubeconfig, o.Context)
}

func (o *ClusterConfig) GetClient() (*kubernetes.Clientset, error) {
	config, err := GetConfig(o.Kubeconfig, o.Context)

	if err != nil {
		return nil, err
	}

	return kubernetes.NewForConfig(config)
}

func (o *ClusterConfig) GetDynamicClient() (dynamic.Interface, error) {
	config, err := GetConfig(o.Kubeconfig, o.Context)

	if err != nil {
		return nil, err
	}

	return dynamic.NewForConfig(config)
}

func (o *ClusterConfig) GetDiscoveryClient() (*discovery.DiscoveryClient, error) {
	config, err := GetConfig(o.Kubeconfig, o.Context)

	if err != nil {
		return nil, err
	}

	return discovery.NewDiscoveryClientForConfig(config)
}

func KubeconfigPath(override string, fallback string) string {
	if override != "" {
		return override
	}

	// envvar := os.Getenv("KUBECONFIG")

	// if envvar != "" {
	// 	return envvar
	// }

	return fallback
}

func IsClusterAvailableCheck(clusterConfig *ClusterConfig) error {
	discoveryClient, err := clusterConfig.GetDiscoveryClient()
	if err != nil {
		return err
	}

	_, err = discoveryClient.ServerVersion()
	if err != nil {
		return errors.New("Cluster is not available or not reachable")
	}
	return nil
}

func CreateLoopbackService(k8sclient *kubernetes.Clientset, domain string) error {
	service := apiv1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name: "loopback",
		},
		Spec: apiv1.ServiceSpec{
			Type:         apiv1.ServiceTypeExternalName,
			ExternalName: fmt.Sprintf("localhost.%s", domain),
		},
	}

	servicesClient := k8sclient.CoreV1().Services("default")

	servicesClient.Delete(context.TODO(), "loopback", *metav1.NewDeleteOptions(0))

	_, err := servicesClient.Create(context.TODO(), &service, metav1.CreateOptions{})

	if err != nil {
		return errors.Wrap(err, "unable to create localhost loopback service")
	}

	return nil
}
