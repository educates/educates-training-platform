package copyengine

import (
	"context"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
)

// Source is the object a rule copies. It holds the object's metadata as
// the cache has it until a copy needs the object's content: the engine
// then reads the object from the API server, once, and the Source holds
// what it read. A Source serves the copies of one reconcile; the next
// reconcile asks for the Source anew.
type Source struct {
	*unstructured.Unstructured

	// read reports whether the Source holds the object read from the API
	// server.
	read bool
}

// Source returns the source named key, holding its metadata as the cache
// has it. It reads nothing from the API server.
func (e *Engine) Source(ctx context.Context, key types.NamespacedName) (*Source, error) {
	metadata := &metav1.PartialObjectMetadata{}
	metadata.SetGroupVersionKind(e.kind.GroupVersionKind)
	if err := e.cache.Get(ctx, key, metadata); err != nil {
		return nil, err
	}
	content, err := runtime.DefaultUnstructuredConverter.ToUnstructured(metadata)
	if err != nil {
		return nil, err
	}
	source := &unstructured.Unstructured{Object: content}
	source.SetGroupVersionKind(e.kind.GroupVersionKind)
	return &Source{Unstructured: source}, nil
}

// read has source hold the object it names as the API server has it, when
// it does not already.
func (e *Engine) read(ctx context.Context, source *Source) error {
	if source.read {
		return nil
	}
	key := types.NamespacedName{Namespace: source.GetNamespace(), Name: source.GetName()}
	live := e.object(key)
	if err := e.reader.Get(ctx, key, live); err != nil {
		return err
	}
	source.Unstructured, source.read = live, true
	return nil
}
