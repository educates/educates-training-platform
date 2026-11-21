package exportimport

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"strings"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/source"

	secretsv1beta1 "github.com/educates/educates-training-platform/secrets-manager/api/v1beta1"
	"github.com/educates/educates-training-platform/secrets-manager/internal/convergence"
	"github.com/educates/educates-training-platform/secrets-manager/internal/copyrules"
	"github.com/educates/educates-training-platform/secrets-manager/internal/eventmap"
	"github.com/educates/educates-training-platform/secrets-manager/internal/feedback"
	"github.com/educates/educates-training-platform/secrets-manager/internal/selectors"
)

// ImportPhase says where a SecretImporter stands.
type ImportPhase string

const (
	// Imported means the copy named after the importer exists in its
	// namespace, written by an exporter or copier whose offer the importer
	// accepts.
	Imported ImportPhase = "Imported"

	// AwaitingCopy means the importer accepts an exporter's or copier's
	// offer of the copy, and the copy is not there yet: it is about to be
	// written, or cannot be, such as when its source does not exist or
	// another Secret holds its name.
	AwaitingCopy ImportPhase = "AwaitingCopy"

	// AwaitingAuthorization means exporters or copiers offer the copy, and
	// the importer refuses every offer.
	AwaitingAuthorization ImportPhase = "AwaitingAuthorization"

	// NoMatchingExporter means no exporter or copier rule offers the copy
	// into the importer's namespace.
	NoMatchingExporter ImportPhase = "NoMatchingExporter"
)

// Offerer names the SecretExporter or SecretCopier whose rule offers a
// copy. Namespace is empty for a SecretCopier, which is cluster scoped.
type Offerer struct {
	Kind      string
	Namespace string
	Name      string
}

// String names the offerer as <kind>/<name>, or <kind>/<namespace>/<name>
// for a namespaced one.
func (o Offerer) String() string {
	if o.Namespace == "" {
		return o.Kind + "/" + o.Name
	}
	return o.Kind + "/" + o.Namespace + "/" + o.Name
}

// ImportOffer is one rule's offer of the copy a SecretImporter is named
// after, and whether the importer accepts it.
type ImportOffer struct {
	// From names the resource whose rule makes the offer.
	From Offerer

	// Source names the Secret offered, as <namespace>/<name>.
	Source string

	// Refusal says why the importer refuses the offer, and is empty when it
	// accepts it.
	Refusal copyrules.Refusal
}

// ImportState is where a SecretImporter stands: whether it has imported its
// copy, from which exporter or copier, or why not.
type ImportState struct {
	// Phase says whether the importer has imported its copy, or why not.
	Phase ImportPhase

	// BoundTo names the exporter or copier the copy comes from: the one
	// that wrote it when Phase is Imported, or the first whose offer the
	// importer accepts when Phase is AwaitingCopy. It is nil otherwise.
	BoundTo *Offerer

	// Offers are the offers of the copy into the importer's namespace, one
	// for each exporter or copier rule that makes one, ordered by the
	// resource making it.
	Offers []ImportOffer
}

// ImporterReconciler reconciles one SecretImporter at a time. It copies
// nothing: a SecretImporter is accepted or refused by the reconcilers of
// the rules that offer it a copy. It works out where the importer stands.
type ImporterReconciler struct {
	cache       client.Reader
	convergence convergence.Policy
	feedback    *feedback.Feedback
}

// NewImporterReconciler returns a reconciler that reads SecretImporters,
// SecretExporters, SecretCopiers, namespaces and the metadata of Secrets
// from cache, retries under policy, and reports each SecretImporter's
// status and Events through reports.
func NewImporterReconciler(cache client.Reader, policy convergence.Policy,
	reports *feedback.Feedback) *ImporterReconciler {
	return &ImporterReconciler{cache: cache, convergence: policy, feedback: reports}
}

