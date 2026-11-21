package feedback

import (
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	secretsv1beta1 "github.com/educates/educates-training-platform/secrets-manager/api/v1beta1"
)

// maxListed is how many namespaces, or details, a Ready message names
// before it counts the rest.
const maxListed = 5

// problemOrder lists the reasons a resource that is not in sync reports,
// the dominant first.
var problemOrder = []string{
	secretsv1beta1.ReasonInvalidSelector,
	secretsv1beta1.ReasonSourceMissing,
	secretsv1beta1.ReasonTargetErrors,
	secretsv1beta1.ReasonConflict,
	secretsv1beta1.ReasonAwaitingAuthorization,
}

// problemWording is how a Ready message words each problem, and the word
// that introduces the namespaces it affects.
var problemWording = map[string]struct{ lead, preposition string }{
	secretsv1beta1.ReasonInvalidSelector: {"Rules with invalid selectors select nothing", "in"},
	secretsv1beta1.ReasonSourceMissing:   {"Not copied", "into"},
	secretsv1beta1.ReasonTargetErrors:    {"Failed", "in"},
	secretsv1beta1.ReasonConflict: {
		"Secrets the secrets-manager did not create, or that another rule copies from another source, " +
			"hold the copy's name", "in",
	},
	secretsv1beta1.ReasonAwaitingAuthorization: {"No SecretImporter accepts the copy", "in"},
}

// Findings collect the problems a reconcile found on the targets of a
// SecretCopier, SecretExporter or SecretInjector, and make its Ready
// condition of them.
type Findings struct {
	problems map[string]*problem
}

// problem is one reason a resource is not in sync: the namespaces it
// affects and what is wrong.
type problem struct {
	namespaces map[string]bool
	details    map[string]bool
}

// Add records a problem of reason, affecting namespace when it is not
// empty, with detail, saying what is wrong, when it is not empty.
func (f *Findings) Add(reason, namespace, detail string) {
	if f.problems == nil {
		f.problems = map[string]*problem{}
	}
	p, ok := f.problems[reason]
	if !ok {
		p = &problem{namespaces: map[string]bool{}, details: map[string]bool{}}
		f.problems[reason] = p
	}
	if namespace != "" {
		p.namespaces[namespace] = true
	}
	if detail != "" {
		p.details[detail] = true
	}
}

// Ready is the Ready condition the findings make: True with reason InSync
// when there are none, and otherwise False with the reason of the dominant
// problem, its message naming up to five of the namespaces it affects. The
// message lists namespaces and details in order, so that the same findings
// always make the same message.
func (f *Findings) Ready() metav1.Condition {
	for _, reason := range problemOrder {
		p, ok := f.problems[reason]
		if !ok {
			continue
		}
		wording := problemWording[reason]
		message := wording.lead
		if len(p.namespaces) > 0 {
			message += " " + wording.preposition + " namespaces " +
				List(slices.Sorted(maps.Keys(p.namespaces)), ", ")
		}
		if len(p.details) > 0 {
			message += ": " + List(slices.Sorted(maps.Keys(p.details)), "; ")
		}
		return metav1.Condition{
			Type:    secretsv1beta1.ReadyCondition,
			Status:  metav1.ConditionFalse,
			Reason:  reason,
			Message: message,
		}
	}
	return metav1.Condition{
		Type:    secretsv1beta1.ReadyCondition,
		Status:  metav1.ConditionTrue,
		Reason:  secretsv1beta1.ReasonInSync,
		Message: "Every target is in sync",
	}
}

// List joins the first five of items with separator, and counts the
// others, as a Ready message names them.
func List(items []string, separator string) string {
	if len(items) <= maxListed {
		return strings.Join(items, separator)
	}
	return fmt.Sprintf("%s and %d more", strings.Join(items[:maxListed], separator), len(items)-maxListed)
}

// SetReady sets ready as the Ready condition of conditions, observed at
// generation. Its last transition time is now, to the second, when its
// status changes, and stays otherwise.
func SetReady(conditions *[]metav1.Condition, ready metav1.Condition, generation int64) {
	ready.Type = secretsv1beta1.ReadyCondition
	ready.ObservedGeneration = generation
	ready.LastTransitionTime = metav1.NewTime(time.Now().Truncate(time.Second))
	meta.SetStatusCondition(conditions, ready)
}

// Ratio is the in-sync ratio kubectl get shows: inSync out of matched, such
// as 5/6.
func Ratio(inSync, matched int32) string {
	return fmt.Sprintf("%d/%d", inSync, matched)
}
