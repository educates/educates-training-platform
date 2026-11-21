package eventmap

import (
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
)

// Deletions passes only the deletion events of objects of type T.
func Deletions[T client.Object]() predicate.TypedPredicate[T] {
	return predicate.TypedFuncs[T]{
		CreateFunc:  func(event.TypedCreateEvent[T]) bool { return false },
		UpdateFunc:  func(event.TypedUpdateEvent[T]) bool { return false },
		DeleteFunc:  func(event.TypedDeleteEvent[T]) bool { return true },
		GenericFunc: func(event.TypedGenericEvent[T]) bool { return false },
	}
}