// SetupWithManager registers the reconciler with mgr. It reconciles a
// SecretImporter when its spec changes, when the Secret named after it in
// its namespace changes, when a SecretExporter or SecretCopier with a rule
// that names its copies after it changes, and when its namespace is
// relabelled or given other owners. Besides, the reconciler's policy
// reconciles each SecretImporter again a backstop interval after its last
// reconcile, and retries a failed one with backoff. The cache must hold the
// indexes eventmap.IndexSecretCopiers, eventmap.IndexSecretExporters and
// eventmap.IndexSecretImporters register.
func (r *ImporterReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return r.convergence.Complete(ctrl.NewControllerManagedBy(mgr).
		Named("secretimporter").
		For(&secretsv1beta1.SecretImporter{}, builder.WithPredicates(predicate.GenerationChangedPredicate{})).
		Watches(&corev1.Secret{}, handler.EnqueueRequestsFromMapFunc(eventmap.SecretImporterForSecret(mgr.GetClient())),
			builder.OnlyMetadata).
		WatchesRawSource(source.Kind(mgr.GetCache(), &secretsv1beta1.SecretExporter{},
			handler.TypedEnqueueRequestsFromMapFunc(
				eventmap.SecretImportersForRules[*secretsv1beta1.SecretExporter](mgr.GetClient())),
			predicate.TypedGenerationChangedPredicate[*secretsv1beta1.SecretExporter]{})).
		WatchesRawSource(source.Kind(mgr.GetCache(), &secretsv1beta1.SecretCopier{},
			handler.TypedEnqueueRequestsFromMapFunc(
				eventmap.SecretImportersForRules[*secretsv1beta1.SecretCopier](mgr.GetClient())),
			predicate.TypedGenerationChangedPredicate[*secretsv1beta1.SecretCopier]{})).
		WatchesRawSource(source.Kind(mgr.GetCache(), &corev1.Namespace{},
			handler.TypedEnqueueRequestsFromMapFunc(eventmap.SecretImportersForNamespace(mgr.GetClient())),
			eventmap.NamespaceSelectionChanged())),
		&secretsv1beta1.SecretImporter{}, r.cache, r)
}

