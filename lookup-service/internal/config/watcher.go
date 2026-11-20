package config

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	toolscache "k8s.io/client-go/tools/cache"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	lookupv1beta1 "github.com/educates/educates-training-platform/lookup-service/api/v1beta1"
)

// ClientObserver is told when a client's configuration is created or
// updated.
type ClientObserver interface {
	ObserveClient(ctx context.Context, c Client) error
}

// ClusterObserver is told when a cluster's configuration, its ClusterConfig
// or its kubeconfig Secret, is created, updated or deleted.
type ClusterObserver interface {
	// ObserveCluster is told the cluster's configuration as it now is.
	ObserveCluster(ctx context.Context, c Cluster)
	// ForgetCluster is told that the cluster of that name is gone.
	ForgetCluster(ctx context.Context, name string)
}

// Watcher watches the configuration resources in the config namespace. It
// writes nothing to them: it adds no finalizers and sets no status.
type Watcher struct {
	model    *Model
	cache    cache.Cache
	clients  ClientObserver
	clusters ClusterObserver
}

// watchedKinds are the configuration resources whose caches must have
// synced before the service is ready.
var watchedKinds = []client.Object{
	&lookupv1beta1.ClientConfig{},
	&lookupv1beta1.ClusterConfig{},
	&lookupv1beta1.TenantConfig{},
	&corev1.Secret{},
}

// NewWatcher registers the watches of the configuration resources with mgr,
// whose cache must be scoped to the model's namespace. clients is told of
// every change to a ClientConfig, and clusters of every change to a
// ClusterConfig or to the kubeconfig Secret it names.
func NewWatcher(mgr manager.Manager, model *Model, clients ClientObserver, clusters ClusterObserver) (*Watcher, error) {
	w := &Watcher{model: model, cache: mgr.GetCache(), clients: clients, clusters: clusters}

	err := ctrl.NewControllerManagedBy(mgr).
		Named("clientconfig").
		For(&lookupv1beta1.ClientConfig{}).
		Complete(reconcile.Func(w.reconcileClient))
	if err != nil {
		return nil, fmt.Errorf("registering the ClientConfig watch: %w", err)
	}

	err = ctrl.NewControllerManagedBy(mgr).
		Named("clusterconfig").
		For(&lookupv1beta1.ClusterConfig{}).
		Watches(&corev1.Secret{}, handler.EnqueueRequestsFromMapFunc(w.clustersUsingSecret)).
		Complete(reconcile.Func(w.reconcileCluster))
	if err != nil {
		return nil, fmt.Errorf("registering the ClusterConfig watch: %w", err)
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

func (w *Watcher) reconcileCluster(ctx context.Context, req reconcile.Request) (reconcile.Result, error) {
	c, found, err := w.model.Cluster(ctx, req.Name)
	if err != nil {
		return reconcile.Result{}, err
	}
	if !found {
		w.clusters.ForgetCluster(ctx, req.Name)
		return reconcile.Result{}, nil
	}
	w.clusters.ObserveCluster(ctx, c)
	return reconcile.Result{}, nil
}

// clustersUsingSecret maps a Secret to the ClusterConfigs whose kubeconfig
// it holds.
func (w *Watcher) clustersUsingSecret(ctx context.Context, secret client.Object) []reconcile.Request {
	names, err := w.model.ClustersUsingSecret(ctx, secret.GetName())
	if err != nil {
		log.FromContext(ctx).Error(err, "Cannot find the clusters a Secret holds the kubeconfig of",
			"secret", secret.GetName())
		return nil
	}
	requests := make([]reconcile.Request, 0, len(names))
	for _, name := range names {
		requests = append(requests, reconcile.Request{
			NamespacedName: client.ObjectKey{Namespace: w.model.namespace, Name: name},
		})
	}
	return requests
}
