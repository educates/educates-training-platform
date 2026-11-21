package exportimport

import (
	"context"
	"fmt"
	"slices"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"

	secretsv1beta1 "github.com/educates/educates-training-platform/secrets-manager/api/v1beta1"
	"github.com/educates/educates-training-platform/secrets-manager/internal/convergence"
	"github.com/educates/educates-training-platform/secrets-manager/internal/copyrules"
	"github.com/educates/educates-training-platform/secrets-manager/internal/feedback"
)

// publish writes the status that report, what reconciling exporter with
// converger found, makes of it, and returns the result of the reconcile.
// When a write of a copy conflicted, the status waits for the reconcile
// that retries it.
func (r *ExporterReconciler) publish(ctx context.Context, exporter *secretsv1beta1.SecretExporter,
	converger *copyrules.Converger, report exporterReport) (ctrl.Result, error) {
	result, err := converger.Result()
	if converger.Conflicted() {
		return result, err
	}

	statusErr := feedback.Publish(ctx, r.feedback, exporter, func(e *secretsv1beta1.SecretExporter) {
		e.Status = exporterStatus(e.Status.Conditions, exporter.Generation, &report)
	})
	return convergence.AfterStatus(result, err, statusErr)
}

// exporterStatus is the status report makes of a SecretExporter at
// generation: whether the exported Secret exists, the targets of each rule
// counted, and the Ready condition of the dominant problem, set on
// conditions, the SecretExporter's.
func exporterStatus(conditions []metav1.Condition, generation int64,
	report *exporterReport) secretsv1beta1.SecretExporterStatus {
	sourceExists := report.sourceExists
	result := secretsv1beta1.SecretExporterStatus{
		ObservedGeneration: generation,
		Conditions:         slices.Clone(conditions),
		SourceExists:       &sourceExists,
		Rules:              make([]secretsv1beta1.TargetCounts, 0, len(report.rules)),
	}
	var findings feedback.Findings
	var missing string
	switch {
	case report.err != nil:
		findings.Add(secretsv1beta1.ReasonTargetErrors, "", report.err.Error())
	case !report.sourceExists:
		missing = fmt.Sprintf("exported Secret %s does not exist", report.source)
	case report.sourceTerminating:
		missing = fmt.Sprintf("exported Secret %s is in a namespace being deleted", report.source)
	}
	if missing != "" {
		findings.Add(secretsv1beta1.ReasonSourceMissing, "", missing)
	}

	for i := range report.rules {
		rule := &report.rules[i]
		if rule.invalidSelector != nil {
			findings.Add(secretsv1beta1.ReasonInvalidSelector, "", fmt.Sprintf("rules[%d]: %v", i, rule.invalidSelector))
		}
		result.Rules = append(result.Rules, copyrules.Count(&findings, rule.targets, missing != ""))
	}
	result.Summary = copyrules.Summary(result.Rules)
	feedback.SetReady(&result.Conditions, findings.Ready(), generation)
	return result
}
