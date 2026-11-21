package exportimport

import (
	"context"
	"fmt"
	"slices"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"

	secretsv1beta1 "github.com/educates/educates-training-platform/secrets-manager/api/v1beta1"
	"github.com/educates/educates-training-platform/secrets-manager/internal/convergence"
	"github.com/educates/educates-training-platform/secrets-manager/internal/copyengine"
	"github.com/educates/educates-training-platform/secrets-manager/internal/feedback"
)

// publish writes the status that report, what reconciling exporter found,
// makes of it, and returns the result of the reconcile. When a write of a
// copy conflicted, the status waits for the reconcile that retries it.
func (r *ExporterReconciler) publish(ctx context.Context, exporter *secretsv1beta1.SecretExporter,
	report exporterReport) (ctrl.Result, error) {
	result, err := exporterResult(report)
	if report.writeConflicted() {
		return result, err
	}

	statusErr := feedback.Publish(ctx, r.feedback, exporter, func(e *secretsv1beta1.SecretExporter) {
		e.Status = exporterStatus(e.Status.Conditions, exporter.Generation, &report)
	})
	return convergence.AfterStatus(result, err, statusErr)
}

// writeConflicted reports whether a write of a copy conflicted with another
// writer's.
func (report *exporterReport) writeConflicted() bool {
	for _, rule := range report.rules {
		for _, target := range rule.targets {
			if apierrors.IsConflict(target.err) {
				return true
			}
		}
	}
	return false
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

	var summary secretsv1beta1.TargetCounts
	for i := range report.rules {
		rule := exporterRuleCounts(&findings, i, &report.rules[i], missing != "")
		summary.TargetNamespaces += rule.TargetNamespaces
		summary.InSync += rule.InSync
		summary.Conflicts += rule.Conflicts
		summary.AwaitingAuthorization += rule.AwaitingAuthorization
		summary.Failures += rule.Failures
		result.Rules = append(result.Rules, rule)
	}
	result.Summary = &secretsv1beta1.CopySummary{
		TargetCounts: summary,
		InSyncRatio:  feedback.Ratio(summary.InSync, summary.TargetNamespaces),
	}
	feedback.SetReady(&result.Conditions, findings.Ready(), generation)
	return result
}

// exporterRuleCounts counts the targets of rule, what reconciling the rule
// of index found, and adds its problems to findings. missing reports that
// the exported Secret is not copied.
func exporterRuleCounts(findings *feedback.Findings, index int, rule *ruleReport,
	missing bool) secretsv1beta1.TargetCounts {
	var counts secretsv1beta1.TargetCounts
	if rule.invalidSelector != nil {
		findings.Add(secretsv1beta1.ReasonInvalidSelector, "", fmt.Sprintf("rules[%d]: %v", index, rule.invalidSelector))
	}
	for _, target := range rule.targets {
		counts.TargetNamespaces++
		switch {
		case missing:
			findings.Add(secretsv1beta1.ReasonSourceMissing, target.namespace, "")
		case target.refusal != Accepted:
			counts.AwaitingAuthorization++
			findings.Add(secretsv1beta1.ReasonAwaitingAuthorization, target.namespace, "")
		case target.err != nil:
			counts.Failures++
			findings.Add(secretsv1beta1.ReasonTargetErrors, target.namespace, target.err.Error())
		case target.result.Outcome.Converged():
			counts.InSync++
		case target.result.Outcome == copyengine.Conflict:
			counts.Conflicts++
			findings.Add(secretsv1beta1.ReasonConflict, target.namespace, "")
		default:
			counts.Failures++
			findings.Add(secretsv1beta1.ReasonTargetErrors, target.namespace, "the copy is being deleted")
		}
	}
	return counts
}
