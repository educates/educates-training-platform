package operator

import (
	"context"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"

	secretsv1beta1 "github.com/educates/educates-training-platform/secrets-manager/api/v1beta1"
	"github.com/educates/educates-training-platform/secrets-manager/internal/copyengine"
)

// lookupRules returns the copy engine's lookup of the resource a copy's
// copier-rule annotation names, read from reader, the cache.
func lookupRules(reader client.Reader) copyengine.RuleLookup {
	return func(ctx context.Context, rule, source string) (copyengine.RuleState, error) {
		resource, ok := secretsv1beta1.RuleResourceOf(rule, source)
		if !ok {
			return copyengine.RuleUnknown, nil
		}
		var obj client.Object = &secretsv1beta1.SecretCopier{}
		if resource.Kind == secretsv1beta1.SecretExporterKind {
			obj = &secretsv1beta1.SecretExporter{}
		}
		err := reader.Get(ctx, resource.Key, obj)
		switch {
		case apierrors.IsNotFound(err):
			return copyengine.RuleGone, nil
		case err != nil:
			return copyengine.RuleUnknown, err
		default:
			return copyengine.RuleExists, nil
		}
	}
}
