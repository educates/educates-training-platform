package exportimport

import (
	"context"
	"fmt"
	"slices"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"

	secretsv1beta1 "github.com/educates/educates-training-platform/secrets-manager/api/v1beta1"
	"github.com/educates/educates-training-platform/secrets-manager/internal/convergence"
	"github.com/educates/educates-training-platform/secrets-manager/internal/feedback"
)

// publish writes the status state, where importer stands, makes of it, and
// returns the result of the reconcile.
func (r *ImporterReconciler) publish(ctx context.Context, importer *secretsv1beta1.SecretImporter,
	state *ImportState) (ctrl.Result, error) {
	statusErr := feedback.Publish(ctx, r.feedback, importer, func(i *secretsv1beta1.SecretImporter) {
		i.Status = importerStatus(i.Status.Conditions, importer, state)
	})
	return convergence.AfterStatus(ctrl.Result{}, nil, statusErr)
}

// importerStatus is the status state makes of importer: its Ready
// condition, set on conditions, the importer's, the resource its copy comes
// from and the copy's name.
func importerStatus(conditions []metav1.Condition, importer *secretsv1beta1.SecretImporter,
	state *ImportState) secretsv1beta1.SecretImporterStatus {
	result := secretsv1beta1.SecretImporterStatus{
		ObservedGeneration: importer.Generation,
		Conditions:         slices.Clone(conditions),
		TargetSecretName:   importer.Name,
	}
	if state.BoundTo != nil {
		result.BoundTo = &secretsv1beta1.ImportSource{
			Kind:      state.BoundTo.Kind,
			Namespace: state.BoundTo.Namespace,
			Name:      state.BoundTo.Name,
		}
	}

	ready := metav1.Condition{Status: metav1.ConditionFalse}
	switch state.Phase {
	case Imported:
		ready.Status, ready.Reason = metav1.ConditionTrue, secretsv1beta1.ReasonImported
		ready.Message = fmt.Sprintf("Imported Secret %s from %s", importer.Name, describe(state.BoundTo))
	case AwaitingCopy:
		ready.Reason = secretsv1beta1.ReasonAwaitingCopy
		ready.Message = fmt.Sprintf("Accepts the offer of %s; the copy is not there yet, and its status says why",
			describe(state.BoundTo))
	case AwaitingAuthorization:
		refused := make([]string, 0, len(state.Offers))
		for _, offer := range state.Offers {
			refused = append(refused, fmt.Sprintf("%s (%s)", describe(&offer.From), offer.Refusal))
		}
		ready.Reason = secretsv1beta1.ReasonAwaitingAuthorization
		ready.Message = "Refuses every offer of Secret " + importer.Name + ": " + feedback.List(refused, ", ")
	default:
		ready.Reason = secretsv1beta1.ReasonNoMatchingExporter
		ready.Message = fmt.Sprintf("No SecretExporter or SecretCopier rule offers Secret %s to namespace %s",
			importer.Name, importer.Namespace)
	}
	feedback.SetReady(&result.Conditions, ready, importer.Generation)
	return result
}

// describe names the resource offerer names, as a message says it.
func describe(offerer *Offerer) string {
	if offerer == nil {
		return "no resource"
	}
	if offerer.Namespace == "" {
		return offerer.Kind + " " + offerer.Name
	}
	return offerer.Kind + " " + offerer.Namespace + "/" + offerer.Name
}
