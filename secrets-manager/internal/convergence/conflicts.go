package convergence

import (
	"errors"
	"fmt"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

// conflictRetryDelay is how long a resource waits to be reconciled again
// after a write conflicted with another writer's, time for the cache to
// catch up with the other write.
const conflictRetryDelay = time.Second

// RetryConflict is the result of a reconcile whose write conflicted with
// another writer's. The conflict is no failure: the resource is reconciled
// again after a moment.
func RetryConflict() reconcile.Result {
	return reconcile.Result{RequeueAfter: conflictRetryDelay}
}

// AfterStatus returns the result of a reconcile that came to result and
// err and then wrote the resource's status, statusErr being why writing it
// failed, or nil. A status write that conflicted retries the reconcile, as
// a conflicting write does, unless the reconcile failed already and its
// backoff retries it. Any other failure to write the status is a failure
// of the reconcile.
func AfterStatus(result reconcile.Result, err, statusErr error) (reconcile.Result, error) {
	switch {
	case statusErr == nil:
		return result, err
	case apierrors.IsConflict(statusErr) && err == nil:
		return RetryConflict(), nil
	case apierrors.IsConflict(statusErr):
		return result, err
	default:
		return reconcile.Result{}, errors.Join(err, fmt.Errorf("writing the status: %w", statusErr))
	}
}
