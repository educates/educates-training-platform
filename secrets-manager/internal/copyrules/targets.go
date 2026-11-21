package copyrules

import (
	"context"
	"errors"
	"fmt"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	secretsv1beta1 "github.com/educates/educates-training-platform/secrets-manager/api/v1beta1"
	"github.com/educates/educates-training-platform/secrets-manager/internal/convergence"
	"github.com/educates/educates-training-platform/secrets-manager/internal/copyengine"
	"github.com/educates/educates-training-platform/secrets-manager/internal/selectors"
)

// Rule is one rule's offer of copies of its source into the namespaces it
// selects.
type Rule struct {
	// Source is the source the rule copies, and is nil when the source is
	// not copied, because it does not exist or its namespace is being
	// deleted: the rule's targets are then only listed.
	Source *copyengine.Source

	// SourceNamespace is the namespace of the source, which the rule never
	// copies into.
	SourceNamespace string

	// Namespaces selects the namespaces the rule copies into. It is valid.
	Namespaces selectors.NamespaceSelector

	// Offer is the rule's offer of a copy into each of them, which the
	// SecretImporter named after the copy there may refuse.
	Offer Offer

	// Copy shapes the copies: their Name, the Rule naming the resource
	// whose rule it is, and the Labels, Annotations and Keys the rule gives
	// them. Its Source, Namespace and Owner are set for each target.
	Copy copyengine.Copy

	// Owner returns the owner of a copy, given importer, the SecretImporter
	// named after the copy in its namespace, or nil when there is none.
	Owner func(importer *secretsv1beta1.SecretImporter) *copyengine.Owner
}

// Target is what converging one copy of a rule's source found.
type Target struct {
	// Namespace is the namespace of the copy.
	Namespace string

	// Refusal says why the target does not accept the copy, when it awaits
	// authorization: no SecretImporter that the rule's offer requires is
	// named after the copy there, or the one there holds another shared
	// secret or does not admit the source's namespace. Nothing is converged
	// then.
	Refusal Refusal

	// Result says what converging the copy did, or why it wrote nothing,
	// when the target accepts the copy and converging it did not fail.
	Result copyengine.Result

	// Err is the failure to read the SecretImporter or to converge the
	// copy, when either failed. A write conflict is the API server's
	// Conflict error.
	Err error
}

// Converger converges the copies the rules of one resource offer, during
// one reconcile of the resource, and collects their failures and write
// conflicts for the reconcile's result.
type Converger struct {
	cache  client.Reader
	engine *copyengine.Engine

	failures   []error
	conflicted bool
}

// NewConverger returns a Converger that reads SecretImporters from cache
// and converges copies with engine, for one reconcile.
func NewConverger(cache client.Reader, engine *copyengine.Engine) *Converger {
	return &Converger{cache: cache, engine: engine}
}

// Converge converges the copy rule offers into each namespace of
// namespaces it selects, other than its source's own, where the target
// accepts it, and returns what it found in each, in the order of
// namespaces. A failure on one target never stops the others.
func (c *Converger) Converge(ctx context.Context, rule *Rule, namespaces []corev1.Namespace) []Target {
	log := ctrl.LoggerFrom(ctx).WithValues("name", rule.Copy.Name)
	var targets []Target
	for i := range namespaces {
		ns := &namespaces[i]
		if ns.Name == rule.SourceNamespace || !rule.Namespaces.Match(ns) {
			continue
		}
		target := Target{Namespace: ns.Name}
		if rule.Source != nil {
			target = c.converge(ctx, log.WithValues("namespace", ns.Name), rule, ns.Name)
		}
		targets = append(targets, target)
	}
	return targets
}

// converge converges the copy rule offers into namespace, when the target
// accepts it.
func (c *Converger) converge(ctx context.Context, log logr.Logger, rule *Rule, namespace string) Target {
	target := Target{Namespace: namespace}
	importer, err := Importer(ctx, c.cache, types.NamespacedName{Namespace: namespace, Name: rule.Copy.Name})
	if err != nil {
		target.Err = fmt.Errorf("reading the SecretImporter: %w", err)
		c.record(rule, target)
		return target
	}
	if target.Refusal = Authorize(importer, rule.Offer); target.Refusal != Accepted {
		log.V(1).Info("The target does not accept the copy: awaiting authorization", "refusal", target.Refusal)
		return target
	}

	desired := rule.Copy
	desired.Source, desired.Namespace, desired.Owner = rule.Source, namespace, rule.Owner(importer)
	target.Result, target.Err = c.engine.Converge(ctx, desired)
	logTarget(log, target.Result, target.Err)
	if target.Err == nil && target.Result.Outcome.Wrote() {
		convergence.Repaired(ctx)
	}
	c.record(rule, target)
	return target
}

// record collects the failure or write conflict of target, a target of
// rule.
func (c *Converger) record(rule *Rule, target Target) {
	switch {
	case target.Err == nil:
	case copyengine.IsWriteConflict(target.Err):
		c.conflicted = true
	default:
		c.failures = append(c.failures, fmt.Errorf("copying Secret %s into namespace %s as %s: %w",
			secretsv1beta1.SecretName(rule.Source.GetNamespace(), rule.Source.GetName()), target.Namespace,
			rule.Copy.Name, target.Err))
	}
}

// Fail collects err, a failure of the reconcile other than a target's, such
// as reading a source.
func (c *Converger) Fail(err error) {
	if err != nil {
		c.failures = append(c.failures, err)
	}
}

// Conflicted reports whether the write of a copy conflicted with another
// writer's. The resource's status then waits for the reconcile that
// retries it.
func (c *Converger) Conflicted() bool {
	return c.conflicted
}

// Result is the result of the reconcile. The failures are its error, which
// the work queue retries with backoff. A write conflict is no failure: it
// is retried by requeueing.
func (c *Converger) Result() (ctrl.Result, error) {
	switch {
	case len(c.failures) > 0:
		return ctrl.Result{}, errors.Join(c.failures...)
	case c.conflicted:
		return convergence.RetryConflict(), nil
	default:
		return ctrl.Result{}, nil
	}
}

// logTarget logs what converging one copy did, when it wrote or was kept
// from writing.
func logTarget(log logr.Logger, result copyengine.Result, err error) {
	switch {
	case copyengine.IsWriteConflict(err):
		log.V(1).Info("Writing the copy conflicted with another writer: retrying")
	case err != nil:
		// The failure is returned, and logged, with the reconcile's error.
	case result.Outcome == copyengine.OwnershipConflict:
		log.V(1).Info("Leaving a Secret the rule does not manage untouched", "managedBy", result.ManagedBy)
	case result.Outcome == copyengine.Agreed:
		log.V(1).Info("Another rule copying the same source manages the copy", "managedBy", result.ManagedBy)
	case result.Outcome == copyengine.InSync, result.Outcome == copyengine.TargetDeleting:
	default:
		log.Info("Copied the source Secret", "outcome", result.Outcome)
	}
}

// Terminating reports whether the namespace name of namespaces is being
// deleted, in which case no source in it is copied.
func Terminating(namespaces []corev1.Namespace, name string) bool {
	for i := range namespaces {
		if namespaces[i].Name == name {
			return namespaces[i].Status.Phase == corev1.NamespaceTerminating ||
				!namespaces[i].DeletionTimestamp.IsZero()
		}
	}
	return false
}
