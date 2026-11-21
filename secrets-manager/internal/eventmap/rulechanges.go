package eventmap

import (
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
)

// RuleChanges passes the events of objects of type T that change their
// rules, a change of their generation, and their deletions.
func RuleChanges[T client.Object]() predicate.TypedPredicate[T] {
	return predicate.TypedFuncs[T]{
		CreateFunc: func(event.TypedCreateEvent[T]) bool { return false },
		UpdateFunc: func(e event.TypedUpdateEvent[T]) bool {
			return e.ObjectOld.GetGeneration() != e.ObjectNew.GetGeneration()
		},
		DeleteFunc:  func(event.TypedDeleteEvent[T]) bool { return true },
		GenericFunc: func(event.TypedGenericEvent[T]) bool { return false },
	}
}
