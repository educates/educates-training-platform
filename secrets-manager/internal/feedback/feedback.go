// Package feedback tells workshop authors and platform operators whether
// each secrets.educates.dev resource is in sync, and why not: it writes the
// status each reconcile computes, and records Events when a resource stops
// or starts being Ready.
//
// A status is written as a JSON merge patch of the status subresource,
// conditional on the resource version the cache holds, and only when it
// differs from the status the cache holds or from the one last written, so
// that reconciling a resource whose status has not changed makes no API
// call. A patch from a cache that has not yet seen the latest version is
// refused by the API server as a conflict; the status is then compared
// with, and written against, the version the API server holds.
//
// Events go through the events.k8s.io API, on transitions only: a Warning
// NotReady once a resource has not been Ready for a grace period, a Normal
// Recovered when a resource this process recorded that Warning about becomes
// Ready again, and a Normal Imported when a SecretImporter imports. The
// grace period runs from the Ready condition's last transition, so it
// survives a restart or a change of leader: a resource the process finds
// not Ready past the grace period gets its Warning at once, whether or not
// the process before it recorded one. Besides, a Warning CopyNotUpdated
// says that a copy another writer made immutable could not take a change of
// its source, once for each change it did not take, and once more after a
// restart or a change of leader.
package feedback

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/events"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"
)

// Feedback writes the status of the secrets.educates.dev resources and
// records their Events. It is safe for concurrent use by the reconcilers of
// every kind.
type Feedback struct {
	writer   client.StatusClient
	reader   client.Reader
	recorder events.EventRecorder
	scheme   *runtime.Scheme
	grace    time.Duration

	mu sync.Mutex
	// stopped is set once the process stops leading, after which no Event
	// is recorded.
	stopped bool
	// written holds, for each resource, the status last written, as JSON.
	written map[resourceKey]string
	// readiness holds, for each resource that is not Ready, how long it has
	// not been and whether it got its Warning.
	readiness map[resourceKey]*readiness
	// notUpdated holds, for each copy that got a CopyNotUpdated Warning, the
	// change of its source the Warning was about.
	notUpdated map[types.NamespacedName]string
}

// resourceKey names a resource of one of the four kinds.
type resourceKey struct {
	kind string
	types.NamespacedName
}

// New returns a Feedback that writes status through writer, reads a
// resource the cache holds an older version of through reader, from the API
// server, and records Events with recorder, raising a Warning once a
// resource has not been Ready for grace. scheme maps the four kinds' Go
// types to their kinds.
func New(writer client.StatusClient, reader client.Reader, recorder events.EventRecorder, scheme *runtime.Scheme,
	grace time.Duration) *Feedback {
	return &Feedback{
		writer:     writer,
		reader:     reader,
		recorder:   recorder,
		scheme:     scheme,
		grace:      grace,
		written:    map[resourceKey]string{},
		readiness:  map[resourceKey]*readiness{},
		notUpdated: map[types.NamespacedName]string{},
	}
}

// Start waits until ctx is done and stops every pending Warning, so that a
// process that no longer leads records no Event. It makes Feedback a
// runnable of the manager, started once the process leads.
func (f *Feedback) Start(ctx context.Context) error {
	<-ctx.Done()
	f.mu.Lock()
	defer f.mu.Unlock()
	f.stopped = true
	for key, state := range f.readiness {
		state.stop()
		delete(f.readiness, key)
	}
	return nil
}

