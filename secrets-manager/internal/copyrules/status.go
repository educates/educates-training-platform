package copyrules

import (
	secretsv1beta1 "github.com/educates/educates-training-platform/secrets-manager/api/v1beta1"
	"github.com/educates/educates-training-platform/secrets-manager/internal/copyengine"
	"github.com/educates/educates-training-platform/secrets-manager/internal/feedback"
)

// Count counts targets, the targets of one rule, for the rule's status, and
// adds their problems to findings. notCopied reports that the rule's source
// is not copied, because it does not exist or its namespace is being
// deleted, so that no target holds its copy.
func Count(findings *feedback.Findings, targets []Target, notCopied bool) secretsv1beta1.TargetCounts {
	var counts secretsv1beta1.TargetCounts
	for _, target := range targets {
		counts.TargetNamespaces++
		switch {
		case notCopied:
			findings.Add(secretsv1beta1.ReasonSourceMissing, target.Namespace, "")
		case target.Refusal != Accepted:
			counts.AwaitingAuthorization++
			findings.Add(secretsv1beta1.ReasonAwaitingAuthorization, target.Namespace, "")
		case target.Err != nil:
			counts.Failures++
			findings.Add(secretsv1beta1.ReasonTargetErrors, target.Namespace, target.Err.Error())
		case target.Result.Outcome.Converged():
			counts.InSync++
		case target.Result.Outcome == copyengine.OwnershipConflict:
			counts.Conflicts++
			findings.Add(secretsv1beta1.ReasonConflict, target.Namespace, "")
		default:
			counts.Failures++
			findings.Add(secretsv1beta1.ReasonTargetErrors, target.Namespace, "the copy is being deleted")
		}
	}
	return counts
}

// Summary sums the counts of a resource's rules into its status summary.
func Summary(rules []secretsv1beta1.TargetCounts) *secretsv1beta1.CopySummary {
	var sum secretsv1beta1.TargetCounts
	for _, rule := range rules {
		sum.TargetNamespaces += rule.TargetNamespaces
		sum.InSync += rule.InSync
		sum.Conflicts += rule.Conflicts
		sum.AwaitingAuthorization += rule.AwaitingAuthorization
		sum.Failures += rule.Failures
	}
	return &secretsv1beta1.CopySummary{TargetCounts: sum, InSyncRatio: feedback.Ratio(sum.InSync, sum.TargetNamespaces)}
}
