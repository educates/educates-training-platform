// Package secretcopier reconciles SecretCopiers: it copies the source
// Secret of each rule into every namespace the rule selects, other than the
// source's own.
package secretcopier

import (
	"context"
	"fmt"

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
	"github.com/educates/educates-training-platform/secrets-manager/internal/copyrules"
	"github.com/educates/educates-training-platform/secrets-manager/internal/eventmap"
	"github.com/educates/educates-training-platform/secrets-manager/internal/feedback"
	"github.com/educates/educates-training-platform/secrets-manager/internal/selectors"
)

// Reconciler reconciles one SecretCopier at a time.
type Reconciler struct {
	cache       client.Reader
	engine      *copyengine.Engine
	convergence convergence.Policy
	feedback    *feedback.Feedback
}

// New returns a reconciler that reads SecretCopiers and namespaces from
// cache, converges copies with engine, a Secret engine, retries under
// policy, and reports each SecretCopier's status and Events through
// reports.
func New(cache client.Reader, engine *copyengine.Engine, policy convergence.Policy,
	reports *feedback.Feedback) *Reconciler {
	return &Reconciler{cache: cache, engine: engine, convergence: policy, feedback: reports}
}

// SetupWithManager registers the reconciler with mgr. It reconciles a
// SecretCopier when its spec changes, when a Secret it copies, or one with
// a name it gives its copies, changes, when a namespace it may select, or
// the namespace of a Secret it copies, is created, relabelled, given other
// owners or starts being deleted, when a SecretImporter named
// after its copies is created, changed or deleted, and when another
// SecretCopier or a SecretExporter that names copies as it does changes its
// rules or is deleted, which may leave those copies for it to take over.
// Besides, the reconciler's policy reconciles each SecretCopier again a
// backstop interval after its last reconcile, and retries a failed one with
// backoff. The cache must hold the indexes eventmap.IndexSecretCopiers
// registers.
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
			eventmap.RuleChanges[*secretsv1beta1.SecretCopier]())).
		WatchesRawSource(source.Kind(mgr.GetCache(), &secretsv1beta1.SecretExporter{},
			handler.TypedEnqueueRequestsFromMapFunc(eventmap.SecretCopiersSharingExportedCopies(mgr.GetClient())),
			eventmap.RuleChanges[*secretsv1beta1.SecretExporter]())).
		WatchesRawSource(source.Kind(mgr.GetCache(), &secretsv1beta1.SecretImporter{},
			handler.TypedEnqueueRequestsFromMapFunc(eventmap.SecretCopiersForImporter(mgr.GetClient())),
			predicate.TypedGenerationChangedPredicate[*secretsv1beta1.SecretImporter]{})),
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

	// missingKeys are the keys the rule's sourceSecret.keys lists that the
	// source lacks, when the source is copied.
	missingKeys []string

	// targets are the copies of the source, one in each namespace the rule
	// selects. Nothing is copied into them when the source does not exist or
	// its namespace is being deleted.
	targets []copyrules.Target
}

// Reconcile copies the source Secret of each rule of the SecretCopier
// req names, and writes its status. A failure on one target is reported in
// the returned error and never stops the others; a write conflict requeues
// the SecretCopier.
func (r *Reconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	copier := &secretsv1beta1.SecretCopier{}
	if err := r.cache.Get(ctx, req.NamespacedName, copier); err != nil {
		if apierrors.IsNotFound(err) {
			r.feedback.Forget(secretsv1beta1.SecretCopierKind, req.NamespacedName)
		}
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	if !copier.DeletionTimestamp.IsZero() {
		r.feedback.Forget(secretsv1beta1.SecretCopierKind, req.NamespacedName)
		return ctrl.Result{}, nil
	}

	namespaces := &corev1.NamespaceList{}
	if err := r.cache.List(ctx, namespaces); err != nil {
		return ctrl.Result{}, fmt.Errorf("listing namespaces: %w", err)
	}

	converger := copyrules.NewConverger(r.cache, r.engine)
	reports := make([]ruleReport, 0, len(copier.Spec.Rules))
	for i := range copier.Spec.Rules {
		reports = append(reports, r.reconcileRule(ctx, converger, copier, &copier.Spec.Rules[i], namespaces.Items))
	}
	for i := range reports {
		converger.Settle(secretsv1beta1.CopierRule(secretsv1beta1.SecretCopierKind, copier.Name), reports[i].targets)
	}
	return r.publish(ctx, copier, converger, reports)
}

// reconcileRule copies the source Secret of rule, a rule of copier, into
// every namespace of namespaces the rule selects, with converger, and
// reports what it found.
func (r *Reconciler) reconcileRule(ctx context.Context, converger *copyrules.Converger,
	copier *secretsv1beta1.SecretCopier, rule *secretsv1beta1.SecretCopierRule,
	namespaces []corev1.Namespace) ruleReport {
	report := ruleReport{
		source: types.NamespacedName{Namespace: rule.SourceSecret.Namespace, Name: rule.SourceSecret.Name},
	}
	log := ctrl.LoggerFrom(ctx).WithValues("sourceSecret", report.source.String())

	src, err := r.engine.Source(ctx, report.source)
	switch {
	case apierrors.IsNotFound(err):
		log.V(1).Info("The source Secret does not exist: nothing to copy")
	case err != nil:
		report.err = fmt.Errorf("reading the source Secret %s: %w", report.source, err)
		converger.Fail(report.err)
		return report
	default:
		report.sourceExists = true
	}

	selector := selectors.Namespaces(rule.TargetNamespaces)
	if err := selector.Err(); err != nil {
		log.Info("The rule's targetNamespaces are invalid: the rule selects no namespace", "reason", err.Error())
		report.invalidSelector = err
		return report
	}
	if report.sourceExists && copyrules.Terminating(namespaces, report.source.Namespace) {
		log.V(1).Info("The source Secret's namespace is being deleted: not copying it")
		report.sourceTerminating = true
	}
	if !report.sourceExists || report.sourceTerminating {
		src = nil
	}

	report.targets = converger.Converge(ctrl.LoggerInto(ctx, log), &copyrules.Rule{
		Source:          src,
		SourceNamespace: report.source.Namespace,
		Namespaces:      selector,
		Offer:           copyrules.CopierOffer(rule),
		Copy: copyengine.Copy{
			Name:        rule.CopyName(),
			Rule:        secretsv1beta1.CopierRule(secretsv1beta1.SecretCopierKind, copier.Name),
			Labels:      rule.TargetSecret.Labels,
			Annotations: rule.TargetSecret.Annotations,
			Keys:        rule.SourceSecret.Keys,
		},
		Owner: func(*secretsv1beta1.SecretImporter) *copyengine.Owner { return owner(copier, rule) },
	}, namespaces)

	if src != nil {
		missing, err := r.engine.MissingKeys(ctx, src, rule.SourceSecret.Keys)
		switch {
		case apierrors.IsNotFound(err):
			log.V(1).Info("The source Secret was deleted while being copied")
		case err != nil:
			report.err = fmt.Errorf("reading the source Secret %s: %w", report.source, err)
			converger.Fail(report.err)
		default:
			report.missingKeys = missing
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
