package exportimport

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/source"

	secretsv1beta1 "github.com/educates/educates-training-platform/secrets-manager/api/v1beta1"
	"github.com/educates/educates-training-platform/secrets-manager/internal/copyengine"
	"github.com/educates/educates-training-platform/secrets-manager/internal/eventmap"
	"github.com/educates/educates-training-platform/secrets-manager/internal/selectors"
)

// conflictRetryDelay is how long a SecretExporter waits to be reconciled
// again after a write conflicted with another writer's.
const conflictRetryDelay = time.Second

// ExporterReconciler reconciles one SecretExporter at a time.
type ExporterReconciler struct {
	cache  client.Reader
	engine *copyengine.Engine
}

// NewExporterReconciler returns a reconciler that reads SecretExporters,
// SecretImporters and namespaces from cache, and converges copies with
// engine, a Secret engine.
func NewExporterReconciler(cache client.Reader, engine *copyengine.Engine) *ExporterReconciler {
	return &ExporterReconciler{cache: cache, engine: engine}
}

// SetupWithManager registers the reconciler with mgr. It reconciles a
// SecretExporter when its spec changes, when the Secret it exports, one of
// its copies, or a Secret with a name it gives its copies, changes, when a
// namespace it may select is created, relabelled or given other owners,
// when a SecretImporter named after its copies is created, changed or
// deleted, and when a SecretCopier or another SecretExporter that names
// copies as it does is deleted, leaving those copies for it to take over.
// The cache must hold the indexes eventmap.IndexSecretExporters registers.
func (r *ExporterReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		Named("secretexporter").
		For(&secretsv1beta1.SecretExporter{}, builder.WithPredicates(predicate.GenerationChangedPredicate{})).
		Watches(&corev1.Secret{}, handler.EnqueueRequestsFromMapFunc(eventmap.SecretExportersForSecret(mgr.GetClient())),
			builder.OnlyMetadata).
		WatchesRawSource(source.Kind(mgr.GetCache(), &corev1.Namespace{},
			handler.TypedEnqueueRequestsFromMapFunc(eventmap.SecretExportersForNamespace(mgr.GetClient())),
			eventmap.NamespaceSelectionChanged())).
		WatchesRawSource(source.Kind(mgr.GetCache(), &secretsv1beta1.SecretImporter{},
			handler.TypedEnqueueRequestsFromMapFunc(eventmap.SecretExportersForImporter(mgr.GetClient())),
			predicate.TypedGenerationChangedPredicate[*secretsv1beta1.SecretImporter]{})).
		WatchesRawSource(source.Kind(mgr.GetCache(), &secretsv1beta1.SecretExporter{},
			handler.TypedEnqueueRequestsFromMapFunc(
				eventmap.SecretExportersSharingCopies[*secretsv1beta1.SecretExporter](mgr.GetClient())),
			eventmap.Deletions[*secretsv1beta1.SecretExporter]())).
		WatchesRawSource(source.Kind(mgr.GetCache(), &secretsv1beta1.SecretCopier{},
			handler.TypedEnqueueRequestsFromMapFunc(
				eventmap.SecretExportersSharingCopies[*secretsv1beta1.SecretCopier](mgr.GetClient())),
			eventmap.Deletions[*secretsv1beta1.SecretCopier]())).
		Complete(r)
}

// exporterReport is what reconciling a SecretExporter found.
type exporterReport struct {
	// source names the exported Secret: the SecretExporter's namesake in
	// its namespace.
	source types.NamespacedName

	// sourceExists reports whether the exported Secret exists.
	sourceExists bool

	// sourceTerminating reports whether the exported Secret's namespace is
	// being deleted, in which case the Secret is not copied.
	sourceTerminating bool

	// err is the failure to read the exported Secret, or to list the
	// namespaces, when either failed.
	err error

	// rules hold what reconciling each rule found, in the order of the
	// SecretExporter's rules.
	rules []ruleReport
}

// ruleReport is what reconciling one rule of a SecretExporter found.
type ruleReport struct {
	// copyName is the name of the rule's copies.
	copyName string

	// invalidSelector says why the rule's targetNamespaces are invalid,
	// when they are, in which case the rule selects no namespace.
	invalidSelector error

	// targets are the rule's copies, one in each namespace it selects, when
	// the exported Secret is copied.
	targets []targetReport
}

// targetReport is what reconciling one copy of a rule found.
type targetReport struct {
	// namespace is the namespace of the copy.
	namespace string

	// refusal says why the target does not accept the copy, when it awaits
	// authorization: no SecretImporter is named after the copy there, or
	// the one there holds another shared secret or does not admit the
	// source's namespace. Nothing is converged then.
	refusal Refusal

	// result says what converging the copy did, or why it wrote nothing,
	// when the target accepts the copy and converging it did not fail.
	result copyengine.Result

	// err is the failure to read the SecretImporter or to converge the
	// copy, when either failed. A write conflict is the API server's
	// Conflict error.
	err error
}

