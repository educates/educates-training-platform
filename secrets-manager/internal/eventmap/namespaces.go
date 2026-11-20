package eventmap

import (
	"maps"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
)

// NamespaceSelectionChanged passes the events that can change which rules
// select a namespace: its creation, and a change of its labels or owners.
func NamespaceSelectionChanged() predicate.TypedPredicate[*corev1.Namespace] {
	return predicate.TypedFuncs[*corev1.Namespace]{
		CreateFunc: func(event.TypedCreateEvent[*corev1.Namespace]) bool { return true },
		UpdateFunc: func(e event.TypedUpdateEvent[*corev1.Namespace]) bool {
			return !maps.Equal(e.ObjectOld.Labels, e.ObjectNew.Labels) ||
				!equality.Semantic.DeepEqual(e.ObjectOld.OwnerReferences, e.ObjectNew.OwnerReferences)
		},
		DeleteFunc:  func(event.TypedDeleteEvent[*corev1.Namespace]) bool { return false },
		GenericFunc: func(event.TypedGenericEvent[*corev1.Namespace]) bool { return false },
	}
}
