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
// data, is replaced whole: a key outside the source's is removed with a
// JSON merge patch, conditional on the resource version the engine last
// saw.
//
// A copy whose fixed field, such as a Secret's type, differs from the
// source's is deleted and written anew. A copy another writer made
// immutable, whose payload must change, is a failure; it is never deleted.
//
// A copy carries a third tracking annotation, source-version: the resource
// version of the source it was last written from. The engine remembers,
// for each copy it finds in sync or writes, the resource versions of the
// source and of the copy, and the shape the rule gives the copy. While the
// cache holds those versions and the rule asks for that shape, the copy is
// in sync, and converging it reads and writes nothing on the API server.
// Otherwise the engine reads the source and the copy from the API server
// and compares them, writing the copy only for a real difference in its
// payload, labels, annotations or owner reference: a source change that
// alters none of them is compared once and remembered, and does not
// rewrite the copy to refresh source-version. The memory lives as long as
// the process, so after a restart each copy is compared once more.
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

// Copy is one copy of a source for the engine to converge. Every field but
// Source makes up the shape of the copy, which the engine remembers with
// the copy: a change to any of them has the engine compare the copy again.
type Copy struct {
	// Source is the object copied, as the engine's Source returns it.
	Source *Source

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
	cache        client.Reader
	reader       client.Reader
	writer       client.Client
	fieldManager string
	rules        RuleLookup
	memory       *memory
}

// New returns an engine copying objects of kind. It reads the metadata of
// sources and targets through cache, which must serve objects of kind as
// metadata, reads their content through reader, which must read from the
// API server rather than a cache, writes copies through writer, applying
// them as fieldManager, and asks rules whether the resource a target's
// copier-rule annotation names still exists.
func New(kind *Kind, cache, reader client.Reader, writer client.Client, fieldManager string,
	rules RuleLookup) *Engine {
	return &Engine{kind: kind, cache: cache, reader: reader, writer: writer, fieldManager: fieldManager,
		rules: rules, memory: newMemory()}
}

// Forget drops what the engine remembers of the copy named key, which no
// longer exists.
func (e *Engine) Forget(key types.NamespacedName) {
	e.memory.forget(key)
}

// Converge brings the copy c describes to what it should hold, when the
// target is c.Rule's to write, writing it only when it differs. A write
// conflict is returned as the API server's Conflict error.
//
// It decides from the cache, reading nothing from the API server, that a
// target is not c.Rule's to write, that it is being deleted, or that it is
// in sync, as the engine last found or wrote it.
func (e *Engine) Converge(ctx context.Context, c Copy) (Result, error) {
	key := types.NamespacedName{Namespace: c.Namespace, Name: c.Name}
	copyShape, err := shapeOf(c)
	if err != nil {
		return Result{}, err
	}

	cached := &metav1.PartialObjectMetadata{}
	cached.SetGroupVersionKind(e.kind.GroupVersionKind)
	err = e.cache.Get(ctx, key, cached)
	switch {
	case apierrors.IsNotFound(err):
	case err != nil:
		return Result{}, err
	case cached.GetDeletionTimestamp() != nil:
		return Result{Outcome: TargetDeleting}, nil
	default:
		result, mine, err := e.claim(ctx, c, cached)
		if err != nil || !mine {
			return result, err
		}
		if result.Outcome != Adopted && e.memory.holds(key, e.remembered(c, cached, copyShape)) {
			return Result{Outcome: InSync}, nil
		}
	}

	result, current, err := e.converge(ctx, c, key)
	if err != nil || current == nil {
		e.memory.forget(key)
		return result, err
	}
	e.memory.remember(key, e.remembered(c, current, copyShape))
	return result, nil
}

// remembered is what the engine remembers of c's copy, when it is target,
// with the shape c gives it.
func (e *Engine) remembered(c Copy, target metav1.Object, copyShape shape) remembered {
	return remembered{
		source:        types.NamespacedName{Namespace: c.Source.GetNamespace(), Name: c.Source.GetName()},
		sourceVersion: c.Source.GetResourceVersion(),
		copyVersion:   target.GetResourceVersion(),
		shape:         copyShape,
	}
}

