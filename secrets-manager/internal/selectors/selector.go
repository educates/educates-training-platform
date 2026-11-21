package selectors

import (
	"fmt"
	"slices"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"

	secretsv1beta1 "github.com/educates/educates-training-platform/secrets-manager/api/v1beta1"
)

// Fields are the selectors a rule gives for one kind of object, such as
// its targetNamespaces or the sourceSecrets of a SecretInjector rule. A nil
// field is a selector the rule does not give.
type Fields struct {
	Names  *secretsv1beta1.NameSelector
	UIDs   *secretsv1beta1.UIDSelector
	Owners *secretsv1beta1.OwnerSelector
	Labels *metav1.LabelSelector
}

// Selector decides whether an object passes the selectors of a rule. Every
// selector the rule gives must pass. A selector that is invalid, such as a
// matchExpressions entry using In or NotIn without values, matches nothing.
type Selector struct {
	names  []string
	uids   []string
	owners []secretsv1beta1.OwnerReference
	// labels is nil when the rule gives no labelSelector.
	labels labels.Selector
	err    error
}

// New returns the selector of fields. When the rule gives no matchNames, or
// an empty list, defaultNames stand in for them; nil defaultNames pass
// every name. An empty matchUIDs or matchOwners passes every object.
func New(fields Fields, defaultNames []string) Selector {
	s := Selector{names: defaultNames}
	if fields.Names != nil && len(fields.Names.MatchNames) > 0 {
		s.names = fields.Names.MatchNames
	}
	if fields.UIDs != nil {
		s.uids = fields.UIDs.MatchUIDs
	}
	if fields.Owners != nil {
		s.owners = fields.Owners.MatchOwners
	}
	if fields.Labels != nil {
		selector, err := metav1.LabelSelectorAsSelector(fields.Labels)
		if err != nil {
			s.err = fmt.Errorf("invalid labelSelector: %w", err)
		}
		s.labels = selector
	}
	return s
}

// Err returns why the selector is invalid, and nil when it is valid.
func (s Selector) Err() error {
	return s.err
}

// Match reports whether obj passes every selector. An invalid selector
// matches nothing.
func (s Selector) Match(obj metav1.Object) bool {
	return s.err == nil &&
		MatchNames(s.names, obj.GetName()) &&
		s.matchUID(obj) &&
		s.matchOwners(obj) &&
		(s.labels == nil || s.labels.Matches(labels.Set(obj.GetLabels())))
}

// matchUID reports whether obj has one of the selected UIDs, when the
// selector lists any.
func (s Selector) matchUID(obj metav1.Object) bool {
	return len(s.uids) == 0 || slices.Contains(s.uids, string(obj.GetUID()))
}

// matchOwners reports whether one of the owner references of obj names a
// selected owner, on its apiVersion, kind, name and UID, when the selector
// lists any.
func (s Selector) matchOwners(obj metav1.Object) bool {
	if len(s.owners) == 0 {
		return true
	}
	for _, ref := range obj.GetOwnerReferences() {
		if slices.Contains(s.owners, secretsv1beta1.OwnerReference{
			APIVersion: ref.APIVersion, Kind: ref.Kind, Name: ref.Name, UID: ref.UID,
		}) {
			return true
		}
	}
	return false
}
