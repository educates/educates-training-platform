// Package secretcopier reconciles SecretCopiers: it copies the source
// Secret of each rule into every namespace the rule selects, other than the
// source's own.
package secretcopier

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/source"

	secretsv1beta1 "github.com/educates/educates-training-platform/secrets-manager/api/v1beta1"
	"github.com/educates/educates-training-platform/secrets-manager/internal/convergence"
	"github.com/educates/educates-training-platform/secrets-manager/internal/copyengine"
	"github.com/educates/educates-training-platform/secrets-manager/internal/eventmap"
	"github.com/educates/educates-training-platform/secrets-manager/internal/selectors"
)

// conflictRetryDelay is how long a SecretCopier waits to be reconciled
// again after a write conflicted with another writer's.
const conflictRetryDelay = time.Second

// Reconciler reconciles one SecretCopier at a time.
type Reconciler struct {
	cache       client.Reader
	engine      *copyengine.Engine
	convergence convergence.Policy
}

// New returns a reconciler that reads SecretCopiers and namespaces from
// cache, converges copies with engine, a Secret engine, and retries under
// policy.
func New(cache client.Reader, engine *copyengine.Engine, policy convergence.Policy) *Reconciler {
	return &Reconciler{cache: cache, engine: engine, convergence: policy}
}

// SetupWithManager registers the reconciler with mgr. It reconciles a
// SecretCopier when its spec changes, when a Secret it copies, or one with
// a name it gives its copies, changes, when a namespace it may select is
// created, relabelled or given other owners, and when another SecretCopier
// that names copies as it does is deleted, leaving those copies for it to
// take over. Besides, the reconciler's policy reconciles each SecretCopier
// again a backstop interval after its last reconcile, and retries a failed
// one with backoff. The cache must hold the indexes
// eventmap.IndexSecretCopiers registers.
func (r *Reconciler) SetupWithManager(mgr ctrl.Manager) error {
	return r.convergence.Complete(ctrl.NewControllerManagedBy(mgr).
		Named("secretcopier").
		For(&secretsv1beta1.SecretCopier{}, builder.WithPredicates(predicate.GenerationChangedPredicate{})).
		Watches(&corev1.Secret{}, handler.EnqueueRequestsFromMapFunc(eventmap.SecretCopiersForSecret(mgr.GetClient())),
			builder.OnlyMetadata).
		WatchesRawSource(source.Kind(mgr.GetCache(), &corev1.Namespace{},
			handler.TypedEnqueueRequestsFromMapFunc(eventmap.SecretCopiersForNamespace(mgr.GetClient())),
			eventmap.NamespaceSelectionChanged())).
		WatchesRawSource(source.Kind(mgr.GetCache(), &secretsv1beta1.SecretCopier{},
			handler.TypedEnqueueRequestsFromMapFunc(eventmap.SecretCopiersSharingCopies(mgr.GetClient())),
			eventmap.Deletions[*secretsv1beta1.SecretCopier]())),
		&secretsv1beta1.SecretCopier{}, mgr.GetClient(), r)
}

// ruleReport is what reconciling one rule of a SecretCopier found.
type ruleReport struct {
	// source names the rule's source Secret.
	source types.NamespacedName

	// sourceExists reports whether the source Secret exists.
	sourceExists bool

	// sourceTerminating reports whether the source's namespace is being
	// deleted, in which case the source is not copied.
	sourceTerminating bool

	// invalidSelector says why the rule's targetNamespaces are invalid,
	// when they are, in which case the rule selects no namespace.
	invalidSelector error

	// err is the failure to read the source, when reading it failed.
	err error

	// targets are the copies of the source, one in each namespace the rule
	// selects, when the source is copied.
	targets []targetReport
}

// targetReport is what converging one copy of a rule's source found.
type targetReport struct {
	// namespace is the namespace of the copy.
	namespace string

	// result says what converging the copy did, or why it wrote nothing,
	// when it did not fail.
	result copyengine.Result

	// err is the failure to converge the copy, when it failed. A write
	// conflict is the API server's Conflict error.
	err error
}

// Reconcile copies the source Secret of each rule of the SecretCopier
// req names. A failure on one target is reported in the returned error and
// never stops the others; a write conflict requeues the SecretCopier.
func (r *Reconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	copier := &secretsv1beta1.SecretCopier{}
	if err := r.cache.Get(ctx, req.NamespacedName, copier); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	if !copier.DeletionTimestamp.IsZero() {
		return ctrl.Result{}, nil
	}

	namespaces := &corev1.NamespaceList{}
	if err := r.cache.List(ctx, namespaces); err != nil {
		return ctrl.Result{}, fmt.Errorf("listing namespaces: %w", err)
	}

	reports := make([]ruleReport, 0, len(copier.Spec.Rules))
	for i := range copier.Spec.Rules {
		reports = append(reports, r.reconcileRule(ctx, copier, &copier.Spec.Rules[i], namespaces.Items))
	}
	return reconcileResult(reports)
}

