package selectors

import (
	corev1 "k8s.io/api/core/v1"

	secretsv1beta1 "github.com/educates/educates-training-platform/secrets-manager/api/v1beta1"
)

// defaultNamespaceNames are the matchNames of a rule whose nameSelector is
// absent or lists no names: every namespace but Kubernetes' own.
var defaultNamespaceNames = []string{"!kube-*"}

// NamespaceSelector decides which namespaces a rule targets: the Active
// namespaces that pass its selectors.
type NamespaceSelector struct {
	selector Selector
}

// Namespaces returns the selector of the targetNamespaces of a
// SecretCopier or SecretExporter rule.
func Namespaces(targets secretsv1beta1.TargetNamespaces) NamespaceSelector {
	return NamespacesOf(Fields{
		Names:  targets.NameSelector,
		UIDs:   targets.UIDSelector,
		Owners: targets.OwnerSelector,
		Labels: targets.LabelSelector,
	})
}

// NamespacesOf returns the selector of a rule's target namespaces, given as
// fields, for a kind whose target namespaces have a type of their own, such
// as a SecretInjector rule's.
func NamespacesOf(fields Fields) NamespaceSelector {
	return NamespaceSelector{selector: New(fields, defaultNamespaceNames)}
}

// Err returns why the selector is invalid, and nil when it is valid. An
// invalid selector selects no namespace.
func (s NamespaceSelector) Err() error {
	return s.selector.Err()
}

// Match reports whether the selector selects ns: whether ns is Active and
// passes every selector.
func (s NamespaceSelector) Match(ns *corev1.Namespace) bool {
	return ns.Status.Phase == corev1.NamespaceActive && s.selector.Match(ns)
}
