package v1beta1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// SecretExporterKind is the kind of a SecretExporter.
const SecretExporterKind = "SecretExporter"

// ExporterCopyAuthorization holds the shared secret a SecretImporter must
// hold to accept an exported copy.
type ExporterCopyAuthorization struct {
	// sharedSecret must equal the sharedSecret of the SecretImporter named
	// after the copy in the target namespace.
	SharedSecret string `json:"sharedSecret"`
}

// SecretExporterRule offers the exported Secret to the namespaces it
// selects.
type SecretExporterRule struct {
	// targetNamespaces selects the namespaces the Secret is offered to.
	TargetNamespaces TargetNamespaces `json:"targetNamespaces"`

	// targetSecret shapes the copies.
	// +optional
	TargetSecret TargetSecret `json:"targetSecret,omitempty"`

	// copyAuthorization holds the shared secret a SecretImporter must hold
	// to accept the copy, the SecretExporter's UID when it is not set.
	// +optional
	CopyAuthorization *ExporterCopyAuthorization `json:"copyAuthorization,omitempty"`
}

// SecretExporterSpec holds the rules of a SecretExporter.
type SecretExporterSpec struct {
	// rules each offer the Secret to the namespaces they select.
	// +optional
	Rules []SecretExporterRule `json:"rules,omitempty"`
}

// SecretExporter offers the Secret of its name in its namespace to the
// namespaces its rules select, where a SecretImporter accepts it.
//
// +kubebuilder:object:root=true
type SecretExporter struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec SecretExporterSpec `json:"spec"`
}

// SecretExporterList is a list of SecretExporters.
//
// +kubebuilder:object:root=true
type SecretExporterList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`

	Items []SecretExporter `json:"items"`
}

func init() {
	register(&SecretExporter{}, &SecretExporterList{})
}
