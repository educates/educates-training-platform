package v1beta1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// SecretInjectorKind is the kind of a SecretInjector.
const SecretInjectorKind = "SecretInjector"

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

	// +optional
	Status SecretInjectorStatus `json:"status,omitempty"`
}

// SecretInjectorStatus says whether a SecretInjector is in sync, and why
// not.
type SecretInjectorStatus struct {
	// observedGeneration is the generation of the spec the status
	// describes.
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// conditions holds the Ready condition.
	// +optional
	// +listType=map
	// +listMapKey=type
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// summary counts the injections of every rule.
	// +optional
	Summary *InjectionSummary `json:"summary,omitempty"`

	// rules counts the injections of each rule, in the order of the spec's
	// rules.
	// +optional
	Rules []InjectionCounts `json:"rules,omitempty"`
}

// InjectionCounts count what a SecretInjector, or one of its rules,
// matches and injects. An injection is the Secrets a rule selects in a
// namespace, added to one service account the rule selects there.
type InjectionCounts struct {
	// targetNamespaces counts the target namespaces matched.
	TargetNamespaces int32 `json:"targetNamespaces"`

	// serviceAccountsMatched counts the service accounts matched in them.
	ServiceAccountsMatched int32 `json:"serviceAccountsMatched"`

	// injectionsInSync counts the matched service accounts that hold every
	// Secret the rule injects into them.
	InjectionsInSync int32 `json:"injectionsInSync"`

	// failures counts the injections that failed, and are retried, and the
	// target namespaces that could not be read.
	Failures int32 `json:"failures"`
}

// InjectionSummary counts what every rule of a SecretInjector matches and
// injects.
type InjectionSummary struct {
	InjectionCounts `json:",inline"`

	// inSyncRatio is injectionsInSync out of serviceAccountsMatched, as
	// kubectl get shows it, such as 5/6.
	InSyncRatio string `json:"inSyncRatio"`
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
