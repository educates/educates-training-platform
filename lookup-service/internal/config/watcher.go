package config

import (
	"context"
	"fmt"

	toolscache "k8s.io/client-go/tools/cache"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	lookupv1beta1 "github.com/educates/educates-training-platform/lookup-service/api/v1beta1"
)

// ClientObserver is told when a client's configuration is created or
// updated.
type ClientObserver interface {
	ObserveClient(ctx context.Context, c Client) error
}

// Watcher watches the configuration resources in the config namespace. It
// writes nothing to them: it adds no finalizers and sets no status.
type Watcher struct {
	model   *Model
	cache   cache.Cache
	clients ClientObserver
}

// watchedKinds are the configuration resources whose caches must have
// synced before the service is ready.
var watchedKinds = []client.Object{
	&lookupv1beta1.ClientConfig{},
}

// NewWatcher registers the watches of the configuration resources with mgr,
// whose cache must be scoped to the model's namespace. clients is told of
// every change to a ClientConfig.
func NewWatcher(mgr manager.Manager, model *Model, clients ClientObserver) (*Watcher, error) {
	w := &Watcher{model: model, cache: mgr.GetCache(), clients: clients}

	err := ctrl.NewControllerManagedBy(mgr).
		Named("clientconfig").
		For(&lookupv1beta1.ClientConfig{}).
		Complete(reconcile.Func(w.reconcileClient))
	if err != nil {
		return nil, fmt.Errorf("registering the ClientConfig watch: %w", err)
	}
	return w, nil
}

// WaitForSync blocks until the caches of the configuration resources have
// synced, so that the model holds every resource the API server holds. It
// must be called once the manager's cache has started.
func (w *Watcher) WaitForSync(ctx context.Context) error {
	for _, obj := range watchedKinds {
		informer, err := w.cache.GetInformer(ctx, obj, cache.BlockUntilSynced(true))
		if err != nil {
			return fmt.Errorf("waiting for the %T cache: %w", obj, err)
		}
		if !toolscache.WaitForCacheSync(ctx.Done(), informer.HasSynced) {
			return fmt.Errorf("the %T cache did not sync: %w", obj, context.Cause(ctx))
		}
	}
	return nil
}

func (w *Watcher) reconcileClient(ctx context.Context, req reconcile.Request) (reconcile.Result, error) {
	c, found, err := w.model.Client(ctx, req.Name)
	if err != nil {
		return reconcile.Result{}, err
	}
	if !found {
		return reconcile.Result{}, nil
	}
	return reconcile.Result{}, w.clients.ObserveClient(ctx, c)
}
