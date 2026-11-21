// Package copyengine converges one copy of a source object in one target
// namespace: it decides whether the target is the asking rule's to write,
// what the copy should hold, given the source and the shape the rule gives
// the copy, compares that with the target the API server holds, and writes
// only a real difference.
//
// A copy carries two tracking annotations: copier-rule names the resource
// whose rule wrote it, and secret-name the source it copies. They decide
// whose a target is:
//
//   - A target without copier-rule was not written by a rule. It is left
//     untouched, as a conflict.
//   - A target whose copier-rule names the resource asking for the copy is
//     that resource's own.
//   - A target whose copier-rule names another resource that exists stays
//     that resource's. It agrees with the asking rule when secret-name
//     names the same source, and is a conflict otherwise.
//   - A target whose copier-rule names a resource that no longer exists is
//     an orphan, which the asking rule adopts.
//
// Copies are written with server-side apply, forced, under one field
// manager. The applied configuration holds the copy's payload, taken from
// the source, its labels, its annotations and its owner reference. The API
// server's record of field ownership is the record of what the engine set:
// a label, annotation or owner reference it stops applying is removed, one
// another writer set is left alone, and a field another writer changed is
// taken back by the next apply. Adopting an orphan applies the adopting
// rule's whole configuration, which replaces the owner reference the engine
// applied for the orphan's former rule. Keyed payload, such as a Secret's
// data, holds the source's keys, or those of them the rule selects, and is
// replaced whole: any other key is removed with a JSON merge patch,
// conditional on the resource version the engine last saw.
//
// A copy whose fixed field, such as a Secret's type, differs from the
// source's is deleted and written anew. A copy another writer made
// immutable, whose payload must change, is a failure; it is never deleted.
//
// The engine knows nothing of the resource whose rule asks for the copy
// beyond the copier-rule value naming it and the owner it gives the copy,
// and assumes nothing of the copied kind beyond what its Kind says.
package copyengine

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	secretsv1beta1 "github.com/educates/educates-training-platform/secrets-manager/api/v1beta1"
)

// ErrImmutable is the failure of a copy another writer made immutable,
// whose payload must change.
var ErrImmutable = errors.New("the copy is immutable")

// Copy is one copy of a source for the engine to converge.
type Copy struct {
	// Source is the object copied, as read from the API server.
	Source *unstructured.Unstructured

	// Namespace and Name name the copy.
	Namespace string
	Name      string

	// Rule names the resource whose rule asks for the copy, as the
	// copier-rule annotation does: <lowercased kind>/<name>.
	Rule string

	// Labels are set on the copy besides the source's labels, and win over
	// a source label with the same key.
	Labels map[string]string

	// Annotations are set on the copy besides the tracking annotations.
	// The source's annotations are never copied.
	Annotations map[string]string

	// Keys, when not empty, limits the copy's keyed fields, such as a
	// Secret's data, to these keys, those the source has. When empty, the
	// copy holds every key of the source.
	Keys []string

	// Owner, when set, is given an owner reference on the copy, so that
	// Kubernetes deletes the copy with it.
	Owner *Owner
}

// Owner is the resource a copy's owner reference names. The reference
// marks it as the copy's controller, and blocks a foreground deletion of it
// until the copy is gone.
type Owner struct {
	APIVersion string
	Kind       string
	Name       string
	UID        types.UID
}

// Engine converges copies of one kind of object.
type Engine struct {
	kind         *Kind
	reader       client.Reader
	writer       client.Client
	fieldManager string
	rules        RuleLookup
}

// New returns an engine copying objects of kind. It reads sources and
// targets through reader, which must read from the API server rather than
// a cache, writes copies through writer, applying them as fieldManager,
// and asks rules whether the resource a target's copier-rule annotation
// names still exists.
func New(kind *Kind, reader client.Reader, writer client.Client, fieldManager string, rules RuleLookup) *Engine {
	return &Engine{kind: kind, reader: reader, writer: writer, fieldManager: fieldManager, rules: rules}
}

// Source reads the source named key from the API server.
func (e *Engine) Source(ctx context.Context, key types.NamespacedName) (*unstructured.Unstructured, error) {
	source := e.object(key)
	if err := e.reader.Get(ctx, key, source); err != nil {
		return nil, err
	}
	return source, nil
}

// Converge brings the copy c describes to what it should hold, when the
// target is c.Rule's to write, writing it only when it differs. A write
// conflict is returned as the API server's Conflict error.
func (e *Engine) Converge(ctx context.Context, c Copy) (Result, error) {
	desired := e.desired(c)

	key := types.NamespacedName{Namespace: c.Namespace, Name: c.Name}
	live := e.object(key)
	err := e.reader.Get(ctx, key, live)
	switch {
	case apierrors.IsNotFound(err):
		return e.write(ctx, desired, Created)
	case err != nil:
		return Result{}, err
	case live.GetDeletionTimestamp() != nil:
		return Result{Outcome: TargetDeleting}, nil
	}

	result, mine, err := e.claim(ctx, c, live)
	if err != nil || !mine {
		return result, err
	}
	written := Updated
	if result.Outcome == Adopted {
		written = Adopted
	}

	fixedFieldsDiffer := !sameFields(live, desired, e.kind.FixedFields)
	if e.immutable(live) && (fixedFieldsDiffer || !sameKeyedFields(live, desired, e.kind.KeyedFields)) {
		return Result{}, fmt.Errorf("%w, and differs from its source", ErrImmutable)
	}
	if fixedFieldsDiffer {
		if err := e.delete(ctx, live); err != nil {
			return Result{}, err
		}
		return e.write(ctx, desired, Recreated)
	}

	applied, err := e.kind.Extract(live, e.fieldManager)
	if err != nil {
		return Result{}, err
	}
	if !sameContent(desired.Object, applied) {
		return e.write(ctx, desired, written)
	}
	removed, err := e.removeStrayKeys(ctx, live, desired)
	if err != nil {
		return Result{}, err
	}
	if removed {
		return Result{Outcome: written}, nil
	}
	return Result{Outcome: InSync}, nil
}

