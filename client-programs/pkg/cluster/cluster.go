package cluster

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	"github.com/pkg/errors"
	apiv1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
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
			return nil, errors.Wrap(err, "kubeconfig file does not exist")
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

// KindKubeconfigPath returns the kubeconfig file kind writes a local
// cluster's context into: kubeconfig when it is set, otherwise
// ~/.kube/config, whatever $KUBECONFIG says.
func KindKubeconfigPath(kubeconfig string) string {
	fallback := ""

	if home, err := os.UserHomeDir(); err == nil {
		fallback = filepath.Join(home, clientcmd.RecommendedHomeDir, clientcmd.RecommendedFileName)
	}

	return KubeconfigPath(kubeconfig, fallback)
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

// The loopback service is an ExternalName Service through which workshop
// sessions reach `educates serve-workshop` on the host, as
// localhost.<ingress domain>.
const (
	loopbackServiceName      = "loopback"
	loopbackServiceNamespace = "default"
	loopbackHostPrefix       = "localhost."
)

func CreateLoopbackService(k8sclient *kubernetes.Clientset, domain string) error {
	service := apiv1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name: loopbackServiceName,
		},
		Spec: apiv1.ServiceSpec{
			Type:         apiv1.ServiceTypeExternalName,
			ExternalName: loopbackHostPrefix + domain,
		},
	}

	servicesClient := k8sclient.CoreV1().Services(loopbackServiceNamespace)

	servicesClient.Delete(context.TODO(), loopbackServiceName, *metav1.NewDeleteOptions(0))

	_, err := servicesClient.Create(context.TODO(), &service, metav1.CreateOptions{})

	if err != nil {
		return errors.Wrap(err, "unable to create localhost loopback service")
	}

	return nil
}

// RefreshLoopbackService points the loopback service at
// localhost.<domain> when the cluster has one, and reports whether it
// changed it. Only a Service of the shape CreateLoopbackService creates
// is changed, so a cluster without one, or with an unrelated
// default/loopback Service, is left alone, as is one the caller is not
// allowed to read.
func RefreshLoopbackService(ctx context.Context, k8sclient kubernetes.Interface, domain string) (bool, error) {
	servicesClient := k8sclient.CoreV1().Services(loopbackServiceNamespace)

	service, err := servicesClient.Get(ctx, loopbackServiceName, metav1.GetOptions{})
	if apierrors.IsNotFound(err) || apierrors.IsForbidden(err) {
		return false, nil
	}
	if err != nil {
		return false, errors.Wrap(err, "unable to read localhost loopback service")
	}

	if service.Spec.Type != apiv1.ServiceTypeExternalName || !strings.HasPrefix(service.Spec.ExternalName, loopbackHostPrefix) {
		return false, nil
	}

	externalName := loopbackHostPrefix + domain
	if service.Spec.ExternalName == externalName {
		return false, nil
	}

	service.Spec.ExternalName = externalName
	if _, err := servicesClient.Update(ctx, service, metav1.UpdateOptions{}); err != nil {
		return false, errors.Wrap(err, "unable to update localhost loopback service")
	}

	return true, nil
}
