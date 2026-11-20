package v1beta1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Label is an Educates label, a name with an optional value. A label
// without a value matches the empty string.
type Label struct {
	// name is the label's name.
	Name string `json:"name"`

	// value is the label's value.
	// +optional
	Value *string `json:"value,omitempty"`
}

// KubeconfigSecretRef names the Secret, in the config namespace, that holds
// a cluster's kubeconfig.
type KubeconfigSecretRef struct {
	// name is the name of the Secret.
	Name string `json:"name"`

	// key is the key of the kubeconfig in the Secret, config when unset.
	// +optional
	Key string `json:"key,omitempty"`
}

// KubeconfigCredentials say how a cluster is reached through a kubeconfig.
type KubeconfigCredentials struct {
	// secretRef names the Secret holding the kubeconfig.
	// +optional
	SecretRef *KubeconfigSecretRef `json:"secretRef,omitempty"`

	// context is the kubeconfig context the cluster is reached through, the
	// kubeconfig's current context when unset.
	// +optional
	Context string `json:"context,omitempty"`
}

// ClusterCredentials say how a cluster is reached.
type ClusterCredentials struct {
	// kubeconfig gives the kubeconfig the cluster is reached through.
	Kubeconfig KubeconfigCredentials `json:"kubeconfig"`
}

// ClusterConfigSpec describes a cluster whose training portals the Lookup
// Service offers.
type ClusterConfigSpec struct {
	// labels are the cluster's labels, which tenants select clusters by.
	// +optional
	Labels []Label `json:"labels,omitempty"`

	// credentials say how the cluster is reached. Without a kubeconfig
	// Secret, the cluster is the one the Lookup Service runs on.
	// +optional
	Credentials *ClusterCredentials `json:"credentials,omitempty"`
}

// ClusterConfig is a cluster whose training portals the Lookup Service
// offers.
//
// +kubebuilder:object:root=true
type ClusterConfig struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec ClusterConfigSpec `json:"spec"`
}

// ClusterConfigList is a list of ClusterConfigs.
//
// +kubebuilder:object:root=true
type ClusterConfigList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`

	Items []ClusterConfig `json:"items"`
}

// KubeconfigSecretRef returns the reference to the Secret holding the
// cluster's kubeconfig, or nil when the cluster is the one the Lookup
// Service runs on.
func (c *ClusterConfig) KubeconfigSecretRef() *KubeconfigSecretRef {
	if c.Spec.Credentials == nil {
		return nil
	}
	return c.Spec.Credentials.Kubeconfig.SecretRef
}

func init() {
	register(&ClusterConfig{}, &ClusterConfigList{})
}
