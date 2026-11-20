// Package secretcopier reconciles SecretCopiers: it copies the source
// Secret of each rule into every namespace the rule selects, other than the
// source's own.
package secretcopier

import (
	"context"
	"errors"
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
	"github.com/educates/educates-training-platform/secrets-manager/internal/copyengine"
	"github.com/educates/educates-training-platform/secrets-manager/internal/eventmap"
	"github.com/educates/educates-training-platform/secrets-manager/internal/selectors"
)

// Reconciler reconciles one SecretCopier at a time.
type Reconciler struct {
	cache  client.Reader
	engine *copyengine.Engine
}

// New returns a reconciler that reads SecretCopiers and namespaces from
// cache, and converges copies with engine, a Secret engine.
func New(cache client.Reader, engine *copyengine.Engine) *Reconciler {
	return &Reconciler{cache: cache, engine: engine}
}

// SetupWithManager registers the reconciler with mgr. It reconciles a
// SecretCopier when its spec changes, when a Secret it copies, or one with
// a name it gives its copies, changes, and when a namespace it may select
// is created, relabelled or given other owners. The cache must hold the
// indexes eventmap.IndexSecretCopiers registers.
func (r *Reconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		Named("secretcopier").
		For(&secretsv1beta1.SecretCopier{}, builder.WithPredicates(predicate.GenerationChangedPredicate{})).
		Watches(&corev1.Secret{}, handler.EnqueueRequestsFromMapFunc(eventmap.SecretCopiersForSecret(mgr.GetClient())),
			builder.OnlyMetadata).
		WatchesRawSource(source.Kind(mgr.GetCache(), &corev1.Namespace{},
			handler.TypedEnqueueRequestsFromMapFunc(eventmap.SecretCopiersForNamespace(mgr.GetClient())),
			eventmap.NamespaceSelectionChanged())).
		Complete(r)
}

// Reconcile copies the source Secret of each rule of the SecretCopier
// req names. A failure on one target is reported in the returned error and
// never stops the others.
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

	var failures []error
	for i := range copier.Spec.Rules {
		failures = append(failures, r.reconcileRule(ctx, copier, &copier.Spec.Rules[i], namespaces.Items)...)
	}
	return ctrl.Result{}, errors.Join(failures...)
}

// reconcileRule copies the source Secret of rule into every namespace of
// namespaces the rule selects, and returns the failures.
func (r *Reconciler) reconcileRule(ctx context.Context, copier *secretsv1beta1.SecretCopier,
	rule *secretsv1beta1.SecretCopierRule, namespaces []corev1.Namespace) []error {
	sourceKey := types.NamespacedName{Namespace: rule.SourceSecret.Namespace, Name: rule.SourceSecret.Name}
	log := ctrl.LoggerFrom(ctx).WithValues("sourceSecret", sourceKey.String())

	src, err := r.engine.Source(ctx, sourceKey)
	if apierrors.IsNotFound(err) {
		log.V(1).Info("The source Secret does not exist: nothing to copy")
		return nil
	}
	if err != nil {
		return []error{fmt.Errorf("reading the source Secret %s: %w", sourceKey, err)}
	}

	selector := selectors.Namespaces(rule.TargetNamespaces)
	copyName := rule.CopyName()
	annotations := map[string]string{
		secretsv1beta1.CopierRuleAnnotation: secretsv1beta1.CopierRule(secretsv1beta1.SecretCopierKind, copier.Name),
		secretsv1beta1.SecretNameAnnotation: sourceKey.String(),
	}

	var failures []error
	for i := range namespaces {
		ns := &namespaces[i]
		if ns.Name == sourceKey.Namespace || !selector.Match(ns) {
			continue
		}
		outcome, err := r.engine.Converge(ctx, copyengine.Copy{
			Source:      src,
			Namespace:   ns.Name,
			Name:        copyName,
			Labels:      rule.TargetSecret.Labels,
			Annotations: annotations,
		})
		if err != nil {
			failures = append(failures, fmt.Errorf("copying Secret %s into namespace %s: %w", sourceKey, ns.Name, err))
			continue
		}
		if outcome == copyengine.Created || outcome == copyengine.Updated {
			log.Info("Copied the source Secret", "namespace", ns.Name, "name", copyName, "outcome", outcome)
		}
	}
	return failures
}
