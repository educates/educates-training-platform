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

// SecretExporterTargetNameIndex indexes SecretExporters by the names of
// their rules' copies.
const SecretExporterTargetNameIndex = "secretexporter.targetSecretName"

// IndexSecretExporters registers the field index the SecretExporter map
// functions look up.
func IndexSecretExporters(ctx context.Context, indexer client.FieldIndexer) error {
	return indexer.IndexField(ctx, &secretsv1beta1.SecretExporter{}, SecretExporterTargetNameIndex, copyNames)
}

// copyNames returns the distinct names the rules of a SecretCopier or
// SecretExporter give their copies.
func copyNames(obj client.Object) []string {
	switch resource := obj.(type) {
	case *secretsv1beta1.SecretCopier:
		return ruleValues(resource, (*secretsv1beta1.SecretCopierRule).CopyName)
	case *secretsv1beta1.SecretExporter:
		names := map[string]struct{}{}
		for i := range resource.Spec.Rules {
			names[resource.Spec.Rules[i].CopyName(resource.Name)] = struct{}{}
		}
		return slices.Sorted(maps.Keys(names))
	default:
		return nil
	}
}

// SecretExportersForSecret maps an event on a Secret, of which it needs
// only the metadata, to the SecretExporters it affects: the one that
// exports it, the one its copier-rule annotation names, which lives in the
// namespace its secret-name annotation gives, and those with a rule that
// names a copy after it. It reads the SecretExporters from reader, a cache
// holding the index IndexSecretExporters registers.
func SecretExportersForSecret(reader client.Reader) handler.MapFunc {
	return func(ctx context.Context, secret client.Object) []reconcile.Request {
		keys := map[types.NamespacedName]struct{}{}
		source := client.ObjectKeyFromObject(secret)
		if err := reader.Get(ctx, source, &secretsv1beta1.SecretExporter{}); err == nil {
			keys[source] = struct{}{}
		}
		if writer, ok := ruleResourceOf(secret); ok && writer.Kind == secretsv1beta1.SecretExporterKind {
			keys[writer.Key] = struct{}{}
		}
		exporters := &secretsv1beta1.SecretExporterList{}
		if err := reader.List(ctx, exporters,
			client.MatchingFields{SecretExporterTargetNameIndex: secret.GetName()}); err != nil {
			ctrl.LoggerFrom(ctx).Error(err, "Listing SecretExporters for a Secret failed", "secret", source)
		}
		for i := range exporters.Items {
			keys[client.ObjectKeyFromObject(&exporters.Items[i])] = struct{}{}
		}
		return namespacedRequests(keys)
	}
}

// SecretExportersForNamespace maps an event on a namespace to the
// SecretExporters with a rule that targets it, evaluating the selectors of
// the SecretExporters reader holds.
func SecretExportersForNamespace(reader client.Reader) handler.TypedMapFunc[*corev1.Namespace, reconcile.Request] {
	return func(ctx context.Context, ns *corev1.Namespace) []reconcile.Request {
		exporters := &secretsv1beta1.SecretExporterList{}
		if err := reader.List(ctx, exporters); err != nil {
			ctrl.LoggerFrom(ctx).Error(err, "Listing SecretExporters for a namespace failed", "namespace", ns.Name)
			return nil
		}
		keys := map[types.NamespacedName]struct{}{}
		for i := range exporters.Items {
			exporter := &exporters.Items[i]
			if exporter.Namespace == ns.Name {
				continue
			}
			for _, rule := range exporter.Spec.Rules {
				if selectors.Namespaces(rule.TargetNamespaces).Match(ns) {
					keys[client.ObjectKeyFromObject(exporter)] = struct{}{}
					break
				}
			}
		}
		return namespacedRequests(keys)
	}
}

// SecretExportersSharingCopies maps a change to the rules of a SecretCopier
// or SecretExporter, or its deletion, to the other SecretExporters with a
// rule that names its copies as one of the changed resource's rules does,
// or did before the change. Copies kept from them while a rule of the
// changed resource targeted them are theirs to take over once none does.
// On a change, it is called with the resource before and after it. It
// reads the SecretExporters from reader, a cache holding the index
// IndexSecretExporters registers.
func SecretExportersSharingCopies[T client.Object](reader client.Reader) handler.TypedMapFunc[T, reconcile.Request] {
	return func(ctx context.Context, changed T) []reconcile.Request {
		keys := map[types.NamespacedName]struct{}{}
		for _, name := range copyNames(changed) {
			exporters := &secretsv1beta1.SecretExporterList{}
			if err := reader.List(ctx, exporters, client.MatchingFields{SecretExporterTargetNameIndex: name}); err != nil {
				ctrl.LoggerFrom(ctx).Error(err, "Listing SecretExporters for a changed resource failed",
					"resource", client.ObjectKeyFromObject(changed))
				continue
			}
			for i := range exporters.Items {
				if exporters.Items[i].UID != changed.GetUID() {
					keys[client.ObjectKeyFromObject(&exporters.Items[i])] = struct{}{}
				}
			}
		}
		return namespacedRequests(keys)
	}
}

// SecretCopiersSharingExportedCopies maps a change to the rules of a
// SecretExporter, or its deletion, to the SecretCopiers with a rule that
// names its copies as one of the changed SecretExporter's rules does, or
// did before the change, so that they take over those copies once none of
// its rules targets them. On a change, it is called with the
// SecretExporter before and after it. It reads the SecretCopiers from
// reader, a cache holding the indexes IndexSecretCopiers registers.
func SecretCopiersSharingExportedCopies(reader client.Reader) handler.TypedMapFunc[*secretsv1beta1.SecretExporter,
	reconcile.Request] {
	return func(ctx context.Context, changed *secretsv1beta1.SecretExporter) []reconcile.Request {
		names := map[string]struct{}{}
		for _, name := range copyNames(changed) {
			copiers := &secretsv1beta1.SecretCopierList{}
			if err := reader.List(ctx, copiers, client.MatchingFields{SecretCopierTargetNameIndex: name}); err != nil {
				ctrl.LoggerFrom(ctx).Error(err, "Listing SecretCopiers for a changed SecretExporter failed",
					"secretExporter", client.ObjectKeyFromObject(changed))
				continue
			}
			for _, copier := range copiers.Items {
				names[copier.Name] = struct{}{}
			}
		}
		return requests(names)
	}
}

// namespacedRequests returns a reconcile request for each key of a
// namespaced resource.
func namespacedRequests(keys map[types.NamespacedName]struct{}) []reconcile.Request {
	result := make([]reconcile.Request, 0, len(keys))
	for _, key := range slices.SortedFunc(maps.Keys(keys), compareKeys) {
		result = append(result, reconcile.Request{NamespacedName: key})
	}
	return result
}

// compareKeys orders keys by namespace, then name.
func compareKeys(a, b types.NamespacedName) int {
	if c := strings.Compare(a.Namespace, b.Namespace); c != 0 {
		return c
	}
	return strings.Compare(a.Name, b.Name)
}
