package scenarios

import (
	"fmt"
	"maps"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

// object is a JSON object, as the spec of a secrets.educates.dev resource is
// written. Values may be any type that marshals to JSON; resource turns the
// whole into the form an unstructured object holds.
type object = map[string]any

// Reclaim policies of a SecretCopier rule.
const (
	retain          = "Retain"
	deleteWithOwner = "Delete"
)

// The counts a status summary and its rule entries hold.
const (
	countTargetNamespaces      = "targetNamespaces"
	countInSync                = "inSync"
	countConflicts             = "conflicts"
	countAwaitingAuthorization = "awaitingAuthorization"
	countFailures              = "failures"
)

// merged combines the fields of parts into one object, later parts winning.
func merged(parts ...object) object {
	result := object{}
	for _, part := range parts {
		maps.Copy(result, part)
	}
	return result
}

// rulesSpec is the spec of a SecretCopier, SecretExporter or SecretInjector
// with rules.
func rulesSpec(rules ...object) object {
	return object{"rules": rules}
}

// byName selects by name: globs, and exclusions written with a leading !.
// Without patterns, matchNames is an empty list.
func byName(patterns ...string) object {
	return object{"nameSelector": object{"matchNames": append([]string{}, patterns...)}}
}

// inNamespaces selects namespaces by their exact names.
func inNamespaces(namespaces ...*corev1.Namespace) object {
	return byName(namespaceNames(namespaces...)...)
}

// namespaceNames lists the names of namespaces.
func namespaceNames(namespaces ...*corev1.Namespace) []string {
	result := make([]string, 0, len(namespaces))
	for _, ns := range namespaces {
		result = append(result, ns.Name)
	}
	return result
}

// byLabels selects by matchLabels.
func byLabels(labels map[string]string) object {
	return byLabelSelector(labels)
}

// byExpressions selects by matchExpressions.
func byExpressions(expressions ...object) object {
	return byLabelSelector(nil, expressions...)
}

// byLabelSelector selects by matchLabels, when labels holds any, and by
// matchExpressions, when expressions holds any.
func byLabelSelector(labels map[string]string, expressions ...object) object {
	selector := object{}
	if len(labels) > 0 {
		selector["matchLabels"] = labels
	}
	if len(expressions) > 0 {
		selector["matchExpressions"] = expressions
	}
	return object{"labelSelector": selector}
}

// expression is a matchExpressions entry. Without values, it has no values
// field at all.
func expression(key, operator string, values ...string) object {
	entry := object{"key": key, "operator": operator}
	if len(values) > 0 {
		entry["values"] = values
	}
	return entry
}

// byUIDs selects by UID.
func byUIDs(uids ...types.UID) object {
	return object{"uidSelector": object{"matchUIDs": uids}}
}

// byOwners selects by owner reference.
func byOwners(owners ...object) object {
	return object{"ownerSelector": object{"matchOwners": owners}}
}

// ownerOf is a matchOwners entry.
func ownerOf(ownerAPIVersion, kind, name string, uid types.UID) object {
	return jsonObject(metav1.OwnerReference{APIVersion: ownerAPIVersion, Kind: kind, Name: name, UID: uid})
}

// namespaceOwner is a matchOwners entry naming the namespace owner.
func namespaceOwner(owner *corev1.Namespace) object {
	return ownerOf("v1", "Namespace", owner.Name, owner.UID)
}

// copierRule is a SecretCopier rule copying src into the namespaces targets
// selects, with the other fields of extra.
func copierRule(src *corev1.Secret, targets object, extra ...object) object {
	source := object{"sourceSecret": secretReference{Name: src.Name, Namespace: src.Namespace}}
	return merged(append([]object{targeting(targets), source}, extra...)...)
}

// secretReference is the sourceSecret of a SecretCopier rule.
type secretReference struct {
	Name      string `json:"name"`
	Namespace string `json:"namespace"`
}

// exporterRule is a SecretExporter rule offering its Secret to the
// namespaces targets selects, with the other fields of extra.
func exporterRule(targets object, extra ...object) object {
	return merged(append([]object{targeting(targets)}, extra...)...)
}

// injectorRule is a SecretInjector rule injecting the Secrets sources
// selects in the namespaces targets selects, with the other fields of extra.
func injectorRule(targets, sources object, extra ...object) object {
	return merged(append([]object{targeting(targets), {"sourceSecrets": sources}}, extra...)...)
}

// targeting is the targetNamespaces of a rule.
func targeting(targets object) object {
	return object{"targetNamespaces": targets}
}

// accounts is the serviceAccounts of an injector rule, selecting service
// accounts by their exact names.
func accounts(accountNames ...string) object {
	return accountsBy(byName(accountNames...))
}

// accountsBy is the serviceAccounts of an injector rule.
func accountsBy(selector object) object {
	return object{"serviceAccounts": selector}
}

// copyShape is the targetSecret of a SecretCopier or SecretExporter rule.
type copyShape struct {
	Name        string            `json:"name,omitempty"`
	Labels      map[string]string `json:"labels,omitempty"`
	Annotations map[string]string `json:"annotations,omitempty"`
	Keys        []string          `json:"keys,omitempty"`
}

// shaped sets the targetSecret of a rule.
func shaped(shape copyShape) object {
	return object{"targetSecret": shape}
}

// sharedSecret is the copyAuthorization of a rule or of a SecretImporter's
// spec.
func sharedSecret(secret string) object {
	return object{"copyAuthorization": object{"sharedSecret": secret}}
}

// reclaim sets the reclaimPolicy of a SecretCopier rule.
func reclaim(policy string) object {
	return object{"reclaimPolicy": policy}
}

// sourceNamespaces is the sourceNamespaces of a SecretImporter's spec.
func sourceNamespaces(patterns ...string) object {
	return object{"sourceNamespaces": byName(patterns...)}
}

// inSyncRatio is the summary.inSyncRatio of inSync targets out of matched.
func inSyncRatio(inSync, matched int) string {
	return fmt.Sprintf("%d/%d", inSync, matched)
}
