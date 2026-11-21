package v1beta1

import (
	"strings"
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
