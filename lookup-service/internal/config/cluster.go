package config

import (
	"fmt"

	lookupv1beta1 "github.com/educates/educates-training-platform/lookup-service/api/v1beta1"
)

// DefaultKubeconfigKey is the key of a kubeconfig Secret holding the
// kubeconfig when the ClusterConfig names none.
const DefaultKubeconfigKey = "config"

// Cluster is a cluster whose training portals the service offers,
// projected from its ClusterConfig.
type Cluster struct {
	Name string
	// UID is the uid of the ClusterConfig. A ClusterConfig created again
	// under the same name is a new cluster.
	UID    string
	Labels Labels
	// Kubeconfig is the kubeconfig a remote cluster is reached through. It
	// is nil for the local cluster, the one the service runs on, which the
	// service reaches through its mounted remote-access token.
	Kubeconfig *KubeconfigSource
}

// Local reports whether the cluster is the one the service runs on.
func (c Cluster) Local() bool {
	return c.Kubeconfig == nil
}

// KubeconfigSource is the kubeconfig a remote cluster is reached through,
// as its Secret in the config namespace holds it.
type KubeconfigSource struct {
	SecretName string
	Key        string
	// Context is the context to reach the cluster through, the kubeconfig's
	// current context when empty.
	Context string
	// Data is the kubeconfig, nil when Problem says why there is none.
	Data []byte
	// Problem says why the Secret gives no kubeconfig, such as a Secret
	// that does not exist.
	Problem string
}

func projectCluster(config *lookupv1beta1.ClusterConfig, secret func(name string) (map[string][]byte, bool)) Cluster {
	cluster := Cluster{Name: config.Name, UID: string(config.UID), Labels: projectLabels(config.Spec.Labels)}
	ref := config.KubeconfigSecretRef()
	if ref == nil {
		return cluster
	}

	source := &KubeconfigSource{SecretName: ref.Name, Key: ref.Key, Context: config.Spec.Credentials.Kubeconfig.Context}
	if source.Key == "" {
		source.Key = DefaultKubeconfigKey
	}
	data, found := secret(ref.Name)
	switch value, hasKey := data[source.Key]; {
	case !found:
		source.Problem = fmt.Sprintf("the kubeconfig Secret %s does not exist", ref.Name)
	case !hasKey:
		source.Problem = fmt.Sprintf("the kubeconfig Secret %s has no key %s", ref.Name, source.Key)
	default:
		source.Data = value
	}
	cluster.Kubeconfig = source
	return cluster
}

func projectLabels(labels []lookupv1beta1.Label) Labels {
	projected := make(Labels, 0, len(labels))
	for _, label := range labels {
		projected = append(projected, Label{Name: label.Name, Value: label.Value})
	}
	return projected
}

// ProjectLabels projects a resource's list of Educates labels, as
// unstructured objects hold it: each a name with an optional value.
// Entries without a name are left out.
func ProjectLabels(list []any) Labels {
	projected := make(Labels, 0, len(list))
	for _, item := range list {
		entry, ok := item.(map[string]any)
		if !ok {
			continue
		}
		name, ok := entry["name"].(string)
		if !ok {
			continue
		}
		label := Label{Name: name}
		if value, ok := entry["value"].(string); ok {
			label.Value = &value
		}
		projected = append(projected, label)
	}
	return projected
}