// Reconcile works out where the SecretImporter req names stands, and
// writes its status.
func (r *ImporterReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	importer := &secretsv1beta1.SecretImporter{}
	if err := r.cache.Get(ctx, req.NamespacedName, importer); err != nil {
		if apierrors.IsNotFound(err) {
			r.feedback.Forget(secretsv1beta1.SecretImporterKind, req.NamespacedName)
		}
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	if !importer.DeletionTimestamp.IsZero() {
		r.feedback.Forget(secretsv1beta1.SecretImporterKind, req.NamespacedName)
		return ctrl.Result{}, nil
	}
	state, err := r.State(ctx, importer)
	if err != nil {
		return ctrl.Result{}, err
	}
	log := ctrl.LoggerFrom(ctx).WithValues("phase", state.Phase)
	if state.BoundTo != nil {
		log = log.WithValues("boundTo", state.BoundTo.String())
	}
	log.V(1).Info("Worked out where the SecretImporter stands")
	return r.publish(ctx, importer, &state)
}

// State works out where importer stands, from the offers of the copy named
// after it into its namespace and the copy there.
func (r *ImporterReconciler) State(ctx context.Context, importer *secretsv1beta1.SecretImporter) (ImportState, error) {
	ns := &corev1.Namespace{}
	err := r.cache.Get(ctx, types.NamespacedName{Name: importer.Namespace}, ns)
	switch {
	case apierrors.IsNotFound(err):
		return ImportState{Phase: NoMatchingExporter}, nil
	case err != nil:
		return ImportState{}, fmt.Errorf("reading namespace %s: %w", importer.Namespace, err)
	}

	exporterOffers, err := r.exporterOffers(ctx, importer, ns)
	if err != nil {
		return ImportState{}, err
	}
	copierOffers, err := r.copierOffers(ctx, importer, ns)
	if err != nil {
		return ImportState{}, err
	}
	state := ImportState{Offers: append(exporterOffers, copierOffers...)}
	if len(state.Offers) == 0 {
		state.Phase = NoMatchingExporter
		return state, nil
	}
	slices.SortStableFunc(state.Offers, func(a, b ImportOffer) int {
		return cmp.Or(strings.Compare(a.From.String(), b.From.String()), strings.Compare(a.Source, b.Source))
	})

	writer, copied, err := r.copyWriter(ctx, client.ObjectKeyFromObject(importer))
	if err != nil {
		return ImportState{}, err
	}
	state.Phase = AwaitingAuthorization
	for i := range state.Offers {
		offer := &state.Offers[i]
		if offer.Refusal != copyrules.Accepted {
			continue
		}
		if offer.writes(writer, copied) {
			state.Phase, state.BoundTo = Imported, &offer.From
			return state, nil
		}
		if state.BoundTo == nil {
			state.Phase, state.BoundTo = AwaitingCopy, &offer.From
		}
	}
	return state, nil
}

// writes reports whether the offer's rule wrote a copy whose tracking
// annotations hold writer, its copier-rule, and copied, its secret-name.
func (o *ImportOffer) writes(writer, copied string) bool {
	return writer != "" && writer == secretsv1beta1.CopierRule(o.From.Kind, o.From.Name) && copied == o.Source
}

// exporterOffers lists the offers SecretExporters make of the copy importer
// is named after into ns, its namespace: one for each SecretExporter in
// another namespace with a rule naming its copies after the importer that
// selects ns.
func (r *ImporterReconciler) exporterOffers(ctx context.Context, importer *secretsv1beta1.SecretImporter,
	ns *corev1.Namespace) ([]ImportOffer, error) {
	exporters := &secretsv1beta1.SecretExporterList{}
	if err := r.cache.List(ctx, exporters,
		client.MatchingFields{eventmap.SecretExporterTargetNameIndex: importer.Name}); err != nil {
		return nil, fmt.Errorf("listing the SecretExporters offering the copy: %w", err)
	}
	var offers []ImportOffer
	for i := range exporters.Items {
		exporter := &exporters.Items[i]
		if exporter.Namespace == ns.Name || !exporter.DeletionTimestamp.IsZero() {
			continue
		}
		for j := range exporter.Spec.Rules {
			rule := &exporter.Spec.Rules[j]
			if rule.CopyName(exporter.Name) != importer.Name || !selectors.Namespaces(rule.TargetNamespaces).Match(ns) {
				continue
			}
			offers = append(offers, ImportOffer{
				From:    Offerer{Kind: secretsv1beta1.SecretExporterKind, Namespace: exporter.Namespace, Name: exporter.Name},
				Source:  secretsv1beta1.SecretName(exporter.Namespace, exporter.Name),
				Refusal: copyrules.Authorize(importer, copyrules.ExporterOffer(exporter, rule)),
			})
			break
		}
	}
	return offers, nil
}

// copierOffers lists the offers SecretCopiers make of the copy importer is
// named after into ns, its namespace: one for each rule naming its copies
// after the importer that selects ns and copies a source from another
// namespace.
func (r *ImporterReconciler) copierOffers(ctx context.Context, importer *secretsv1beta1.SecretImporter,
	ns *corev1.Namespace) ([]ImportOffer, error) {
	copiers := &secretsv1beta1.SecretCopierList{}
	if err := r.cache.List(ctx, copiers,
		client.MatchingFields{eventmap.SecretCopierTargetNameIndex: importer.Name}); err != nil {
		return nil, fmt.Errorf("listing the SecretCopiers offering the copy: %w", err)
	}
	var offers []ImportOffer
	for i := range copiers.Items {
		copier := &copiers.Items[i]
		if !copier.DeletionTimestamp.IsZero() {
			continue
		}
		for j := range copier.Spec.Rules {
			rule := &copier.Spec.Rules[j]
			if rule.CopyName() != importer.Name || rule.SourceSecret.Namespace == ns.Name ||
				!selectors.Namespaces(rule.TargetNamespaces).Match(ns) {
				continue
			}
			offers = append(offers, ImportOffer{
				From:    Offerer{Kind: secretsv1beta1.SecretCopierKind, Name: copier.Name},
				Source:  secretsv1beta1.SecretName(rule.SourceSecret.Namespace, rule.SourceSecret.Name),
				Refusal: copyrules.Authorize(importer, copyrules.CopierOffer(rule)),
			})
		}
	}
	return offers, nil
}

// copyWriter returns the tracking annotations of the Secret key names, its
// copier-rule and secret-name, read from the cache's metadata of Secrets.
// Both are empty when there is no such Secret, or it is being deleted.
func (r *ImporterReconciler) copyWriter(ctx context.Context, key types.NamespacedName) (string, string, error) {
	copied := &metav1.PartialObjectMetadata{}
	copied.SetGroupVersionKind(corev1.SchemeGroupVersion.WithKind("Secret"))
	err := r.cache.Get(ctx, key, copied)
	switch {
	case apierrors.IsNotFound(err):
		return "", "", nil
	case err != nil:
		return "", "", fmt.Errorf("reading the copy %s: %w", key, err)
	case !copied.DeletionTimestamp.IsZero():
		return "", "", nil
	}
	annotations := copied.GetAnnotations()
	return annotations[secretsv1beta1.CopierRuleAnnotation], annotations[secretsv1beta1.SecretNameAnnotation], nil
}
