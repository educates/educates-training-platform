package v1beta1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// NameSelector selects resources by name.
type NameSelector struct {
	// matchNames are glob patterns. A name matching any of them is
	// selected, and an empty list selects every name.
	MatchNames []string `json:"matchNames"`
}

// LabelSelectorRequirement is a condition on one label.
type LabelSelectorRequirement struct {
	// key is the label's name.
	Key string `json:"key"`

	// operator is In, NotIn, Exists or DoesNotExist.
	Operator string `json:"operator"`

	// values are the values In and NotIn compare the label's value with.
	// +optional
	Values []string `json:"values,omitempty"`
}

// LabelSelector selects resources by their Educates labels.
type LabelSelector struct {
	// matchLabels are labels a resource must have, with these values.
	// +optional
	MatchLabels map[string]string `json:"matchLabels,omitempty"`

	// matchExpressions are conditions a resource's labels must all meet.
	// +optional
	MatchExpressions []LabelSelectorRequirement `json:"matchExpressions,omitempty"`
}

// ResourceSelector selects resources by name and by label. A resource must
// match both, and a selector left out selects every resource.
type ResourceSelector struct {
	// nameSelector selects resources by name.
	// +optional
	NameSelector *NameSelector `json:"nameSelector,omitempty"`

	// labelSelector selects resources by their Educates labels.
	// +optional
	LabelSelector *LabelSelector `json:"labelSelector,omitempty"`
}

// TenantConfigSpec describes which training portals a tenant reaches.
type TenantConfigSpec struct {
	// clusters selects the clusters whose portals the tenant reaches.
	// +optional
	Clusters *ResourceSelector `json:"clusters,omitempty"`

	// portals selects the portals, on those clusters, the tenant reaches.
	// +optional
	Portals *ResourceSelector `json:"portals,omitempty"`
}

// TenantConfig is a tenant of the Lookup Service: the training portals its
// API clients may offer workshops from.
//
// +kubebuilder:object:root=true
type TenantConfig struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec TenantConfigSpec `json:"spec"`
}

// TenantConfigList is a list of TenantConfigs.
//
// +kubebuilder:object:root=true
type TenantConfigList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`

	Items []TenantConfig `json:"items"`
}

func init() {
	register(&TenantConfig{}, &TenantConfigList{})
}
