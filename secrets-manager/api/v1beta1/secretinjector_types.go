package v1beta1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// SourceSecrets selects the Secrets a SecretInjector rule injects. Every
// selector present must pass, and the rule gives at least one.
type SourceSecrets struct {
	// nameSelector selects Secrets by name.
	// +optional
	NameSelector *NameSelector `json:"nameSelector,omitempty"`

	// uidSelector selects Secrets by UID.
	// +optional
	UIDSelector *UIDSelector `json:"uidSelector,omitempty"`

	// ownerSelector selects Secrets by owner.
	// +optional
	OwnerSelector *OwnerSelector `json:"ownerSelector,omitempty"`

	// labelSelector selects Secrets by label.
	// +optional
	LabelSelector *metav1.LabelSelector `json:"labelSelector,omitempty"`
}

// ServiceAccounts selects the service accounts a SecretInjector rule
// injects into. Every selector present must pass.
type ServiceAccounts struct {
	// nameSelector selects service accounts by their exact names: unlike
	// the other name selectors, its names are not globs.
	// +optional
	NameSelector *NameSelector `json:"nameSelector,omitempty"`

	// labelSelector selects service accounts by label.
	// +optional
	LabelSelector *metav1.LabelSelector `json:"labelSelector,omitempty"`
}

// SecretInjectorRule adds the Secrets it selects to the service accounts it
// selects, in the namespaces it selects.
type SecretInjectorRule struct {
	// targetNamespaces selects the namespaces the rule injects in.
	// +optional
	TargetNamespaces TargetNamespaces `json:"targetNamespaces,omitempty"`

	// sourceSecrets selects the Secrets injected.
	SourceSecrets SourceSecrets `json:"sourceSecrets"`

	// serviceAccounts selects the service accounts injected into, every
	// service account of the namespace when it is not set.
	// +optional
	ServiceAccounts *ServiceAccounts `json:"serviceAccounts,omitempty"`
}

// SecretInjectorSpec holds the rules of a SecretInjector.
type SecretInjectorSpec struct {
	// rules each inject the Secrets they select.
	// +optional
	Rules []SecretInjectorRule `json:"rules,omitempty"`
}

// SecretInjector adds Secrets to the service accounts of the namespaces its
// rules select. It is cluster scoped.
//
// +kubebuilder:object:root=true
type SecretInjector struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec SecretInjectorSpec `json:"spec,omitempty"`
}

// SecretInjectorList is a list of SecretInjectors.
//
// +kubebuilder:object:root=true
type SecretInjectorList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`

	Items []SecretInjector `json:"items"`
}

func init() {
	register(&SecretInjector{}, &SecretInjectorList{})
}
