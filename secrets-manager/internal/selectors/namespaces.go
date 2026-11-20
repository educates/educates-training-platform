package selectors

import (
	corev1 "k8s.io/api/core/v1"

	secretsv1beta1 "github.com/educates/educates-training-platform/secrets-manager/api/v1beta1"
)

// defaultNamespaceNames are the matchNames of a rule without a
// nameSelector: every namespace but Kubernetes' own.
var defaultNamespaceNames = []string{"!kube-*"}

// NamespaceSelector decides which namespaces a SecretCopier or
// SecretExporter rule targets.
type NamespaceSelector struct {
	names []string
}

// Namespaces returns the selector of a rule's targetNamespaces.
func Namespaces(targets secretsv1beta1.TargetNamespaces) NamespaceSelector {
	names := defaultNamespaceNames
	if targets.NameSelector != nil {
		names = targets.NameSelector.MatchNames
	}
	return NamespaceSelector{names: names}
}

// Match reports whether the selector selects ns.
func (s NamespaceSelector) Match(ns *corev1.Namespace) bool {
	return MatchNames(s.names, ns.Name)
}
