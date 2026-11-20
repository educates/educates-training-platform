package eventmap

import (
	"context"
	"maps"
	"slices"
	"strings"

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

// secretCopierRuleKind is the kind a copier-rule annotation names a
// SecretCopier by.
var secretCopierRuleKind = strings.ToLower(secretsv1beta1.SecretCopierKind)

// IndexSecretCopiers registers the field indexes the SecretCopier map
// functions look up.
func IndexSecretCopiers(ctx context.Context, indexer client.FieldIndexer) error {
	if err := indexer.IndexField(ctx, &secretsv1beta1.SecretCopier{}, SecretCopierSourceIndex,
		func(obj client.Object) []string {
			return ruleValues(obj, func(rule *secretsv1beta1.SecretCopierRule) string {
				return rule.SourceSecret.Namespace + "/" + rule.SourceSecret.Name
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
		if kind, name, ok := secretsv1beta1.ParseCopierRule(
			secret.GetAnnotations()[secretsv1beta1.CopierRuleAnnotation]); ok && kind == secretCopierRuleKind {
			names[name] = struct{}{}
		}
		lookups := map[string]string{
			SecretCopierSourceIndex:     secret.GetNamespace() + "/" + secret.GetName(),
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

// requests returns a reconcile request for each name of a cluster-scoped
// resource.
func requests(names map[string]struct{}) []reconcile.Request {
	result := make([]reconcile.Request, 0, len(names))
	for _, name := range slices.Sorted(maps.Keys(names)) {
		result = append(result, reconcile.Request{NamespacedName: types.NamespacedName{Name: name}})
	}
	return result
}
