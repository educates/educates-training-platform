package secretcopier

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

// publish writes the status that reports, what reconciling the rules of
// copier with converger found, make of it, and returns the result of the
// reconcile. When a write of a copy conflicted, the status waits for the
// reconcile that retries it.
func (r *Reconciler) publish(ctx context.Context, copier *secretsv1beta1.SecretCopier,
	converger *copyrules.Converger, reports []ruleReport) (ctrl.Result, error) {
	result, err := converger.Result()
	if converger.Conflicted() {
		return result, err
	}

	statusErr := feedback.Publish(ctx, r.feedback, copier, func(c *secretsv1beta1.SecretCopier) {
		c.Status = status(c.Status.Conditions, copier.Generation, reports)
	})
	return convergence.AfterStatus(result, err, statusErr)
}

// status is the status reports make of a SecretCopier at generation: the
// targets of each rule counted, and the Ready condition of the dominant
// problem, set on conditions, the SecretCopier's.
func status(conditions []metav1.Condition, generation int64, reports []ruleReport) secretsv1beta1.SecretCopierStatus {
	result := secretsv1beta1.SecretCopierStatus{
		ObservedGeneration: generation,
		Conditions:         slices.Clone(conditions),
		Rules:              make([]secretsv1beta1.SecretCopierRuleStatus, 0, len(reports)),
	}
	var findings feedback.Findings
	counts := make([]secretsv1beta1.TargetCounts, 0, len(reports))
	for i := range reports {
		rule := ruleStatus(&findings, i, &reports[i])
		result.Rules = append(result.Rules, rule)
		counts = append(counts, rule.TargetCounts)
	}
	result.Summary = copyrules.Summary(counts)
	feedback.SetReady(&result.Conditions, findings.Ready(), generation)
	return result
}

// ruleStatus counts the targets of report, what reconciling the rule of
// index found, and adds its problems to findings.
func ruleStatus(findings *feedback.Findings, index int, report *ruleReport) secretsv1beta1.SecretCopierRuleStatus {
	rule := secretsv1beta1.SecretCopierRuleStatus{
		SourceSecret: secretsv1beta1.SecretReference{Name: report.source.Name, Namespace: report.source.Namespace},
		SourceExists: report.sourceExists,
	}
	var missing string
	switch {
	case report.invalidSelector != nil:
		findings.Add(secretsv1beta1.ReasonInvalidSelector, "", fmt.Sprintf("rules[%d]: %v", index, report.invalidSelector))
	case report.err != nil:
		findings.Add(secretsv1beta1.ReasonTargetErrors, "", report.err.Error())
	case !report.sourceExists:
		missing = fmt.Sprintf("source Secret %s does not exist", report.source)
	case report.sourceTerminating:
		missing = fmt.Sprintf("source Secret %s is in a namespace being deleted", report.source)
	}
	if missing != "" {
		findings.Add(secretsv1beta1.ReasonSourceMissing, "", missing)
	}
	rule.TargetCounts = copyrules.Count(findings, report.targets, missing != "")
	return rule
}
