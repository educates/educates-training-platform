package secretinjector

import (
	"errors"
)

// Outcome is what one reconcile of a SecretInjector found and did, rule by
// rule.
type Outcome struct {
	// Rules holds the outcome of each rule, in the order of the spec's
	// rules.
	Rules []RuleOutcome

	// Conflicted reports that a service account changed while it was
	// written, so that the SecretInjector must be reconciled again. Such a
	// service account counts neither as in sync nor as a failure.
	Conflicted bool
}

// RuleOutcome is what one rule of a SecretInjector found and did.
type RuleOutcome struct {
	// InvalidSelector is why the rule's selectors are invalid, and nil when
	// they are valid. A rule with an invalid selector selects nothing.
	InvalidSelector error

	// TargetNamespaces counts the namespaces the rule selects.
	TargetNamespaces int

	// ServiceAccountsMatched counts the service accounts the rule selects
	// in those namespaces.
	ServiceAccountsMatched int

	// ServiceAccountsInSync counts the selected service accounts that hold
	// every name the rule injects into them.
	ServiceAccountsInSync int

	// InjectionsInSync counts the names the rule injects that the selected
	// service accounts hold, once for each service account.
	InjectionsInSync int

	// Failures lists the selected service accounts the rule could not bring
	// in sync, and the selected namespaces it could not look in, and why.
	Failures []Failure
}

// Failure is a service account a rule could not bring in sync, or a
// namespace it could not look in.
type Failure struct {
	Namespace string
	// ServiceAccount is empty when the rule could not look in the
	// namespace.
	ServiceAccount string
	Err            error
}

// Err joins the errors of every rule's failures, and is nil when no rule
// failed.
func (o Outcome) Err() error {
	var errs []error
	for _, rule := range o.Rules {
		for _, failure := range rule.Failures {
			errs = append(errs, failure.Err)
		}
	}
	return errors.Join(errs...)
}
