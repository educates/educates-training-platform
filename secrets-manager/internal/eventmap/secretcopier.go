package eventmap

import (
	"context"
	"maps"
	"slices"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	secretsv1beta1 "github.com/educates/educates-training-platform/secrets-manager/api/v1beta1"
	"github.com/educates/educates-training-platform/secrets-manager/internal/selectors"
)

const (
	// SecretCopierSourceIndex indexes SecretCopiers by the Secrets their
	// rules copy, as <namespace>/<name>.
	SecretCopierSourceIndex = "secretcopier.sourceSecret"

	// SecretCopierTargetNameIndex indexes SecretCopiers by the names of
	// their rules' copies.
	SecretCopierTargetNameIndex = "secretcopier.targetSecretName"
)

// IndexSecretCopiers registers the field indexes the SecretCopier map
// functions look up.
func IndexSecretCopiers(ctx context.Context, indexer client.FieldIndexer) error {
	if err := indexer.IndexField(ctx, &secretsv1beta1.SecretCopier{}, SecretCopierSourceIndex,
		func(obj client.Object) []string {
			return ruleValues(obj, func(rule *secretsv1beta1.SecretCopierRule) string {
				return secretsv1beta1.SecretName(rule.SourceSecret.Namespace, rule.SourceSecret.Name)
			})
		}); err != nil {
		return err
	}
	return indexer.IndexField(ctx, &secretsv1beta1.SecretCopier{}, SecretCopierTargetNameIndex,
		func(obj client.Object) []string {
			return ruleValues(obj, (*secretsv1beta1.SecretCopierRule).CopyName)
		})
}

// ruleValues returns the distinct values value gives the rules of a
// SecretCopier.
func ruleValues(obj client.Object, value func(*secretsv1beta1.SecretCopierRule) string) []string {
	copier, ok := obj.(*secretsv1beta1.SecretCopier)
	if !ok {
		return nil
	}
	values := map[string]struct{}{}
	for i := range copier.Spec.Rules {
		values[value(&copier.Spec.Rules[i])] = struct{}{}
	}
	return slices.Sorted(maps.Keys(values))
}

// SecretCopiersForSecret maps an event on a Secret, of which it needs only
// the metadata, to the SecretCopiers it affects: the one its copier-rule
// annotation names, and those with a rule that copies it or names a copy
// after it. It reads the SecretCopiers from reader, a cache holding the
// indexes IndexSecretCopiers registers.
func SecretCopiersForSecret(reader client.Reader) handler.MapFunc {
	return func(ctx context.Context, secret client.Object) []reconcile.Request {
		names := map[string]struct{}{}
		if writer, ok := ruleResourceOf(secret); ok && writer.Kind == secretsv1beta1.SecretCopierKind {
			names[writer.Key.Name] = struct{}{}
		}
		lookups := map[string]string{
			SecretCopierSourceIndex:     secretsv1beta1.SecretName(secret.GetNamespace(), secret.GetName()),
			SecretCopierTargetNameIndex: secret.GetName(),
		}
		for index, value := range lookups {
			copiers := &secretsv1beta1.SecretCopierList{}
			if err := reader.List(ctx, copiers, client.MatchingFields{index: value}); err != nil {
				ctrl.LoggerFrom(ctx).Error(err, "Listing SecretCopiers for a Secret failed", "index", index,
					"secret", client.ObjectKeyFromObject(secret))
				continue
			}
			for _, copier := range copiers.Items {
				names[copier.Name] = struct{}{}
			}
		}
		return requests(names)
	}
}

// SecretCopiersSharingCopies maps the deletion of a SecretCopier to the
// other SecretCopiers with a rule that names its copies as one of the
// deleted SecretCopier's rules does. Those copies were kept from them while
// the deleted SecretCopier existed, and are theirs to take over. It reads
// the SecretCopiers from reader, a cache holding the indexes
// IndexSecretCopiers registers.
func SecretCopiersSharingCopies(reader client.Reader) handler.TypedMapFunc[*secretsv1beta1.SecretCopier,
	reconcile.Request] {
	return func(ctx context.Context, deleted *secretsv1beta1.SecretCopier) []reconcile.Request {
		names := map[string]struct{}{}
		for _, copyName := range ruleValues(deleted, (*secretsv1beta1.SecretCopierRule).CopyName) {
			copiers := &secretsv1beta1.SecretCopierList{}
			if err := reader.List(ctx, copiers, client.MatchingFields{SecretCopierTargetNameIndex: copyName}); err != nil {
				ctrl.LoggerFrom(ctx).Error(err, "Listing SecretCopiers for a deleted SecretCopier failed",
					"secretCopier", deleted.Name)
				continue
			}
			for _, copier := range copiers.Items {
				if copier.UID != deleted.UID {
					names[copier.Name] = struct{}{}
				}
			}
		}
		return requests(names)
	}
}

// SecretCopiersForNamespace maps an event on a namespace to the
// SecretCopiers with a rule that targets it, evaluating the selectors of
// the SecretCopiers reader holds.
func SecretCopiersForNamespace(reader client.Reader) handler.TypedMapFunc[*corev1.Namespace, reconcile.Request] {
	return func(ctx context.Context, ns *corev1.Namespace) []reconcile.Request {
		copiers := &secretsv1beta1.SecretCopierList{}
		if err := reader.List(ctx, copiers); err != nil {
			ctrl.LoggerFrom(ctx).Error(err, "Listing SecretCopiers for a namespace failed", "namespace", ns.Name)
			return nil
		}
		names := map[string]struct{}{}
		for _, copier := range copiers.Items {
			for _, rule := range copier.Spec.Rules {
				if rule.SourceSecret.Namespace != ns.Name && selectors.Namespaces(rule.TargetNamespaces).Match(ns) {
					names[copier.Name] = struct{}{}
					break
				}
			}
		}
		return requests(names)
	}
}

// ruleResourceOf resolves the tracking annotations of secret to the
// resource whose rule wrote it, and reports whether they name one.
func ruleResourceOf(secret client.Object) (secretsv1beta1.RuleResource, bool) {
	annotations := secret.GetAnnotations()
	return secretsv1beta1.RuleResourceOf(annotations[secretsv1beta1.CopierRuleAnnotation],
		annotations[secretsv1beta1.SecretNameAnnotation])
}

// requests returns a reconcile request for each name of a cluster-scoped
// resource.
func requests(names map[string]struct{}) []reconcile.Request {
	result := make([]reconcile.Request, 0, len(names))
	for _, name := range slices.Sorted(maps.Keys(names)) {
		result = append(result, reconcile.Request{NamespacedName: types.NamespacedName{Name: name}})
	}
	return result
}