// Reconcile copies the Secret the SecretExporter req names exports into
// each namespace its rules select where a SecretImporter accepts the copy.
// A failure on one target is reported in the returned error and never
// stops the others; a write conflict requeues the SecretExporter.
func (r *ExporterReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	exporter := &secretsv1beta1.SecretExporter{}
	if err := r.cache.Get(ctx, req.NamespacedName, exporter); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	if !exporter.DeletionTimestamp.IsZero() {
		return ctrl.Result{}, nil
	}
	return exporterResult(r.export(ctx, exporter))
}

// export copies the Secret exporter exports into each namespace its rules
// select where a SecretImporter accepts the copy, and reports what it
// found.
func (r *ExporterReconciler) export(ctx context.Context, exporter *secretsv1beta1.SecretExporter) exporterReport {
	report := exporterReport{source: client.ObjectKeyFromObject(exporter)}
	log := ctrl.LoggerFrom(ctx)

	src, err := r.engine.Source(ctx, report.source)
	switch {
	case apierrors.IsNotFound(err):
		log.V(1).Info("The exported Secret does not exist: nothing to copy")
		return report
	case err != nil:
		report.err = fmt.Errorf("reading the exported Secret %s: %w", report.source, err)
		return report
	}
	report.sourceExists = true

	namespaces := &corev1.NamespaceList{}
	if err := r.cache.List(ctx, namespaces); err != nil {
		report.err = fmt.Errorf("listing namespaces: %w", err)
		return report
	}
	if terminating(namespaces.Items, exporter.Namespace) {
		log.V(1).Info("The exported Secret's namespace is being deleted: not copying it")
		report.sourceTerminating = true
		return report
	}

	report.rules = make([]ruleReport, 0, len(exporter.Spec.Rules))
	for i := range exporter.Spec.Rules {
		report.rules = append(report.rules, r.exportRule(ctx, exporter, &exporter.Spec.Rules[i], src, namespaces.Items))
	}
	return report
}

// exportRule copies src, the Secret exporter exports, into every namespace
// of namespaces that rule selects where a SecretImporter accepts the copy,
// and reports what it found.
func (r *ExporterReconciler) exportRule(ctx context.Context, exporter *secretsv1beta1.SecretExporter,
	rule *secretsv1beta1.SecretExporterRule, src *unstructured.Unstructured, namespaces []corev1.Namespace) ruleReport {
	report := ruleReport{copyName: rule.CopyName(exporter.Name)}
	log := ctrl.LoggerFrom(ctx).WithValues("name", report.copyName)

	selector := selectors.Namespaces(rule.TargetNamespaces)
	if err := selector.Err(); err != nil {
		log.Info("The rule's targetNamespaces are invalid: the rule selects no namespace", "reason", err.Error())
		report.invalidSelector = err
		return report
	}

	offer := ExporterOffer(exporter, rule)
	for i := range namespaces {
		ns := &namespaces[i]
		if ns.Name == exporter.Namespace || !selector.Match(ns) {
			continue
		}
		target := targetReport{namespace: ns.Name}
		targetLog := log.WithValues("namespace", ns.Name)
		importer, err := Importer(ctx, r.cache, types.NamespacedName{Namespace: ns.Name, Name: report.copyName})
		if err != nil {
			target.err = fmt.Errorf("reading the SecretImporter: %w", err)
			report.targets = append(report.targets, target)
			continue
		}
		if target.refusal = Authorize(importer, offer); target.refusal != Accepted {
			targetLog.V(1).Info("The target does not accept the copy: awaiting authorization", "refusal", target.refusal)
			report.targets = append(report.targets, target)
			continue
		}
		target.result, target.err = r.engine.Converge(ctx, copyengine.Copy{
			Source:      src,
			Namespace:   ns.Name,
			Name:        report.copyName,
			Rule:        secretsv1beta1.CopierRule(secretsv1beta1.SecretExporterKind, exporter.Name),
			Labels:      rule.TargetSecret.Labels,
			Annotations: rule.TargetSecret.Annotations,
			Keys:        rule.TargetSecret.Keys,
			Owner: &copyengine.Owner{
				APIVersion: secretsv1beta1.GroupVersion.String(),
				Kind:       secretsv1beta1.SecretImporterKind,
				Name:       importer.Name,
				UID:        importer.UID,
			},
		})
		report.targets = append(report.targets, target)
		logTarget(targetLog, target.result, target.err)
	}
	return report
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
		log.Info("Exported the Secret", "outcome", result.Outcome)
	}
}

// exporterResult turns what reconciling a SecretExporter found into the
// result of the reconcile. The failures are its error, which the work queue
// retries with backoff. A write conflict is no failure: it is retried by
// requeueing.
func exporterResult(report exporterReport) (ctrl.Result, error) {
	var failures []error
	if report.err != nil {
		failures = append(failures, report.err)
	}
	conflicted := false
	for _, rule := range report.rules {
		for _, target := range rule.targets {
			switch {
			case target.err == nil:
			case apierrors.IsConflict(target.err):
				conflicted = true
			default:
				failures = append(failures, fmt.Errorf("exporting Secret %s into namespace %s as %s: %w",
					report.source, target.namespace, rule.copyName, target.err))
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