// reconcileRule copies the source Secret of rule into every namespace of
// namespaces the rule selects, and reports what it found.
func (r *Reconciler) reconcileRule(ctx context.Context, copier *secretsv1beta1.SecretCopier,
	rule *secretsv1beta1.SecretCopierRule, namespaces []corev1.Namespace) ruleReport {
	report := ruleReport{
		source: types.NamespacedName{Namespace: rule.SourceSecret.Namespace, Name: rule.SourceSecret.Name},
	}
	log := ctrl.LoggerFrom(ctx).WithValues("sourceSecret", report.source.String())

	src, err := r.engine.Source(ctx, report.source)
	switch {
	case apierrors.IsNotFound(err):
		log.V(1).Info("The source Secret does not exist: nothing to copy")
		return report
	case err != nil:
		report.err = fmt.Errorf("reading the source Secret %s: %w", report.source, err)
		return report
	}
	report.sourceExists = true

	selector := selectors.Namespaces(rule.TargetNamespaces)
	if err := selector.Err(); err != nil {
		log.Info("The rule's targetNamespaces are invalid: the rule selects no namespace", "reason", err.Error())
		report.invalidSelector = err
		return report
	}
	if terminating(namespaces, report.source.Namespace) {
		log.V(1).Info("The source Secret's namespace is being deleted: not copying it")
		report.sourceTerminating = true
		return report
	}

	copyName := rule.CopyName()
	for i := range namespaces {
		ns := &namespaces[i]
		if ns.Name == report.source.Namespace || !selector.Match(ns) {
			continue
		}
		result, err := r.engine.Converge(ctx, copyengine.Copy{
			Source:      src,
			Namespace:   ns.Name,
			Name:        copyName,
			Rule:        secretsv1beta1.CopierRule(secretsv1beta1.SecretCopierKind, copier.Name),
			Labels:      rule.TargetSecret.Labels,
			Annotations: rule.TargetSecret.Annotations,
			Keys:        rule.TargetSecret.Keys,
			Owner:       owner(copier, rule),
		})
		report.targets = append(report.targets, targetReport{namespace: ns.Name, result: result, err: err})
		logTarget(log.WithValues("namespace", ns.Name, "name", copyName), result, err)
		if err == nil && result.Outcome.Wrote() {
			convergence.Repaired(ctx)
		}
	}
	return report
}

// owner is the owner a rule of copier gives its copies: copier under
// reclaimPolicy Delete, the default, and none under Retain.
func owner(copier *secretsv1beta1.SecretCopier, rule *secretsv1beta1.SecretCopierRule) *copyengine.Owner {
	if rule.ReclaimPolicy == secretsv1beta1.ReclaimRetain {
		return nil
	}
	return &copyengine.Owner{
		APIVersion: secretsv1beta1.GroupVersion.String(),
		Kind:       secretsv1beta1.SecretCopierKind,
		Name:       copier.Name,
		UID:        copier.UID,
	}
}

// terminating reports whether the namespace name of namespaces is being
// deleted.
func terminating(namespaces []corev1.Namespace, name string) bool {
	for i := range namespaces {
		if namespaces[i].Name == name {
			return namespaces[i].Status.Phase == corev1.NamespaceTerminating ||
				!namespaces[i].DeletionTimestamp.IsZero()
		}
	}
	return false
}

// logTarget logs what converging one copy did, when it wrote or was kept
// from writing.
func logTarget(log logr.Logger, result copyengine.Result, err error) {
	switch {
	case apierrors.IsConflict(err):
		log.V(1).Info("Writing the copy conflicted with another writer: retrying")
	case err != nil:
		// The failure is returned, and logged, with the reconcile's error.
	case result.Outcome == copyengine.Conflict:
		log.V(1).Info("Leaving a Secret the rule does not manage untouched", "managedBy", result.ManagedBy)
	case result.Outcome == copyengine.Agreed:
		log.V(1).Info("Another rule copying the same source manages the copy", "managedBy", result.ManagedBy)
	case result.Outcome == copyengine.InSync, result.Outcome == copyengine.TargetDeleting:
	default:
		log.Info("Copied the source Secret", "outcome", result.Outcome)
	}
}

// reconcileResult turns what reconciling the rules found into the result
// of the reconcile. The failures are its error, which the work queue
// retries with backoff. A write conflict is no failure: it is retried by
// requeueing.
func reconcileResult(reports []ruleReport) (ctrl.Result, error) {
	var failures []error
	conflicted := false
	for _, report := range reports {
		if report.err != nil {
			failures = append(failures, report.err)
		}
		for _, target := range report.targets {
			switch {
			case target.err == nil:
			case apierrors.IsConflict(target.err):
				conflicted = true
			default:
				failures = append(failures, fmt.Errorf("copying Secret %s into namespace %s: %w",
					report.source, target.namespace, target.err))
			}
		}
	}
	if len(failures) > 0 {
		return ctrl.Result{}, errors.Join(failures...)
	}
	if conflicted {
		return ctrl.Result{RequeueAfter: conflictRetryDelay}, nil
	}
	return ctrl.Result{}, nil
}
