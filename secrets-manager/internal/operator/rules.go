package operator

import (
	"context"
	"strings"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	secretsv1beta1 "github.com/educates/educates-training-platform/secrets-manager/api/v1beta1"
	"github.com/educates/educates-training-platform/secrets-manager/internal/copyengine"
)

// lookupRules returns the copy engine's lookup of the resource a copy's
// copier-rule annotation names, read from reader, the cache. A
// SecretCopier is cluster scoped. A SecretExporter exports the Secret of
// its own name in its own namespace, so it lives in the namespace the
// copy's secret-name annotation gives.
func lookupRules(reader client.Reader) copyengine.RuleLookup {
	copierKind := strings.ToLower(secretsv1beta1.SecretCopierKind)
	exporterKind := strings.ToLower(secretsv1beta1.SecretExporterKind)
	return func(ctx context.Context, rule, source string) (copyengine.RuleState, error) {
		kind, name, ok := secretsv1beta1.ParseCopierRule(rule)
		if !ok {
			return copyengine.RuleUnknown, nil
		}
		var obj client.Object
		key := types.NamespacedName{Name: name}
		switch kind {
		case copierKind:
			obj = &secretsv1beta1.SecretCopier{}
		case exporterKind:
			namespace, _, ok := strings.Cut(source, "/")
			if !ok || namespace == "" {
				return copyengine.RuleUnknown, nil
			}
			obj, key.Namespace = &secretsv1beta1.SecretExporter{}, namespace
		default:
			return copyengine.RuleUnknown, nil
		}
		err := reader.Get(ctx, key, obj)
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
