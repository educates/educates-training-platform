package secretinjector

import (
	"context"
	"errors"
	"fmt"
	"math"
	"slices"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	ctrl "sigs.k8s.io/controller-runtime"

	secretsv1beta1 "github.com/educates/educates-training-platform/secrets-manager/api/v1beta1"
	"github.com/educates/educates-training-platform/secrets-manager/internal/feedback"
)

// publish writes the status outcome, what injecting the Secrets of injector
// found and did, makes of it, and returns the result of the reconcile. When
// a service account changed while it was written, the status waits for the
// reconcile that retries it.
func (r *Reconciler) publish(ctx context.Context, injector *secretsv1beta1.SecretInjector,
	outcome *Outcome) (ctrl.Result, error) {
	err := outcome.Err()
	if outcome.Conflicted {
		if err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{RequeueAfter: conflictRetryDelay}, nil
	}

	updated := injector.DeepCopy()
	updated.Status = status(injector, outcome)
	statusErr := r.feedback.Publish(ctx, injector, updated)
	switch {
	case statusErr == nil:
		return ctrl.Result{}, err
	case apierrors.IsConflict(statusErr) && err == nil:
		return ctrl.Result{RequeueAfter: conflictRetryDelay}, nil
	case apierrors.IsConflict(statusErr):
		return ctrl.Result{}, err
	default:
		return ctrl.Result{}, errors.Join(err, fmt.Errorf("writing the status: %w", statusErr))
	}
}

// status is the status outcome makes of injector: what each rule matched
// and injected, counted, and the Ready condition of the dominant problem.
func status(injector *secretsv1beta1.SecretInjector, outcome *Outcome) secretsv1beta1.SecretInjectorStatus {
	result := secretsv1beta1.SecretInjectorStatus{
		ObservedGeneration: injector.Generation,
		Conditions:         slices.Clone(injector.Status.Conditions),
		Rules:              make([]secretsv1beta1.InjectionCounts, 0, len(outcome.Rules)),
	}
	var findings feedback.Findings
	var summary secretsv1beta1.InjectionCounts
	for i := range outcome.Rules {
		rule := &outcome.Rules[i]
		if rule.InvalidSelector != nil {
			findings.Add(secretsv1beta1.ReasonInvalidSelector, "", fmt.Sprintf("rules[%d]: %v", i, rule.InvalidSelector))
		}
		for _, failure := range rule.Failures {
			findings.Add(secretsv1beta1.ReasonTargetErrors, failure.Namespace, failure.Err.Error())
		}
		counts := secretsv1beta1.InjectionCounts{
			TargetNamespaces:       count(rule.TargetNamespaces),
			ServiceAccountsMatched: count(rule.ServiceAccountsMatched),
			InjectionsInSync:       count(rule.ServiceAccountsInSync),
			Failures:               count(len(rule.Failures)),
		}
		summary.TargetNamespaces += counts.TargetNamespaces
		summary.ServiceAccountsMatched += counts.ServiceAccountsMatched
		summary.InjectionsInSync += counts.InjectionsInSync
		summary.Failures += counts.Failures
		result.Rules = append(result.Rules, counts)
	}
	result.Summary = &secretsv1beta1.InjectionSummary{
		InjectionCounts: summary,
		InSyncRatio:     feedback.Ratio(summary.InjectionsInSync, summary.ServiceAccountsMatched),
	}
	feedback.SetReady(&result.Conditions, findings.Ready(), injector.Generation)
	return result
}

// count is n as a status count, which no cluster exceeds.
func count(n int) int32 {
	return int32(min(n, math.MaxInt32))
}
