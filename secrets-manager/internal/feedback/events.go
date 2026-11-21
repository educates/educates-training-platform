package feedback

import (
	"time"
	"unicode/utf8"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/reference"
	"sigs.k8s.io/controller-runtime/pkg/client"

	secretsv1beta1 "github.com/educates/educates-training-platform/secrets-manager/api/v1beta1"
)

// The reasons of the Events Feedback records.
const (
	// EventNotReady is the reason of the Warning about a resource that has
	// not been Ready for the grace period.
	EventNotReady = "NotReady"

	// EventRecovered is the reason of the Normal Event about a resource that
	// got a NotReady Warning and is Ready again.
	EventRecovered = "Recovered"

	// EventImported is the reason of the Normal Event about a SecretImporter
	// that imports its Secret.
	EventImported = "Imported"
)

// maxNoteLength is the longest note an events.k8s.io Event takes, in bytes.
const maxNoteLength = 1024

// actions name, for each kind, what the secrets-manager does for it, as an
// Event's action says.
var actions = map[string]string{
	secretsv1beta1.SecretCopierKind:   "Copy",
	secretsv1beta1.SecretExporterKind: "Export",
	secretsv1beta1.SecretImporterKind: "Import",
	secretsv1beta1.SecretInjectorKind: "Inject",
}

// readiness is what Feedback holds of a resource that is not Ready.
type readiness struct {
	// uid tells the resource from another of the same name.
	uid types.UID
	// regarding names the resource in its Events.
	regarding *corev1.ObjectReference
	// action is what the secrets-manager does for the resource.
	action string
	// since is when the resource stopped being Ready.
	since time.Time
	// ready is the resource's latest Ready condition.
	ready metav1.Condition
	// warned reports that the resource got its NotReady Warning.
	warned bool
	// timer raises the Warning once the grace period has passed.
	timer *time.Timer
}

// stop stops the pending Warning of the resource, if any.
func (r *readiness) stop() {
	if r.timer != nil {
		r.timer.Stop()
	}
}

// observe records the Events that the change of the Ready condition of obj,
// the resource key names, from previous to current calls for, and arms or
// stops its NotReady Warning. previous is nil when the resource had no
// Ready condition.
func (f *Feedback) observe(key resourceKey, gvk schema.GroupVersionKind, obj client.Object,
	previous, current *metav1.Condition) {
	if current == nil {
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.stopped {
		return
	}

	state := f.readiness[key]
	if state != nil && state.uid != obj.GetUID() {
		state.stop()
		delete(f.readiness, key)
		state = nil
	}

	if current.Status == metav1.ConditionTrue {
		if state != nil {
			state.stop()
			delete(f.readiness, key)
			if state.warned {
				f.record(state.regarding, corev1.EventTypeNormal, EventRecovered, state.action, current.Message)
			}
		}
		if current.Reason == secretsv1beta1.ReasonImported && !imported(previous) {
			if regarding := f.reference(obj, gvk); regarding != nil {
				f.record(regarding, corev1.EventTypeNormal, EventImported, actions[gvk.Kind], current.Message)
			}
		}
		return
	}

	if state == nil {
		regarding := f.reference(obj, gvk)
		if regarding == nil {
			return
		}
		state = &readiness{
			uid:       obj.GetUID(),
			regarding: regarding,
			action:    actions[gvk.Kind],
			since:     current.LastTransitionTime.Time,
		}
		// A resource not Ready for the whole grace period before this
		// process started had the time to get its Warning from the process
		// before it.
		state.warned = state.since.Add(f.grace).Before(f.started)
		f.readiness[key] = state
	}
	state.ready = *current
	if !state.warned && state.timer == nil {
		state.timer = time.AfterFunc(time.Until(state.since.Add(f.grace)), func() { f.warn(key, state) })
	}
}

// warn records the NotReady Warning of the resource key names, when state
// is still what Feedback holds of it and it has not got the Warning yet.
func (f *Feedback) warn(key resourceKey, state *readiness) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.stopped || f.readiness[key] != state || state.warned {
		return
	}
	state.warned = true
	f.record(state.regarding, corev1.EventTypeWarning, EventNotReady, state.action,
		"Not Ready since "+state.since.UTC().Format(time.RFC3339)+": "+state.ready.Reason+": "+state.ready.Message)
}

// record records an Event of eventType and reason about regarding, with
// note, shortened to the length an Event takes.
func (f *Feedback) record(regarding *corev1.ObjectReference, eventType, reason, action, note string) {
	f.recorder.Eventf(regarding, nil, eventType, reason, action, "%s", truncate(note, maxNoteLength))
}

// reference names obj, of gvk, in an Event, or is nil when it cannot.
func (f *Feedback) reference(obj client.Object, gvk schema.GroupVersionKind) *corev1.ObjectReference {
	typed, ok := obj.DeepCopyObject().(client.Object)
	if !ok {
		return nil
	}
	typed.GetObjectKind().SetGroupVersionKind(gvk)
	regarding, err := reference.GetReference(f.scheme, typed)
	if err != nil {
		return nil
	}
	return regarding
}

// imported reports whether ready, a Ready condition or nil, says a
// SecretImporter has imported its Secret.
func imported(ready *metav1.Condition) bool {
	return ready != nil && ready.Status == metav1.ConditionTrue && ready.Reason == secretsv1beta1.ReasonImported
}

// readyOf returns the Ready condition in the status of obj, or nil when it
// has none.
func readyOf(obj client.Object) *metav1.Condition {
	content, err := runtime.DefaultUnstructuredConverter.ToUnstructured(obj)
	if err != nil {
		return nil
	}
	items, _, _ := unstructured.NestedSlice(content, "status", "conditions")
	conditions := make([]metav1.Condition, 0, len(items))
	for _, item := range items {
		entry, ok := item.(map[string]any)
		if !ok {
			continue
		}
		condition := metav1.Condition{}
		if runtime.DefaultUnstructuredConverter.FromUnstructured(entry, &condition) == nil {
			conditions = append(conditions, condition)
		}
	}
	return meta.FindStatusCondition(conditions, secretsv1beta1.ReadyCondition)
}

// truncate shortens text to at most limit bytes, on a rune boundary.
func truncate(text string, limit int) string {
	if len(text) <= limit {
		return text
	}
	cut := limit - len("...")
	for cut > 0 && !utf8.RuneStart(text[cut]) {
		cut--
	}
	return text[:cut] + "..."
}
