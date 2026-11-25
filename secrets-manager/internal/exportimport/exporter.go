package exportimport

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

// ExporterReconciler reconciles one SecretExporter at a time.
type ExporterReconciler struct {
	cache       client.Reader
	engine      *copyengine.Engine
	convergence convergence.Policy
	feedback    *feedback.Feedback
}

// NewExporterReconciler returns a reconciler that reads SecretExporters,
// SecretImporters and namespaces from cache, converges copies with engine,
// a Secret engine, retries under policy, and reports each SecretExporter's
// status and Events through reports.
func NewExporterReconciler(cache client.Reader, engine *copyengine.Engine,
	policy convergence.Policy, reports *feedback.Feedback) *ExporterReconciler {
	return &ExporterReconciler{cache: cache, engine: engine, convergence: policy, feedback: reports}
}

// SetupWithManager registers the reconciler with mgr. It reconciles a
// SecretExporter when its spec changes, when the Secret it exports, one of
// its copies, or a Secret with a name it gives its copies, changes, when a
// namespace it may select, or its own, is created, relabelled, given other
// owners or starts being deleted,
// when a SecretImporter named after its copies is created, changed or
// deleted, and when a SecretCopier or another SecretExporter that names
// copies as it does changes its rules or is deleted, which may leave those
// copies for it to take over. Besides, the reconciler's policy reconciles
// each SecretExporter again a backstop interval after its last reconcile,
// and retries a failed one with backoff. The cache must hold the indexes
// eventmap.IndexSecretExporters registers.
func (r *ExporterReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return r.convergence.Complete(ctrl.NewControllerManagedBy(mgr).
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
			eventmap.RuleChanges[*secretsv1beta1.SecretExporter]())).
		WatchesRawSource(source.Kind(mgr.GetCache(), &secretsv1beta1.SecretCopier{},
			handler.TypedEnqueueRequestsFromMapFunc(
				eventmap.SecretExportersSharingCopies[*secretsv1beta1.SecretCopier](mgr.GetClient())),
			eventmap.RuleChanges[*secretsv1beta1.SecretCopier]())),
		&secretsv1beta1.SecretExporter{}, mgr.GetClient(), r)
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
	// invalidSelector says why the rule's targetNamespaces are invalid,
	// when they are, in which case the rule selects no namespace.
	invalidSelector error

	// targets are the rule's copies, one in each namespace it selects.
	// Nothing is copied into them when the exported Secret does not exist or
	// its namespace is being deleted.
	targets []copyrules.Target
}

// Reconcile copies the Secret the SecretExporter req names exports into
// each namespace its rules select where a SecretImporter accepts the copy,
// and writes its status. A failure on one target is reported in the
// returned error and never stops the others; a write conflict requeues the
// SecretExporter.
func (r *ExporterReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	exporter := &secretsv1beta1.SecretExporter{}
	if err := r.cache.Get(ctx, req.NamespacedName, exporter); err != nil {
		if apierrors.IsNotFound(err) {
			r.feedback.Forget(secretsv1beta1.SecretExporterKind, req.NamespacedName)
		}
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	if !exporter.DeletionTimestamp.IsZero() {
		r.feedback.Forget(secretsv1beta1.SecretExporterKind, req.NamespacedName)
		return ctrl.Result{}, nil
	}
	converger := copyrules.NewConverger(r.cache, r.engine)
	return r.publish(ctx, exporter, converger, r.export(ctx, converger, exporter))
}

// export copies the Secret exporter exports, with converger, into each
// namespace its rules select where a SecretImporter accepts the copy, and
// reports what it found.
func (r *ExporterReconciler) export(ctx context.Context, converger *copyrules.Converger,
	exporter *secretsv1beta1.SecretExporter) exporterReport {
	report := exporterReport{source: client.ObjectKeyFromObject(exporter)}
	log := ctrl.LoggerFrom(ctx)

	src, err := r.engine.Source(ctx, report.source)
	switch {
	case apierrors.IsNotFound(err):
		log.V(1).Info("The exported Secret does not exist: nothing to copy")
	case err != nil:
		report.err = fmt.Errorf("reading the exported Secret %s: %w", report.source, err)
		converger.Fail(report.err)
		return report
	default:
		report.sourceExists = true
	}

	namespaces := &corev1.NamespaceList{}
	if err := r.cache.List(ctx, namespaces); err != nil {
		report.err = fmt.Errorf("listing namespaces: %w", err)
		converger.Fail(report.err)
		return report
	}
	if report.sourceExists && copyrules.Terminating(namespaces.Items, exporter.Namespace) {
		log.V(1).Info("The exported Secret's namespace is being deleted: not copying it")
		report.sourceTerminating = true
	}
	if !report.sourceExists || report.sourceTerminating {
		src = nil
	}

	report.rules = make([]ruleReport, 0, len(exporter.Spec.Rules))
	for i := range exporter.Spec.Rules {
		report.rules = append(report.rules,
			exportRule(ctx, converger, exporter, &exporter.Spec.Rules[i], src, namespaces.Items))
	}
	for i := range report.rules {
		converger.Settle(secretsv1beta1.CopierRule(secretsv1beta1.SecretExporterKind, exporter.Name),
			report.rules[i].targets)
	}
	return report
}

// exportRule copies src, the Secret exporter exports, with converger, into
// every namespace of namespaces that rule selects where a SecretImporter
// accepts the copy, and reports what it found. src is nil when the Secret
// is not copied, and the rule's targets are then only counted.
func exportRule(ctx context.Context, converger *copyrules.Converger, exporter *secretsv1beta1.SecretExporter,
	rule *secretsv1beta1.SecretExporterRule, src *copyengine.Source, namespaces []corev1.Namespace) ruleReport {
	var report ruleReport
	copyName := rule.CopyName(exporter.Name)
	selector := selectors.Namespaces(rule.TargetNamespaces)
	if err := selector.Err(); err != nil {
		ctrl.LoggerFrom(ctx).Info("The rule's targetNamespaces are invalid: the rule selects no namespace",
			"name", copyName, "reason", err.Error())
		report.invalidSelector = err
		return report
	}

	report.targets = converger.Converge(ctx, &copyrules.Rule{
		Source:          src,
		SourceNamespace: exporter.Namespace,
		Namespaces:      selector,
		Offer:           copyrules.ExporterOffer(exporter, rule),
		Copy: copyengine.Copy{
			Name:        copyName,
			Rule:        secretsv1beta1.CopierRule(secretsv1beta1.SecretExporterKind, exporter.Name),
			Labels:      rule.TargetSecret.Labels,
			Annotations: rule.TargetSecret.Annotations,
			Keys:        rule.TargetSecret.Keys,
		},
		Owner: func(importer *secretsv1beta1.SecretImporter) *copyengine.Owner {
			return &copyengine.Owner{
				APIVersion: secretsv1beta1.GroupVersion.String(),
				Kind:       secretsv1beta1.SecretImporterKind,
				Name:       importer.Name,
				UID:        importer.UID,
			}
		},
	}, namespaces)
	return report
}
