package copyengine

import (
	"context"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	secretsv1beta1 "github.com/educates/educates-training-platform/secrets-manager/api/v1beta1"
)

// Outcome says what converging a copy did, or why it wrote nothing.
type Outcome string

const (
	// Created means the copy did not exist and was written.
	Created Outcome = "Created"

	// Updated means the copy differed from what it should hold and was
	// written.
	Updated Outcome = "Updated"

	// Recreated means the copy differed from the source in a field it
	// cannot change, and was deleted and written anew.
	Recreated Outcome = "Recreated"

	// Adopted means the copy was written by a resource that no longer
	// exists, and was written as the asking rule's.
	Adopted Outcome = "Adopted"

	// InSync means the copy already held what it should, and nothing was
	// written.
	InSync Outcome = "InSync"

	// Agreed means another existing resource's rule manages the copy, from
	// the same source, and nothing was written: that rule keeps the copy
	// and decides its shape.
	Agreed Outcome = "Agreed"

	// Conflict means the target is not the asking rule's to write: no rule
	// wrote it, or another existing resource's rule wrote it from another
	// source. Nothing was written.
	Conflict Outcome = "Conflict"

	// TargetDeleting means the target is being deleted, and nothing was
	// written: its deletion brings the next attempt.
	TargetDeleting Outcome = "TargetDeleting"
)

// Converged reports whether, after the outcome, the target is a copy of
// the source that a rule keeps current: the asking rule, or one it agrees
// with.
func (o Outcome) Converged() bool {
	switch o {
	case Created, Updated, Recreated, Adopted, InSync, Agreed:
		return true
	default:
		return false
	}
}

// Result is what converging one copy found.
type Result struct {
	// Outcome says what converging the copy did, or why it wrote nothing.
	Outcome Outcome

	// ManagedBy names the other resource whose rule manages the target, as
	// its copier-rule annotation does, when Outcome is Agreed or a
	// Conflict with that rule. It is empty for a Conflict with a target no
	// rule wrote.
	ManagedBy string
}

// RuleState says whether the resource a copier-rule annotation names
// exists.
type RuleState int

const (
	// RuleUnknown means the annotation names no resource the lookup can
	// find.
	RuleUnknown RuleState = iota

	// RuleExists means the resource exists.
	RuleExists

	// RuleGone means the resource no longer exists.
	RuleGone
)

// RuleLookup reports the state of the resource that rule, the value of a
// target's copier-rule annotation, names. source is the target's
// secret-name annotation, <namespace>/<name>, which places a resource that
// lives in its source's namespace.
type RuleLookup func(ctx context.Context, rule, source string) (RuleState, error)

// claim decides whether target, the target of c as the cache or the API
// server has it, is c.Rule's to write, and returns true when it is: when
// c.Rule wrote it, or adopts it, the Result then saying Adopted. Otherwise
// the Result says why the target is left untouched.
func (e *Engine) claim(ctx context.Context, c Copy, target metav1.Object) (Result, bool, error) {
	annotations := target.GetAnnotations()
	writer, tracked := annotations[secretsv1beta1.CopierRuleAnnotation]
	switch {
	case !tracked:
		return Result{Outcome: Conflict}, false, nil
	case writer == c.Rule:
		return Result{}, true, nil
	}

	source := annotations[secretsv1beta1.SecretNameAnnotation]
	state, err := e.rules(ctx, writer, source)
	switch {
	case err != nil:
		return Result{}, false, err
	case state == RuleGone:
		return Result{Outcome: Adopted}, true, nil
	case state == RuleExists && source == sourceName(c.Source):
		return Result{Outcome: Agreed, ManagedBy: writer}, false, nil
	default:
		return Result{Outcome: Conflict, ManagedBy: writer}, false, nil
	}
}
