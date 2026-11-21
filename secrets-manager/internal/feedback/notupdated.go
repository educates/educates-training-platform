package feedback

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"
)

// NotUpdatedCopy is a copy another writer made immutable, which could not
// take a change of its source.
type NotUpdatedCopy struct {
	// Copy names the copy.
	Copy *corev1.ObjectReference

	// Source names the copy's source, and SourceVersion is the resource
	// version of the source the copy did not take.
	Source        types.NamespacedName
	SourceVersion string

	// Field is the field of the source whose change the copy did not take,
	// such as a Secret's data or type.
	Field string
}

// CopyNotUpdated records a Warning CopyNotUpdated Event about obj, the
// SecretCopier or SecretExporter whose rule converges notUpdated, naming the
// copy, its source and the field the copy did not take. It records it once
// for each version of the source the copy does not take, however often
// converging the copy fails again.
func (f *Feedback) CopyNotUpdated(obj client.Object, notUpdated NotUpdatedCopy) {
	gvk, err := apiutil.GVKForObject(obj, f.scheme)
	if err != nil {
		return
	}
	key := types.NamespacedName{Namespace: notUpdated.Copy.Namespace, Name: notUpdated.Copy.Name}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.stopped || f.notUpdated[key] == notUpdated.SourceVersion {
		return
	}
	regarding := f.reference(obj, gvk)
	if regarding == nil {
		return
	}
	f.notUpdated[key] = notUpdated.SourceVersion
	f.record(regarding, notUpdated.Copy, corev1.EventTypeWarning, EventCopyNotUpdated, actions[gvk.Kind],
		fmt.Sprintf("%s %s, the copy of %s, is immutable and was not updated: its source's %s changed",
			notUpdated.Copy.Kind, key, notUpdated.Source, notUpdated.Field))
}

// ForgetCopy drops what Feedback holds of the copy key names, which no
// longer exists.
func (f *Feedback) ForgetCopy(key types.NamespacedName) {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.notUpdated, key)
}
