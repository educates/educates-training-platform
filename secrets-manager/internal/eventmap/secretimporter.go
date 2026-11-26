package eventmap

import (
	"context"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	secretsv1beta1 "github.com/educates/educates-training-platform/secrets-manager/api/v1beta1"
)

// SecretImporterNameIndex indexes SecretImporters by name, which is the
// name of the copy each accepts, so that the importers named after a copy
// are found in every namespace.
const SecretImporterNameIndex = "secretimporter.name"

// IndexSecretImporters registers the field index the SecretImporter map
// functions look up.
func IndexSecretImporters(ctx context.Context, indexer client.FieldIndexer) error {
	return indexer.IndexField(ctx, &secretsv1beta1.SecretImporter{}, SecretImporterNameIndex,
		func(obj client.Object) []string { return []string{obj.GetName()} })
}

// SecretCopiersForImporter maps an event on a SecretImporter to the
// SecretCopiers with a rule that names its copies after it, which the
// importer may now accept or refuse. It reads the SecretCopiers from
// reader, a cache holding the indexes IndexSecretCopiers registers.
func SecretCopiersForImporter(reader client.Reader) handler.TypedMapFunc[*secretsv1beta1.SecretImporter,
	reconcile.Request] {
	return func(ctx context.Context, importer *secretsv1beta1.SecretImporter) []reconcile.Request {
		copiers := &secretsv1beta1.SecretCopierList{}
		if err := reader.List(ctx, copiers, client.MatchingFields{SecretCopierTargetNameIndex: importer.Name}); err != nil {
			ctrl.LoggerFrom(ctx).Error(err, "Listing SecretCopiers for a SecretImporter failed",
				"secretImporter", client.ObjectKeyFromObject(importer))
			return nil
		}
		names := map[string]struct{}{}
		for _, copier := range copiers.Items {
			names[copier.Name] = struct{}{}
		}
		return requests(names)
	}
}

// SecretExportersForImporter maps an event on a SecretImporter to the
// SecretExporters in other namespaces with a rule that names its copies
// after it, which the importer may now accept or refuse. It reads the
// SecretExporters from reader, a cache holding the index
// IndexSecretExporters registers.
func SecretExportersForImporter(reader client.Reader) handler.TypedMapFunc[*secretsv1beta1.SecretImporter,
	reconcile.Request] {
	return func(ctx context.Context, importer *secretsv1beta1.SecretImporter) []reconcile.Request {
		exporters := &secretsv1beta1.SecretExporterList{}
		if err := reader.List(ctx, exporters,
			client.MatchingFields{SecretExporterTargetNameIndex: importer.Name}); err != nil {
			ctrl.LoggerFrom(ctx).Error(err, "Listing SecretExporters for a SecretImporter failed",
				"secretImporter", client.ObjectKeyFromObject(importer))
			return nil
		}
		keys := map[types.NamespacedName]struct{}{}
		for i := range exporters.Items {
			if exporters.Items[i].Namespace != importer.Namespace {
				keys[client.ObjectKeyFromObject(&exporters.Items[i])] = struct{}{}
			}
		}
		return namespacedRequests(keys)
	}
}

// SecretImporterForSecret maps an event on a Secret, of which it needs only
// the metadata, to the SecretImporter named after it in its namespace, when
// reader, a cache, holds one.
func SecretImporterForSecret(reader client.Reader) handler.MapFunc {
	return func(ctx context.Context, secret client.Object) []reconcile.Request {
		key := client.ObjectKeyFromObject(secret)
		if err := reader.Get(ctx, key, &secretsv1beta1.SecretImporter{}); err != nil {
			return nil
		}
		return []reconcile.Request{{NamespacedName: key}}
	}
}

// SecretImportersForRules maps an event on a SecretCopier or SecretExporter
// to the SecretImporters, in every namespace, named after a copy one of its
// rules makes, whose offers it may have changed. It reads the
// SecretImporters from reader, a cache holding the index
// IndexSecretImporters registers.
func SecretImportersForRules[T client.Object](reader client.Reader) handler.TypedMapFunc[T, reconcile.Request] {
	return func(ctx context.Context, resource T) []reconcile.Request {
		keys := map[types.NamespacedName]struct{}{}
		for _, name := range copyNames(resource) {
			importers := &secretsv1beta1.SecretImporterList{}
			if err := reader.List(ctx, importers, client.MatchingFields{SecretImporterNameIndex: name}); err != nil {
				ctrl.LoggerFrom(ctx).Error(err, "Listing SecretImporters for a rule's copies failed",
					"resource", client.ObjectKeyFromObject(resource))
				continue
			}
			for i := range importers.Items {
				keys[client.ObjectKeyFromObject(&importers.Items[i])] = struct{}{}
			}
		}
		return namespacedRequests(keys)
	}
}

// SecretImportersForNamespace maps an event on a namespace, which can
// change the rules that offer copies into it, to the SecretImporters in it
// that reader, a cache, holds.
func SecretImportersForNamespace(reader client.Reader) handler.TypedMapFunc[*corev1.Namespace, reconcile.Request] {
	return func(ctx context.Context, ns *corev1.Namespace) []reconcile.Request {
		importers := &secretsv1beta1.SecretImporterList{}
		if err := reader.List(ctx, importers, client.InNamespace(ns.Name)); err != nil {
			ctrl.LoggerFrom(ctx).Error(err, "Listing SecretImporters for a namespace failed", "namespace", ns.Name)
			return nil
		}
		keys := map[types.NamespacedName]struct{}{}
		for i := range importers.Items {
			keys[client.ObjectKeyFromObject(&importers.Items[i])] = struct{}{}
		}
		return namespacedRequests(keys)
	}
}
