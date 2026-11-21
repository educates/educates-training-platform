package operator

import (
	"context"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"

	secretsv1beta1 "github.com/educates/educates-training-platform/secrets-manager/api/v1beta1"
	"github.com/educates/educates-training-platform/secrets-manager/internal/copyengine"
)

// lookupRules returns the copy engine's lookup of the rule that wrote a
// copy, read from reader, the cache.
func lookupRules(reader client.Reader) copyengine.RuleLookup {
	return func(ctx context.Context, writer, source, name string) (copyengine.RuleState, error) {
		resource, ok := secretsv1beta1.RuleResourceOf(writer, source)
		if !ok {
			return copyengine.RuleUnknown, nil
		}
		copier, exporter := &secretsv1beta1.SecretCopier{}, &secretsv1beta1.SecretExporter{}
		var obj client.Object = copier
		copies := func() bool { return copierCopies(copier, source, name) }
		if resource.Kind == secretsv1beta1.SecretExporterKind {
			obj = exporter
			copies = func() bool { return exporterCopies(exporter, source, name) }
		}
		err := reader.Get(ctx, resource.Key, obj)
		switch {
		case apierrors.IsNotFound(err):
			return copyengine.RuleGone, nil
		case err != nil:
			return copyengine.RuleUnknown, err
		case copies():
			return copyengine.RuleExists, nil
		default:
			return copyengine.RuleDropped, nil
		}
	}
}

// copierCopies reports whether a rule of copier copies source, named as the
// secret-name annotation names it, under name.
func copierCopies(copier *secretsv1beta1.SecretCopier, source, name string) bool {
	for i := range copier.Spec.Rules {
		rule := &copier.Spec.Rules[i]
		if rule.CopyName() == name &&
			secretsv1beta1.SecretName(rule.SourceSecret.Namespace, rule.SourceSecret.Name) == source {
			return true
		}
	}
	return false
}

// exporterCopies reports whether a rule of exporter copies source, named as
// the secret-name annotation names it, under name.
func exporterCopies(exporter *secretsv1beta1.SecretExporter, source, name string) bool {
	if secretsv1beta1.SecretName(exporter.Namespace, exporter.Name) != source {
		return false
	}
	for i := range exporter.Spec.Rules {
		if exporter.Spec.Rules[i].CopyName(exporter.Name) == name {
			return true
		}
	}
	return false
}