// desired is the configuration the engine applies for c: the copy's name,
// labels, annotations and owner reference, and the payload the source's
// kind copies, its keyed fields limited to c.Keys when c lists any.
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
	annotations := maps.Clone(c.Annotations)
	if annotations == nil {
		annotations = map[string]string{}
	}
	annotations[secretsv1beta1.CopierRuleAnnotation] = c.Rule
	annotations[secretsv1beta1.SecretNameAnnotation] = sourceName(c.Source)
	desired.SetAnnotations(annotations)
	if c.Owner != nil {
		controller, blockOwnerDeletion := true, true
		desired.SetOwnerReferences([]metav1.OwnerReference{{
			APIVersion:         c.Owner.APIVersion,
			Kind:               c.Owner.Kind,
			Name:               c.Owner.Name,
			UID:                c.Owner.UID,
			Controller:         &controller,
			BlockOwnerDeletion: &blockOwnerDeletion,
		}})
	}
	for _, field := range e.kind.WholeFields {
		if value, ok := c.Source.Object[field]; ok {
			desired.Object[field] = runtime.DeepCopyJSONValue(value)
		}
	}
	for _, field := range e.kind.KeyedFields {
		entries, _ := c.Source.Object[field].(map[string]any)
		if len(c.Keys) > 0 {
			entries = selectKeys(entries, c.Keys)
		}
		if len(entries) > 0 {
			desired.Object[field] = runtime.DeepCopyJSONValue(entries)
		}
	}
	return desired
}

// selectKeys returns the entries of entries whose keys are in keys.
func selectKeys(entries map[string]any, keys []string) map[string]any {
	selected := map[string]any{}
	for _, key := range keys {
		if value, ok := entries[key]; ok {
			selected[key] = value
		}
	}
	return selected
}

// write applies desired, removes the keys the API server's copy then holds
// beyond it, and reports outcome.
func (e *Engine) write(ctx context.Context, desired *unstructured.Unstructured, outcome Outcome) (Result, error) {
	current, err := e.apply(ctx, desired)
	if err != nil {
		return Result{}, err
	}
	if _, err := e.removeStrayKeys(ctx, current, desired); err != nil {
		return Result{}, err
	}
	return Result{Outcome: outcome}, nil
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

// delete deletes live, on condition that it is still the version the
// engine read. A target gone already is no failure.
func (e *Engine) delete(ctx context.Context, live *unstructured.Unstructured) error {
	uid, resourceVersion := live.GetUID(), live.GetResourceVersion()
	err := e.writer.Delete(ctx, live, client.Preconditions{UID: &uid, ResourceVersion: &resourceVersion})
	return client.IgnoreNotFound(err)
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

// immutable reports whether obj is marked immutable.
func (e *Engine) immutable(obj *unstructured.Unstructured) bool {
	if e.kind.ImmutableField == "" {
		return false
	}
	immutable, _ := obj.Object[e.kind.ImmutableField].(bool)
	return immutable
}

// object returns an empty object of the engine's kind named key.
func (e *Engine) object(key types.NamespacedName) *unstructured.Unstructured {
	obj := &unstructured.Unstructured{}
	obj.SetGroupVersionKind(e.kind.GroupVersionKind)
	obj.SetNamespace(key.Namespace)
	obj.SetName(key.Name)
	return obj
}

// sourceName names source as the secret-name annotation does:
// <namespace>/<name>.
func sourceName(source *unstructured.Unstructured) string {
	return source.GetNamespace() + "/" + source.GetName()
}

// sameFields reports whether live holds the value desired gives each of
// fields that desired sets.
func sameFields(live, desired *unstructured.Unstructured, fields []string) bool {
	for _, field := range fields {
		if value, ok := desired.Object[field]; ok && !sameContent(value, live.Object[field]) {
			return false
		}
	}
	return true
}

// sameKeyedFields reports whether a and b hold the same entries in each of
// fields, an absent field holding none.
func sameKeyedFields(a, b *unstructured.Unstructured, fields []string) bool {
	for _, field := range fields {
		entriesA, _ := a.Object[field].(map[string]any)
		entriesB, _ := b.Object[field].(map[string]any)
		if len(entriesA) != len(entriesB) || (len(entriesA) > 0 && !sameContent(entriesA, entriesB)) {
			return false
		}
	}
	return true
}

// sameContent reports whether two unstructured values hold the same fields
// and values.
func sameContent(a, b any) bool {
	rawA, errA := json.Marshal(a)
	rawB, errB := json.Marshal(b)
	return errA == nil && errB == nil && bytes.Equal(rawA, rawB)
}
