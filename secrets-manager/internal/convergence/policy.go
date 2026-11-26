// Package convergence runs every reconciler of the secrets-manager under
// one policy of retries. A resource is reconciled on the events that
// concern it, and again a backstop interval after its last reconcile,
// though no event asks for it, so that a change whose event was missed or
// mapped to the wrong resource still converges. A backstop reconcile that
// repairs anything logs a warning, since an event should have done it. A
// resource whose reconcile fails is retried with the work queue's
// exponential backoff, capped.
//
// The backstop is each resource's own requeue, not the manager's sync
// period, which would replay every cached object at once.
package convergence

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/client-go/util/workqueue"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

// backoffBase is the backoff of a resource's first failed reconcile. Each
// further failure doubles it, up to the policy's cap.
const backoffBase = 5 * time.Millisecond

// Policy is how the reconcilers of the secrets-manager retry.
type Policy struct {
	// Backstop is how long after its last reconcile a resource that exists
	// is reconciled again, though no event asks for it.
	Backstop time.Duration

	// BackoffCap is the longest a resource whose reconcile failed waits
	// before it is reconciled again.
	BackoffCap time.Duration
}

// Complete builds the controller b describes, reconciling with r under the
// policy. The controller reconciles objects of kind, which reader, a
// cache, holds: once r has reconciled one without failing or asking to be
// requeued, it is reconciled again after the backstop interval, while it
// exists and is not being deleted. r records the targets it repairs with
// Repaired.
func (p Policy) Complete(b *builder.Builder, kind client.Object, reader client.Reader,
	r reconcile.Reconciler) error {
	return b.WithOptions(controller.Options{
		RateLimiter: workqueue.NewTypedItemExponentialFailureRateLimiter[reconcile.Request](backoffBase,
			p.BackoffCap),
	}).Complete(&reconciler{
		policy:   p,
		kind:     kind,
		reader:   reader,
		next:     r,
		backstop: map[reconcile.Request]time.Time{},
	})
}

// Repaired records that the reconcile running with ctx wrote a target that
// was not as it should be. It does nothing outside a reconcile the policy
// runs.
func Repaired(ctx context.Context) {
	if repairs, ok := ctx.Value(repairsKey{}).(*atomic.Int64); ok {
		repairs.Add(1)
	}
}

// repairsKey keys the count of a reconcile's repairs in its context.
type repairsKey struct{}

// reconciler runs a reconciler under a policy.
type reconciler struct {
	policy Policy
	kind   client.Object
	reader client.Reader
	next   reconcile.Reconciler

	mu sync.Mutex
	// backstop holds, for each resource whose backstop reconcile is
	// scheduled, when it is due.
	backstop map[reconcile.Request]time.Time
}

// Reconcile reconciles the resource req names with the reconciler the
// policy runs, and schedules its backstop reconcile.
func (r *reconciler) Reconcile(ctx context.Context, req reconcile.Request) (reconcile.Result, error) {
	backstop := r.takeBackstop(req)

	repairs := &atomic.Int64{}
	result, err := r.next.Reconcile(context.WithValue(ctx, repairsKey{}, repairs), req)
	if backstop && repairs.Load() > 0 {
		ctrl.LoggerFrom(ctx).Info("Warning: the backstop reconcile repaired targets that no event had "+
			"reconciled, so an event was missed or mapped to the wrong resource", "repairs", repairs.Load())
	}
	if err != nil || result.RequeueAfter > 0 {
		return result, err
	}

	exists, err := r.exists(ctx, req)
	if err != nil || !exists {
		return result, err
	}
	r.mu.Lock()
	r.backstop[req] = time.Now().Add(r.policy.Backstop)
	r.mu.Unlock()
	return reconcile.Result{RequeueAfter: r.policy.Backstop}, nil
}

// takeBackstop reports whether the reconcile of req starting now is its
// backstop reconcile, and drops its schedule. A resource reconciled on an
// event before its backstop was due is scheduled anew when that reconcile
// ends.
func (r *reconciler) takeBackstop(req reconcile.Request) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	due, scheduled := r.backstop[req]
	delete(r.backstop, req)
	return scheduled && !time.Now().Before(due)
}

// exists reports whether the resource req names exists, and is not being
// deleted.
func (r *reconciler) exists(ctx context.Context, req reconcile.Request) (bool, error) {
	obj, ok := r.kind.DeepCopyObject().(client.Object)
	if !ok {
		return false, nil
	}
	err := r.reader.Get(ctx, req.NamespacedName, obj)
	switch {
	case apierrors.IsNotFound(err):
		return false, nil
	case err != nil:
		return false, err
	default:
		return obj.GetDeletionTimestamp().IsZero(), nil
	}
}
