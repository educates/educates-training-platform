package v1beta1

import (
	"strings"

	"k8s.io/apimachinery/pkg/types"
)

// The tracking annotations on a copy, which name the rule that wrote it,
// the Secret it copies and the version of that Secret it was written from.
const (
	// CopierRuleAnnotation names the resource whose rule wrote a copy, as
	// <lowercased kind>/<name>, such as secretcopier/registry-credentials.
	CopierRuleAnnotation = "secrets.educates.dev/copier-rule"

	// SecretNameAnnotation names the Secret a copy copies, as
	// <namespace>/<name>.
	SecretNameAnnotation = "secrets.educates.dev/secret-name"

	// SourceVersionAnnotation is the resourceVersion of the Secret a copy
	// was last written from. A copy is written only when it differs from
	// what it should hold, so a change to the Secret that alters nothing
	// the copy holds leaves it at an older version.
	SourceVersionAnnotation = "secrets.educates.dev/source-version"
)

// CopierRule is the value of the copier-rule annotation naming the resource
// of kind and name.
func CopierRule(kind, name string) string {
	return strings.ToLower(kind) + "/" + name
}

// ParseCopierRule splits the value of a copier-rule annotation into the
// lowercased kind and the name of the resource it names, and reports
// whether it has that form.
func ParseCopierRule(value string) (kind, name string, ok bool) {
	kind, name, ok = strings.Cut(value, "/")
	return kind, name, ok && kind != "" && name != ""
}

// SecretName is the value of the secret-name annotation naming the Secret
// name in namespace.
func SecretName(namespace, name string) string {
	return namespace + "/" + name
}

// ParseSecretName returns the namespace and name of the Secret the value
// of a secret-name annotation names, and reports whether it has that form.
func ParseSecretName(value string) (types.NamespacedName, bool) {
	namespace, name, ok := strings.Cut(value, "/")
	return types.NamespacedName{Namespace: namespace, Name: name}, ok && namespace != "" && name != ""
}

// RuleResource names the resource whose rule wrote a copy.
//
// +kubebuilder:object:generate=false
type RuleResource struct {
	// Kind is SecretCopierKind or SecretExporterKind.
	Kind string

	// Key names the resource. A SecretCopier is cluster scoped. A
	// SecretExporter exports the Secret of its own name in its own
	// namespace, so it lives in the namespace of the Secret the copy
	// copies.
	Key types.NamespacedName
}

// RuleResourceOf resolves the values of a copy's copier-rule and
// secret-name annotations to the resource whose rule wrote the copy, and
// reports whether they name a SecretCopier or a SecretExporter.
func RuleResourceOf(copierRule, secretName string) (RuleResource, bool) {
	kind, name, ok := ParseCopierRule(copierRule)
	if !ok {
		return RuleResource{}, false
	}
	switch kind {
	case strings.ToLower(SecretCopierKind):
		return RuleResource{Kind: SecretCopierKind, Key: types.NamespacedName{Name: name}}, true
	case strings.ToLower(SecretExporterKind):
		source, ok := ParseSecretName(secretName)
		if !ok {
			return RuleResource{}, false
		}
		return RuleResource{Kind: SecretExporterKind, Key: types.NamespacedName{Namespace: source.Namespace, Name: name}},
			true
	default:
		return RuleResource{}, false
	}
}
