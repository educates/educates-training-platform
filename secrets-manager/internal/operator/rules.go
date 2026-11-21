package operator

import (
	"context"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	secretsv1beta1 "github.com/educates/educates-training-platform/secrets-manager/api/v1beta1"
	"github.com/educates/educates-training-platform/secrets-manager/internal/copyengine"
	"github.com/educates/educates-training-platform/secrets-manager/internal/selectors"
)

// lookupRules returns the copy engine's lookup of the rule that wrote a
// copy, read from reader, the cache.
func lookupRules(reader client.Reader) copyengine.RuleLookup {
	return func(ctx context.Context, writer, source string, key types.NamespacedName) (copyengine.RuleState, error) {
		resource, ok := secretsv1beta1.RuleResourceOf(writer, source)
		if !ok {
			return copyengine.RuleUnknown, nil
		}
		copier, exporter := &secretsv1beta1.SecretCopier{}, &secretsv1beta1.SecretExporter{}
		var obj client.Object = copier
		targets := func(ns *corev1.Namespace) bool { return copierTargets(copier, source, key.Name, ns) }
		if resource.Kind == secretsv1beta1.SecretExporterKind {
			obj = exporter
			targets = func(ns *corev1.Namespace) bool { return exporterTargets(exporter, source, key.Name, ns) }
		}
		if err := reader.Get(ctx, resource.Key, obj); err != nil {
			if apierrors.IsNotFound(err) {
				return copyengine.RuleGone, nil
			}
			return copyengine.RuleUnknown, err
		}

		ns := &corev1.Namespace{}
		err := reader.Get(ctx, types.NamespacedName{Name: key.Namespace}, ns)
		switch {
		case apierrors.IsNotFound(err):
			return copyengine.RuleDropped, nil
		case err != nil:
			return copyengine.RuleUnknown, err
		case targets(ns):
			return copyengine.RuleTargets, nil
		default:
			return copyengine.RuleDropped, nil
		}
	}
}

// copierTargets reports whether a rule of copier targets the copy name in
// ns: whether it copies source, named as the secret-name annotation names
// it, under name, and selects ns.
func copierTargets(copier *secretsv1beta1.SecretCopier, source, name string, ns *corev1.Namespace) bool {
	for i := range copier.Spec.Rules {
		rule := &copier.Spec.Rules[i]
		if rule.CopyName() == name &&
			secretsv1beta1.SecretName(rule.SourceSecret.Namespace, rule.SourceSecret.Name) == source &&
			rule.SourceSecret.Namespace != ns.Name &&
			selectors.Namespaces(rule.TargetNamespaces).Match(ns) {
			return true
		}
	}
	return false
}

// exporterTargets reports whether a rule of exporter targets the copy name
// in ns: whether exporter exports source, named as the secret-name
// annotation names it, and one of its rules names its copies name and
// selects ns.
func exporterTargets(exporter *secretsv1beta1.SecretExporter, source, name string, ns *corev1.Namespace) bool {
	if secretsv1beta1.SecretName(exporter.Namespace, exporter.Name) != source || exporter.Namespace == ns.Name {
		return false
	}
	for i := range exporter.Spec.Rules {
		rule := &exporter.Spec.Rules[i]
		if rule.CopyName(exporter.Name) == name && selectors.Namespaces(rule.TargetNamespaces).Match(ns) {
			return true
		}
	}
	return false
}