// Publish writes the status a reconcile computed for cached, the resource as
// the cache holds it, unless the resource already holds that status, and
// records the Events the change of its Ready condition calls for. set gives
// a copy of the resource the computed status, its Ready condition set on
// the copy's own conditions. When the cache had not seen the resource's
// latest version, Publish compares the status with the API server's
// version and writes it against that version, once. A conflict that
// remains is returned as the API server's Conflict error: the reconcile is
// to be retried. A resource deleted meanwhile is forgotten.
func Publish[T client.Object](ctx context.Context, f *Feedback, cached T, set func(T)) error {
	gvk, err := apiutil.GVKForObject(cached, f.scheme)
	if err != nil {
		return err
	}
	key := resourceKey{kind: gvk.Kind, NamespacedName: client.ObjectKeyFromObject(cached)}
	f.mu.Lock()
	last, known := f.written[key]
	f.mu.Unlock()

	change, err := withStatus(cached, set, last, known)
	if err != nil {
		return err
	}
	if !change.inSync {
		err = f.patch(ctx, change.base, change.updated)
		if apierrors.IsConflict(err) {
			var fresh T
			if fresh, err = live(ctx, f, gvk, cached); err == nil {
				// The API server's version is the latest: what this process
				// wrote last no longer counts.
				change, err = withStatus(fresh, set, "", false)
			}
			if err == nil && !change.inSync {
				err = f.patch(ctx, change.base, change.updated)
			}
		}
		switch {
		case apierrors.IsNotFound(err):
			f.Forget(gvk.Kind, key.NamespacedName)
			return nil
		case err != nil:
			return err
		}
	}

	f.mu.Lock()
	f.written[key] = change.status
	f.mu.Unlock()
	f.observe(key, gvk, change.updated, readyOf(change.base), readyOf(change.updated))
	return nil
}

// statusChange is what setting the status a reconcile computed changes on
// a resource.
type statusChange[T client.Object] struct {
	// base is the resource as it was read, and updated a copy of it holding
	// the computed status.
	base, updated T

	// status is the computed status, as JSON.
	status string

	// inSync reports whether base holds the computed status already.
	inSync bool
}

// withStatus returns the change set, giving a copy of base the computed
// status, makes to base. last is the status last written, as JSON, when
// known: when it differs from the computed status, base, as a cache holds
// it, may hold a status that is no longer the API server's.
func withStatus[T client.Object](base T, set func(T), last string, known bool) (statusChange[T], error) {
	change := statusChange[T]{base: base}
	updated, ok := base.DeepCopyObject().(T)
	if !ok {
		return change, fmt.Errorf("copying %s", base.GetName())
	}
	set(updated)
	change.updated = updated
	before, err := statusOf(base)
	if err != nil {
		return change, err
	}
	if change.status, err = statusOf(updated); err != nil {
		return change, err
	}
	change.inSync = before == change.status && (!known || last == change.status)
	return change, nil
}

// patch writes the status of updated as a merge patch from base,
// conditional on the resource version of base.
func (f *Feedback) patch(ctx context.Context, base, updated client.Object) error {
	return f.writer.Status().Patch(ctx, updated, client.MergeFromWithOptions(base, client.MergeFromWithOptimisticLock{}))
}

// live reads the resource cached names, of gvk, from the API server.
func live[T client.Object](ctx context.Context, f *Feedback, gvk schema.GroupVersionKind, cached T) (T, error) {
	var none T
	obj, err := f.scheme.New(gvk)
	if err != nil {
		return none, err
	}
	fresh, ok := obj.(T)
	if !ok {
		return none, fmt.Errorf("the scheme makes %T for %s", obj, gvk)
	}
	if err := f.reader.Get(ctx, client.ObjectKeyFromObject(cached), fresh); err != nil {
		return none, err
	}
	return fresh, nil
}

// Forget drops what Feedback holds of the resource of kind, such as
// SecretCopier, that key names, which no longer exists or is being deleted,
// and stops its pending Warning.
func (f *Feedback) Forget(kind string, key types.NamespacedName) {
	k := resourceKey{kind: kind, NamespacedName: key}
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.written, k)
	if state, ok := f.readiness[k]; ok {
		state.stop()
		delete(f.readiness, k)
	}
}

// statusOf renders the status of obj as JSON, with its fields in a stable
// order, or "" when it has none.
func statusOf(obj client.Object) (string, error) {
	content, err := runtime.DefaultUnstructuredConverter.ToUnstructured(obj)
	if err != nil {
		return "", fmt.Errorf("reading the status of %s: %w", obj.GetName(), err)
	}
	status, ok := content["status"]
	if !ok {
		return "", nil
	}
	raw, err := json.Marshal(status)
	if err != nil {
		return "", fmt.Errorf("reading the status of %s: %w", obj.GetName(), err)
	}
	return string(raw), nil
}
