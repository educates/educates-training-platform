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

// CopyName is the name of the copies of a rule of the SecretExporter
// exporter names: targetSecret.name, or the exported Secret's name, which
// is the SecretExporter's.
func (r *SecretExporterRule) CopyName(exporter string) string {
	if r.TargetSecret.Name != "" {
		return r.TargetSecret.Name
	}
	return exporter
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

	// +optional
	Status SecretExporterStatus `json:"status,omitempty"`
}

// SecretExporterStatus says whether a SecretExporter is in sync, and why
// not.
type SecretExporterStatus struct {
	// observedGeneration is the generation of the spec the status
	// describes.
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// conditions holds the Ready condition.
	// +optional
	// +listType=map
	// +listMapKey=type
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// sourceExists reports whether the exported Secret exists.
	SourceExists bool `json:"sourceExists"`

	// summary counts the targets of every rule.
	// +optional
	Summary *CopySummary `json:"summary,omitempty"`

	// rules counts the targets of each rule, in the order of the spec's
	// rules.
	// +optional
	Rules []TargetCounts `json:"rules,omitempty"`
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
