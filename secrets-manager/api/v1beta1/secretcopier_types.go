package v1beta1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// SecretCopierKind is the kind of a SecretCopier.
const SecretCopierKind = "SecretCopier"

// ReclaimPolicy says what happens to a SecretCopier rule's copies when the
// SecretCopier is deleted.
type ReclaimPolicy string

const (
	// ReclaimDelete has Kubernetes delete the copies with their SecretCopier.
	ReclaimDelete ReclaimPolicy = "Delete"

	// ReclaimRetain keeps the copies when their SecretCopier is deleted.
	ReclaimRetain ReclaimPolicy = "Retain"
)

// SecretReference names a Secret.
type SecretReference struct {
	// name is the Secret's name.
	Name string `json:"name"`

	// namespace is the Secret's namespace.
	Namespace string `json:"namespace"`
}

// TargetSecret shapes the copies of a SecretCopier or SecretExporter rule.
type TargetSecret struct {
	// name is the name of the copies, the source's when empty.
	// +optional
	Name string `json:"name,omitempty"`

	// labels are set on the copies, besides the source's labels, winning
	// over a source label with the same key.
	// +optional
	Labels map[string]string `json:"labels,omitempty"`

	// annotations are set on the copies. The source's annotations are
	// never copied. At most 32, none of them under secrets.educates.dev/.
	// +optional
	Annotations map[string]string `json:"annotations,omitempty"`

	// keys limits the copies to these keys of the source, those the source
	// has. The copies hold every key of the source when it is empty.
	// +optional
	Keys []string `json:"keys,omitempty"`
}

// CopyAuthorization gates a copy on a SecretImporter that holds the same
// shared secret.
type CopyAuthorization struct {
	// sharedSecret must equal the sharedSecret of the SecretImporter named
	// after the copy in the target namespace.
	// +optional
	SharedSecret string `json:"sharedSecret,omitempty"`
}

// SecretCopierRule copies one source Secret into the namespaces it
// selects.
type SecretCopierRule struct {
	// sourceSecret is the Secret copied.
	SourceSecret SecretReference `json:"sourceSecret"`

	// targetNamespaces selects the namespaces the Secret is copied into.
	// +optional
	TargetNamespaces TargetNamespaces `json:"targetNamespaces,omitempty"`

	// targetSecret shapes the copies.
	// +optional
	TargetSecret TargetSecret `json:"targetSecret,omitempty"`

	// copyAuthorization, when set, copies only where a SecretImporter
	// holding the same shared secret accepts the copy.
	// +optional
	CopyAuthorization *CopyAuthorization `json:"copyAuthorization,omitempty"`

	// reclaimPolicy says whether the copies are deleted with the
	// SecretCopier. The API server defaults it to Delete.
	// +optional
	ReclaimPolicy ReclaimPolicy `json:"reclaimPolicy,omitempty"`
}

// CopyName is the name of the rule's copies: targetSecret.name, or the
// source's name.
func (r *SecretCopierRule) CopyName() string {
	if r.TargetSecret.Name != "" {
		return r.TargetSecret.Name
	}
	return r.SourceSecret.Name
}

// SecretCopierSpec holds the rules of a SecretCopier.
type SecretCopierSpec struct {
	// rules each copy one Secret.
	// +optional
	Rules []SecretCopierRule `json:"rules,omitempty"`
}

// SecretCopier copies Secrets into the namespaces its rules select. It is
// cluster scoped.
//
// +kubebuilder:object:root=true
type SecretCopier struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec SecretCopierSpec `json:"spec,omitempty"`
}

// SecretCopierList is a list of SecretCopiers.
//
// +kubebuilder:object:root=true
type SecretCopierList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`

	Items []SecretCopier `json:"items"`
}

func init() {
	register(&SecretCopier{}, &SecretCopierList{})
}
