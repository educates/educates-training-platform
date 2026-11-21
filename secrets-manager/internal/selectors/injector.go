package selectors

import (
	"errors"
	"slices"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	secretsv1beta1 "github.com/educates/educates-training-platform/secrets-manager/api/v1beta1"
)

// errNoSourceSelector is why a SecretInjector rule whose sourceSecrets give
// no selector but an empty matchNames selects no Secret.
var errNoSourceSelector = errors.New("sourceSecrets select no Secrets: matchNames is empty, and no other " +
	"selector is given")

// SourceSecrets returns the selector of the Secrets a SecretInjector rule
// injects: names as globs with ! excludes, UIDs, owners and labels. An
// empty matchNames counts as absent, passing every name, so that the rule's
// other selectors decide; a rule left with no selector at all is invalid,
// and selects no Secret rather than every one.
func SourceSecrets(sources secretsv1beta1.SourceSecrets) Selector {
	s := New(Fields{
		Names:  sources.NameSelector,
		UIDs:   sources.UIDSelector,
		Owners: sources.OwnerSelector,
		Labels: sources.LabelSelector,
	}, nil)
	if s.err == nil && (sources.NameSelector == nil || len(sources.NameSelector.MatchNames) == 0) &&
		sources.UIDSelector == nil && sources.OwnerSelector == nil && sources.LabelSelector == nil {
		s.err = errNoSourceSelector
	}
	return s
}

// ServiceAccountSelector decides which service accounts a SecretInjector
// rule injects into.
type ServiceAccountSelector struct {
	// names are the exact names selected, every name when empty.
	names []string
	// labels selects by label only.
	labels Selector
}

// ServiceAccounts returns the selector of the serviceAccounts of a
// SecretInjector rule. Its names are exact, neither globs nor exclusions;
// an absent or empty matchNames passes every name, and absent
// serviceAccounts every service account.
func ServiceAccounts(accounts *secretsv1beta1.ServiceAccounts) ServiceAccountSelector {
	if accounts == nil {
		return ServiceAccountSelector{labels: New(Fields{}, nil)}
	}
	s := ServiceAccountSelector{labels: New(Fields{Labels: accounts.LabelSelector}, nil)}
	if accounts.NameSelector != nil {
		s.names = accounts.NameSelector.MatchNames
	}
	return s
}

// Err returns why the selector is invalid, and nil when it is valid. An
// invalid selector selects no service account.
func (s ServiceAccountSelector) Err() error {
	return s.labels.Err()
}

// Match reports whether account passes every selector.
func (s ServiceAccountSelector) Match(account metav1.Object) bool {
	return s.labels.Match(account) && (len(s.names) == 0 || slices.Contains(s.names, account.GetName()))
}
