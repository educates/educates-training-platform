package v1beta1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// SourceNamespaces selects the namespaces a SecretImporter accepts copies
// from.
type SourceNamespaces struct {
	// nameSelector selects namespaces by name.
	// +optional
	NameSelector *NameSelector `json:"nameSelector,omitempty"`
}

// ImporterCopyAuthorization holds the shared secret a SecretImporter
// accepts copies with.
type ImporterCopyAuthorization struct {
	// sharedSecret must equal the shared secret of the rule that copies.
	SharedSecret string `json:"sharedSecret"`
}

// SecretImporterSpec says which copies a SecretImporter accepts.
type SecretImporterSpec struct {
	// sourceNamespaces selects the namespaces copies are accepted from.
	// +optional
	SourceNamespaces *SourceNamespaces `json:"sourceNamespaces,omitempty"`

	// copyAuthorization holds the shared secret copies are accepted with.
	// +optional
	CopyAuthorization *ImporterCopyAuthorization `json:"copyAuthorization,omitempty"`
}

// SecretImporter accepts the copy of its name into its namespace.
//
// +kubebuilder:object:root=true
type SecretImporter struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec SecretImporterSpec `json:"spec"`
}

// SecretImporterList is a list of SecretImporters.
//
// +kubebuilder:object:root=true
type SecretImporterList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`

	Items []SecretImporter `json:"items"`
}

func init() {
	register(&SecretImporter{}, &SecretImporterList{})
}
