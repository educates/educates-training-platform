package v1beta1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// SecretImporterKind is the kind of a SecretImporter.
const SecretImporterKind = "SecretImporter"

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

	// +optional
	Status SecretImporterStatus `json:"status,omitempty"`
}

// SecretImporterStatus says whether a SecretImporter has imported its
// Secret, from which exporter or copier, or why not.
type SecretImporterStatus struct {
	// observedGeneration is the generation of the spec the status
	// describes.
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// conditions holds the Ready condition.
	// +optional
	// +listType=map
	// +listMapKey=type
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// boundTo names the SecretExporter or SecretCopier the copy comes from:
	// the one that wrote it once imported, or one whose offer the importer
	// accepts while the copy is not there yet.
	// +optional
	BoundTo *ImportSource `json:"boundTo,omitempty"`

	// targetSecretName is the name of the Secret imported: the
	// SecretImporter's own.
	// +optional
	TargetSecretName string `json:"targetSecretName,omitempty"`
}

// ImportSource names the SecretExporter or SecretCopier a SecretImporter's
// copy comes from.
type ImportSource struct {
	// kind is SecretExporter or SecretCopier.
	Kind string `json:"kind"`

	// namespace is the SecretExporter's namespace, and is empty for a
	// SecretCopier, which is cluster scoped.
	// +optional
	Namespace string `json:"namespace,omitempty"`

	// name is the resource's name.
	Name string `json:"name"`
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
