package v1beta1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

// NameSelector selects by name.
type NameSelector struct {
	// matchNames are glob patterns a name must match, and patterns with a
	// leading ! that it must not match.
	MatchNames []string `json:"matchNames"`
}

// UIDSelector selects by UID.
type UIDSelector struct {
	// matchUIDs are the UIDs selected.
	MatchUIDs []string `json:"matchUIDs"`
}

// OwnerSelector selects by owner reference.
type OwnerSelector struct {
	// matchOwners are the owners selected.
	MatchOwners []OwnerReference `json:"matchOwners"`
}

// OwnerReference names an owner by the fields an owner reference shares
// with it.
type OwnerReference struct {
	APIVersion string    `json:"apiVersion"`
	Kind       string    `json:"kind"`
	Name       string    `json:"name"`
	UID        types.UID `json:"uid"`
}

// TargetNamespaces selects the namespaces a SecretCopier or SecretExporter
// rule copies into, or a SecretInjector rule injects in. Every selector
// present must pass.
type TargetNamespaces struct {
	// nameSelector selects namespaces by name.
	// +optional
	NameSelector *NameSelector `json:"nameSelector,omitempty"`

	// uidSelector selects namespaces by UID.
	// +optional
	UIDSelector *UIDSelector `json:"uidSelector,omitempty"`

	// ownerSelector selects namespaces by owner.
	// +optional
	OwnerSelector *OwnerSelector `json:"ownerSelector,omitempty"`

	// labelSelector selects namespaces by label.
	// +optional
	LabelSelector *metav1.LabelSelector `json:"labelSelector,omitempty"`
}
