// Package copyengine converges one copy of a source object in one target
// namespace: it decides what the copy should hold, given the source and the
// shape a rule gives the copy, compares that with the target the API server
// holds, and writes only a real difference.
//
// Copies are written with server-side apply, forced, under one field
// manager. The applied configuration holds the copy's payload, taken from
// the source, its labels and its annotations. The API server's record of
// field ownership is the record of what the engine set: a label or
// annotation it stops applying is removed, one another writer set is left
// alone, and a field another writer changed is taken back by the next
// apply. Keyed payload, such as a Secret's data, is replaced whole: a key
// outside the source's is removed with a JSON merge patch, conditional on
// the resource version the engine last saw.
//
// The engine knows nothing of the resource whose rule asks for the copy,
// and assumes nothing of the copied kind beyond what its Kind says.
package copyengine

import (
	"bytes"
	"context"
	"encoding/json"
	"maps"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// Outcome says what converging a copy did.
type Outcome string

const (
	// Created means the copy did not exist and was written.
	Created Outcome = "Created"

	// Updated means the copy differed from what it should hold and was
	// written.
	Updated Outcome = "Updated"

	// InSync means the copy already held what it should, and nothing was
	// written.
	InSync Outcome = "InSync"

	// TargetDeleting means the target is being deleted, and nothing was
	// written: its deletion brings the next attempt.
	TargetDeleting Outcome = "TargetDeleting"
)

// Copy is one copy of a source for the engine to converge.
type Copy struct {
	// Source is the object copied, as read from the API server.
	Source *unstructured.Unstructured

	// Namespace and Name name the copy.
	Namespace string
	Name      string

	// Labels are set on the copy besides the source's labels, and win over
	// a source label with the same key.
	Labels map[string]string

	// Annotations are set on the copy. The source's annotations are never
	// copied.
	Annotations map[string]string
}

// Engine converges copies of one kind of object.
type Engine struct {
	kind         *Kind
	reader       client.Reader
	writer       client.Client
	fieldManager string
}

// New returns an engine copying objects of kind. It reads sources and
// targets through reader, which must read from the API server rather than
// a cache, and writes copies through writer, applying them as
// fieldManager.
func New(kind *Kind, reader client.Reader, writer client.Client, fieldManager string) *Engine {
	return &Engine{kind: kind, reader: reader, writer: writer, fieldManager: fieldManager}
}

// Source reads the source named key from the API server.
func (e *Engine) Source(ctx context.Context, key types.NamespacedName) (*unstructured.Unstructured, error) {
	source := e.object(key)
	if err := e.reader.Get(ctx, key, source); err != nil {
		return nil, err
	}
	return source, nil
}

// Converge brings the copy c describes to what it should hold, writing it
// only when it differs.
func (e *Engine) Converge(ctx context.Context, c Copy) (Outcome, error) {
	desired := e.desired(c)

	key := types.NamespacedName{Namespace: c.Namespace, Name: c.Name}
	live := e.object(key)
	err := e.reader.Get(ctx, key, live)
	var current *unstructured.Unstructured
	outcome := InSync
	switch {
	case apierrors.IsNotFound(err):
		outcome = Created
	case err != nil:
		return "", err
	case live.GetDeletionTimestamp() != nil:
		return TargetDeleting, nil
	default:
		applied, err := e.kind.Extract(live, e.fieldManager)
		if err != nil {
			return "", err
		}
		if sameContent(desired.Object, applied) {
			current = live
		} else {
			outcome = Updated
		}
	}

	if current == nil {
		if current, err = e.apply(ctx, desired); err != nil {
			return "", err
		}
	}
	removed, err := e.removeStrayKeys(ctx, current, desired)
	if err != nil {
		return "", err
	}
	if removed && outcome == InSync {
		outcome = Updated
	}
	return outcome, nil
}

// desired is the configuration the engine applies for c: the copy's name,
// labels and annotations, and the payload the source's kind copies.
func (e *Engine) desired(c Copy) *unstructured.Unstructured {
	desired := e.object(types.NamespacedName{Namespace: c.Namespace, Name: c.Name})
	labels := maps.Clone(c.Source.GetLabels())
	if labels == nil {
		labels = map[string]string{}
	}
	maps.Copy(labels, c.Labels)
	if len(labels) > 0 {
		desired.SetLabels(labels)
	}
	if len(c.Annotations) > 0 {
		desired.SetAnnotations(c.Annotations)
	}
	for _, field := range e.kind.WholeFields {
		if value, ok := c.Source.Object[field]; ok {
			desired.Object[field] = runtime.DeepCopyJSONValue(value)
		}
	}
	for _, field := range e.kind.KeyedFields {
		if entries, ok := c.Source.Object[field].(map[string]any); ok && len(entries) > 0 {
			desired.Object[field] = runtime.DeepCopyJSONValue(entries)
		}
	}
	return desired
}

// apply writes desired with server-side apply, forced, and returns the
// object the API server holds after it.
func (e *Engine) apply(ctx context.Context, desired *unstructured.Unstructured) (*unstructured.Unstructured, error) {
	applied := desired.DeepCopy()
	err := e.writer.Apply(ctx, client.ApplyConfigurationFromUnstructured(applied),
		client.FieldOwner(e.fieldManager), client.ForceOwnership)
	if err != nil {
		return nil, err
	}
	return applied, nil
}

// removeStrayKeys removes from current the keys of its keyed fields that
// desired does not hold, and reports whether it wrote. The patch carries
// current's resource version, so it fails with a conflict when the copy
// has changed since.
func (e *Engine) removeStrayKeys(ctx context.Context, current, desired *unstructured.Unstructured) (bool, error) {
	patch := map[string]any{}
	for _, field := range e.kind.KeyedFields {
		held, _ := current.Object[field].(map[string]any)
		wanted, _ := desired.Object[field].(map[string]any)
		removals := map[string]any{}
		for key := range held {
			if _, ok := wanted[key]; !ok {
				removals[key] = nil
			}
		}
		if len(removals) > 0 {
			patch[field] = removals
		}
	}
	if len(patch) == 0 {
		return false, nil
	}
	patch["metadata"] = map[string]any{"resourceVersion": current.GetResourceVersion()}
	body, err := json.Marshal(patch)
	if err != nil {
		return false, err
	}
	target := e.object(types.NamespacedName{Namespace: current.GetNamespace(), Name: current.GetName()})
	if err := e.writer.Patch(ctx, target, client.RawPatch(types.MergePatchType, body),
		client.FieldOwner(e.fieldManager)); err != nil {
		return false, err
	}
	return true, nil
}

// object returns an empty object of the engine's kind named key.
func (e *Engine) object(key types.NamespacedName) *unstructured.Unstructured {
	obj := &unstructured.Unstructured{}
	obj.SetGroupVersionKind(e.kind.GroupVersionKind)
	obj.SetNamespace(key.Namespace)
	obj.SetName(key.Name)
	return obj
}

// sameContent reports whether two unstructured contents hold the same
// fields and values.
func sameContent(a, b map[string]any) bool {
	rawA, errA := json.Marshal(a)
	rawB, errB := json.Marshal(b)
	return errA == nil && errB == nil && bytes.Equal(rawA, rawB)
}
