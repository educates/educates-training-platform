// Package feedback tells workshop authors and platform operators whether
// each secrets.educates.dev resource is in sync, and why not: it writes the
// status each reconcile computes, and records Events when a resource stops
// or starts being Ready.
//
// A status is written as a JSON merge patch of the status subresource,
// conditional on the resource version the cache holds, and only when it
// differs from the status the cache holds or from the one last written, so
// that reconciling a resource whose status has not changed makes no API
// call. A status computed from a cache that has not yet seen the last write
// is refused by the API server as a conflict, which the reconciler retries.
//
// Events go through the events.k8s.io API, on transitions only: a Warning
// NotReady once a resource has not been Ready for a grace period, a Normal
// Recovered when a resource that got that Warning becomes Ready again, and a
// Normal Imported when a SecretImporter imports. The grace period runs from
// the Ready condition's last transition, so it survives a restart; a
// resource that was not Ready for the whole grace period before this
// process started is taken to have had its Warning already.
package feedback

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
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
	recorder events.EventRecorder
	scheme   *runtime.Scheme
	grace    time.Duration

	mu sync.Mutex
	// started is when the process started leading, or was composed, until
	// it leads.
	started time.Time
	// stopped is set once the process stops leading, after which no Event
	// is recorded.
	stopped bool
	// written holds, for each resource, the status last written, as JSON.
	written map[resourceKey]string
	// readiness holds, for each resource that is not Ready, how long it has
	// not been and whether it got its Warning.
	readiness map[resourceKey]*readiness
}

// resourceKey names a resource of one of the four kinds.
type resourceKey struct {
	kind string
	types.NamespacedName
}

// New returns a Feedback that writes status through writer and records
// Events with recorder, raising a Warning once a resource has not been
// Ready for grace. scheme maps the four kinds' Go types to their kinds.
func New(writer client.StatusClient, recorder events.EventRecorder, scheme *runtime.Scheme,
	grace time.Duration) *Feedback {
	return &Feedback{
		writer:    writer,
		recorder:  recorder,
		scheme:    scheme,
		grace:     grace,
		started:   time.Now(),
		written:   map[resourceKey]string{},
		readiness: map[resourceKey]*readiness{},
	}
}

// Start marks when the process started leading, then waits until ctx is
// done and stops every pending Warning, so that a process that no longer
// leads records no Event. It makes Feedback a runnable of the manager,
// started once the process leads.
func (f *Feedback) Start(ctx context.Context) error {
	f.mu.Lock()
	f.started = time.Now()
	f.mu.Unlock()

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

// Publish writes the status of updated, the resource cached as the cache
// holds it with the status a reconcile computed, unless the resource
// already holds that status, and records the Events the change of its Ready
// condition calls for. A conflict, returned as the API server's Conflict
// error, means the cache had not seen the resource's latest version: the
// reconcile is to be retried. A resource deleted meanwhile is forgotten.
func (f *Feedback) Publish(ctx context.Context, cached, updated client.Object) error {
	gvk, err := apiutil.GVKForObject(updated, f.scheme)
	if err != nil {
		return err
	}
	key := resourceKey{kind: gvk.Kind, NamespacedName: client.ObjectKeyFromObject(updated)}
	before, err := statusOf(cached)
	if err != nil {
		return err
	}
	after, err := statusOf(updated)
	if err != nil {
		return err
	}

	f.mu.Lock()
	last, known := f.written[key]
	f.mu.Unlock()
	if before != after || known && last != after {
		patch := client.MergeFromWithOptions(cached, client.MergeFromWithOptimisticLock{})
		err := f.writer.Status().Patch(ctx, updated, patch)
		switch {
		case apierrors.IsNotFound(err):
			f.Forget(gvk.Kind, key.NamespacedName)
			return nil
		case err != nil:
			return err
		}
		f.mu.Lock()
		f.written[key] = after
		f.mu.Unlock()
	}

	f.observe(key, gvk, updated, readyOf(cached), readyOf(updated))
	return nil
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
