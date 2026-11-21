package operator

import (
	"context"
	"strings"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	secretsv1beta1 "github.com/educates/educates-training-platform/secrets-manager/api/v1beta1"
	"github.com/educates/educates-training-platform/secrets-manager/internal/copyengine"
	"github.com/educates/educates-training-platform/secrets-manager/internal/copyrules"
	"github.com/educates/educates-training-platform/secrets-manager/internal/selectors"
)

// lookupRules returns the copy engine's lookup of the rule that wrote a
// copy, read from reader, the cache. A resource targets a copy while it is
// not being deleted and one of its rules both selects the copy and is
// accepted by the SecretImporter named after the copy in its namespace,
// when there is one: a rule the importer refuses no longer updates the
// copy.
func lookupRules(reader client.Reader) copyengine.RuleLookup {
	return func(ctx context.Context, writer, source string, key types.NamespacedName) (copyengine.RuleState, error) {
		resource, ok := secretsv1beta1.RuleResourceOf(writer, source)
		if !ok {
			if exportedWithoutSource(writer, source) {
				return copyengine.RuleDropped, nil
			}
			return copyengine.RuleUnknown, nil
		}
		copier, exporter := &secretsv1beta1.SecretCopier{}, &secretsv1beta1.SecretExporter{}
		var obj client.Object = copier
		targets := func(ns *corev1.Namespace, importer *secretsv1beta1.SecretImporter) bool {
			return copierTargets(copier, source, key.Name, ns, importer)
		}
		if resource.Kind == secretsv1beta1.SecretExporterKind {
			obj = exporter
			targets = func(ns *corev1.Namespace, importer *secretsv1beta1.SecretImporter) bool {
				return exporterTargets(exporter, source, key.Name, ns, importer)
			}
		}
		if err := reader.Get(ctx, resource.Key, obj); err != nil {
			if apierrors.IsNotFound(err) {
				return copyengine.RuleGone, nil
			}
			return copyengine.RuleUnknown, err
		}
		if !obj.GetDeletionTimestamp().IsZero() {
			return copyengine.RuleGone, nil
		}

		ns := &corev1.Namespace{}
		if err := reader.Get(ctx, types.NamespacedName{Name: key.Namespace}, ns); err != nil {
			if apierrors.IsNotFound(err) {
				return copyengine.RuleDropped, nil
			}
			return copyengine.RuleUnknown, err
		}
		importer, err := copyrules.Importer(ctx, reader, key)
		switch {
		case err != nil:
			return copyengine.RuleUnknown, err
		case targets(ns, importer):
			return copyengine.RuleTargets, nil
		default:
			return copyengine.RuleDropped, nil
		}
	}
}

// exportedWithoutSource reports whether writer, a copy's copier-rule
// annotation, names a SecretExporter while source, its secret-name
// annotation, names no Secret. No SecretExporter exports such a copy, since
// each exports the Secret of its own name in its own namespace, so the copy
// is an orphan, as a SecretCopier's copy without its secret-name is.
func exportedWithoutSource(writer, source string) bool {
	kind, _, ok := secretsv1beta1.ParseCopierRule(writer)
	if !ok || kind != strings.ToLower(secretsv1beta1.SecretExporterKind) {
		return false
	}
	_, named := secretsv1beta1.ParseSecretName(source)
	return !named
}

// copierTargets reports whether a rule of copier targets the copy name in
// ns: whether it copies source, named as the secret-name annotation names
// it, under name, selects ns, and is accepted by importer, the
// SecretImporter named after the copy in ns, nil when there is none.
func copierTargets(copier *secretsv1beta1.SecretCopier, source, name string, ns *corev1.Namespace,
	importer *secretsv1beta1.SecretImporter) bool {
	for i := range copier.Spec.Rules {
		rule := &copier.Spec.Rules[i]
		if rule.CopyName() == name &&
			secretsv1beta1.SecretName(rule.SourceSecret.Namespace, rule.SourceSecret.Name) == source &&
			rule.SourceSecret.Namespace != ns.Name &&
			selectors.Namespaces(rule.TargetNamespaces).Match(ns) &&
			copyrules.Authorize(importer, copyrules.CopierOffer(rule)) == copyrules.Accepted {
			return true
		}
	}
	return false
}

// exporterTargets reports whether a rule of exporter targets the copy name
// in ns: whether exporter exports source, named as the secret-name
// annotation names it, and one of its rules names its copies name, selects
// ns, and is accepted by importer, the SecretImporter named after the copy
// in ns, nil when there is none.
func exporterTargets(exporter *secretsv1beta1.SecretExporter, source, name string, ns *corev1.Namespace,
	importer *secretsv1beta1.SecretImporter) bool {
	if secretsv1beta1.SecretName(exporter.Namespace, exporter.Name) != source || exporter.Namespace == ns.Name {
		return false
	}
	for i := range exporter.Spec.Rules {
		rule := &exporter.Spec.Rules[i]
		if rule.CopyName(exporter.Name) == name && selectors.Namespaces(rule.TargetNamespaces).Match(ns) &&
			copyrules.Authorize(importer, copyrules.ExporterOffer(exporter, rule)) == copyrules.Accepted {
			return true
		}
	}
	return false
}
