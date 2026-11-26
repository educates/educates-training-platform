package eventmap

import (
	"maps"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
)

// NamespaceSelectionChanged passes the events that can change which rules
// select a namespace, or copy a source in it: its creation, a change of its
// labels or owners, and the start of its deletion, from which no rule
// selects it and no source in it is copied. A namespace is being deleted
// long before it is gone, so its removal changes nothing more.
func NamespaceSelectionChanged() predicate.TypedPredicate[*corev1.Namespace] {
	return predicate.TypedFuncs[*corev1.Namespace]{
		CreateFunc: func(event.TypedCreateEvent[*corev1.Namespace]) bool { return true },
		UpdateFunc: func(e event.TypedUpdateEvent[*corev1.Namespace]) bool {
			return !maps.Equal(e.ObjectOld.Labels, e.ObjectNew.Labels) ||
				!equality.Semantic.DeepEqual(e.ObjectOld.OwnerReferences, e.ObjectNew.OwnerReferences) ||
				e.ObjectOld.Status.Phase != e.ObjectNew.Status.Phase ||
				e.ObjectOld.DeletionTimestamp.IsZero() != e.ObjectNew.DeletionTimestamp.IsZero()
		},
		DeleteFunc:  func(event.TypedDeleteEvent[*corev1.Namespace]) bool { return false },
		GenericFunc: func(event.TypedGenericEvent[*corev1.Namespace]) bool { return false },
	}
}