// converge brings the copy c describes, named key, to what it should hold,
// comparing the source and the target as the API server has them. When
// the target is then c.Rule's copy, in sync, it returns the copy as the
// API server holds it.
func (e *Engine) converge(ctx context.Context, c Copy, key types.NamespacedName) (Result, metav1.Object, error) {
	if err := e.read(ctx, c.Source); err != nil {
		return Result{}, nil, fmt.Errorf("reading the source %s: %w", sourceName(c.Source), err)
	}
	desired := e.desired(c)

	live := e.object(key)
	err := e.reader.Get(ctx, key, live)
	switch {
	case apierrors.IsNotFound(err):
		return e.write(ctx, desired, Created)
	case err != nil:
		return Result{}, nil, err
	case live.GetDeletionTimestamp() != nil:
		return Result{Outcome: TargetDeleting}, nil, nil
	}

	result, mine, err := e.claim(ctx, c, live)
	if err != nil || !mine {
		return result, nil, err
	}
	written := Updated
	if result.Outcome == Adopted {
		written = Adopted
	}

	fixedFieldsDiffer := !sameFields(live, desired, e.kind.FixedFields)
	if e.immutable(live) && (fixedFieldsDiffer || !sameKeyedFields(live, desired, e.kind.KeyedFields)) {
		return Result{}, nil, fmt.Errorf("%w, and differs from its source", ErrImmutable)
	}
	if fixedFieldsDiffer {
		if err := e.delete(ctx, live); err != nil {
			return Result{}, nil, err
		}
		return e.write(ctx, desired, Recreated)
	}

	applied, err := e.kind.Extract(live, e.fieldManager)
	if err != nil {
		return Result{}, nil, err
	}
	if !sameContent(withoutSourceVersion(desired.Object), withoutSourceVersion(applied)) {
		return e.write(ctx, desired, written)
	}
	patched, err := e.removeStrayKeys(ctx, live, desired)
	switch {
	case err != nil:
		return Result{}, nil, err
	case patched != nil:
		return Result{Outcome: written}, patched, nil
	default:
		return Result{Outcome: InSync}, live, nil
	}
}

// desired is the configuration the engine applies for c: the copy's name,
// labels, annotations and owner reference, and the payload the source's
// kind copies. c.Source must hold the source read from the API server.
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
	annotations[secretsv1beta1.SourceVersionAnnotation] = c.Source.GetResourceVersion()
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
		if entries, ok := c.Source.Object[field].(map[string]any); ok && len(entries) > 0 {
			desired.Object[field] = runtime.DeepCopyJSONValue(entries)
		}
	}
	return desired
}

// write applies desired, removes the keys the API server's copy then holds
// beyond it, and reports outcome, with the copy the API server holds after
// the writes.
func (e *Engine) write(ctx context.Context, desired *unstructured.Unstructured,
	outcome Outcome) (Result, metav1.Object, error) {
	current, err := e.apply(ctx, desired)
	if err != nil {
		return Result{}, nil, err
	}
	patched, err := e.removeStrayKeys(ctx, current, desired)
	switch {
	case err != nil:
		return Result{}, nil, err
	case patched != nil:
		return Result{Outcome: outcome}, patched, nil
	default:
		return Result{Outcome: outcome}, current, nil
	}
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
// desired does not hold, and returns the copy the API server holds after
// the removal, or nil when it wrote nothing. The patch carries current's
// resource version, so it fails with a conflict when the copy has changed
// since.
func (e *Engine) removeStrayKeys(ctx context.Context,
	current, desired *unstructured.Unstructured) (*unstructured.Unstructured, error) {
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
		return nil, nil
	}
	patch["metadata"] = map[string]any{"resourceVersion": current.GetResourceVersion()}
	body, err := json.Marshal(patch)
	if err != nil {
		return nil, err
	}
	target := e.object(types.NamespacedName{Namespace: current.GetNamespace(), Name: current.GetName()})
	if err := e.writer.Patch(ctx, target, client.RawPatch(types.MergePatchType, body),
		client.FieldOwner(e.fieldManager)); err != nil {
		return nil, err
	}
	return target, nil
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
func sourceName(source metav1.Object) string {
	return source.GetNamespace() + "/" + source.GetName()
}

// withoutSourceVersion returns content, an object's unstructured content,
// without the source-version annotation, which records when a copy was
// written rather than what it holds. content is left as it is.
func withoutSourceVersion(content map[string]any) map[string]any {
	metadata, _ := content["metadata"].(map[string]any)
	annotations, _ := metadata["annotations"].(map[string]any)
	if _, ok := annotations[secretsv1beta1.SourceVersionAnnotation]; !ok {
		return content
	}
	annotations = maps.Clone(annotations)
	delete(annotations, secretsv1beta1.SourceVersionAnnotation)
	metadata = maps.Clone(metadata)
	if len(annotations) == 0 {
		delete(metadata, "annotations")
	} else {
		metadata["annotations"] = annotations
	}
	result := maps.Clone(content)
	result["metadata"] = metadata
	return result
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
