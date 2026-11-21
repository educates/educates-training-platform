package copyengine

import (
	"context"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

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
	// exists, or none of whose rules still targets the copy, and was
	// written as the asking rule's.
	Adopted Outcome = "Adopted"

	// InSync means the copy already held what it should, and nothing was
	// written.
	InSync Outcome = "InSync"

	// Agreed means another resource's rule that still targets the copy
	// manages it, from the same source, and nothing was written: that rule
	// keeps the copy and decides its shape.
	Agreed Outcome = "Agreed"

	// OwnershipConflict means the target is not the asking rule's to write:
	// no rule wrote it, its copier-rule annotation names no resource, or
	// another rule that still targets it wrote it from another source.
	// Nothing was written.
	OwnershipConflict Outcome = "OwnershipConflict"

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

// Wrote reports whether the outcome wrote the copy, which was not as it
// should be.
func (o Outcome) Wrote() bool {
	switch o {
	case Created, Updated, Recreated, Adopted:
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
	// its copier-rule annotation does, when Outcome is Agreed or an
	// OwnershipConflict with that rule. It is empty for an
	// OwnershipConflict with a target no rule wrote.
	ManagedBy string
}

// RuleState says whether the resource a copy's copier-rule annotation names
// exists, and whether one of its rules still targets the copy.
type RuleState int

const (
	// RuleUnknown means the annotation names no resource the lookup can
	// find.
	RuleUnknown RuleState = iota

	// RuleTargets means the resource exists, and one of its rules still
	// targets the copy: it selects the copy's namespace, names its copies
	// as the copy is named, and copies the source the copy's secret-name
	// annotation names.
	RuleTargets

	// RuleDropped means the resource exists, and none of its rules targets
	// the copy any more: the rule that wrote it stopped selecting its
	// namespace, was given another source or copy name, or was removed.
	RuleDropped

	// RuleGone means the resource no longer exists.
	RuleGone
)

// RuleLookup reports the state of the rule that wrote the copy named key,
// given the copy's copier-rule annotation, writer, and its secret-name
// annotation, source, which also places a resource that lives in its
// source's namespace.
type RuleLookup func(ctx context.Context, writer, source string, key types.NamespacedName) (RuleState, error)

// claim decides whether target, the target of c as the cache or the API
// server has it, is c.Rule's to write, and returns true when it is: when a
// rule of the resource c.Rule names wrote it from c's source, or when c's
// rule adopts it, the Result then saying Adopted. Otherwise the Result
// says why the target is left untouched.
//
// A target another rule wrote stays that rule's while the rule still
// targets it: c's rule agrees with it when both copy the same source, and
// is kept from the target as an ownership conflict otherwise. Once no rule
// of that resource targets the copy, or the resource is gone, the target is
// an orphan, which c's rule adopts.
func (e *Engine) claim(ctx context.Context, c Copy, target metav1.Object) (Result, bool, error) {
	annotations := target.GetAnnotations()
	writer, tracked := annotations[secretsv1beta1.CopierRuleAnnotation]
	source := annotations[secretsv1beta1.SecretNameAnnotation]
	sameSource := source == sourceName(c.Source)
	switch {
	case !tracked:
		return Result{Outcome: OwnershipConflict}, false, nil
	case writer == c.Rule && sameSource:
		return Result{}, true, nil
	}

	state, err := e.rules(ctx, writer, source, types.NamespacedName{Namespace: c.Namespace, Name: c.Name})
	switch {
	case err != nil:
		return Result{}, false, err
	case state == RuleGone, state == RuleDropped:
		return Result{Outcome: Adopted}, true, nil
	case state == RuleTargets && sameSource:
		return Result{Outcome: Agreed, ManagedBy: writer}, false, nil
	default:
		return Result{Outcome: OwnershipConflict, ManagedBy: writer}, false, nil
	}
}
