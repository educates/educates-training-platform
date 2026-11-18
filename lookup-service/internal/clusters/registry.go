// Package clusters is the cluster registry: one controller-runtime cluster,
// with its own cache, per ClusterConfig, watching the cluster's training
// portals, workshop environments and workshop sessions, and feeding them to
// the inventory.
//
// A cluster's cache is started when its ClusterConfig appears, started
// again when the connection its ClusterConfig or kubeconfig Secret gives
// changes, and stopped when the ClusterConfig is deleted, which removes the
// cluster's portals from the inventory at once. A cache that has not synced
// within a bounded time is stopped and started again, and until it syncs
// its cluster has no portals; no cluster ever waits for another, and
// neither the REST API nor readiness waits for any.
package clusters

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/selection"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	toolscache "k8s.io/client-go/tools/cache"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/cluster"

	"github.com/educates/educates-training-platform/lookup-service/internal/config"
	"github.com/educates/educates-training-platform/lookup-service/internal/inventory"
	"github.com/educates/educates-training-platform/lookup-service/internal/kubeconfig"
)

// The defaults of Options.
const (
	DefaultLocalTokenDirectory = "/opt/cluster-access-token"
	DefaultLocalServer         = "https://kubernetes.default.svc"
	DefaultSyncTimeout         = 30 * time.Second
	DefaultRetryInterval       = 10 * time.Second
)

// Options configure the cluster registry.
type Options struct {
	// LocalTokenDirectory is where the remote-access token the local
	// cluster is reached with is mounted, as the files token and ca.crt.
	LocalTokenDirectory string
	// LocalServer is the URL of the local cluster's API server.
	LocalServer string
	// SyncTimeout bounds the wait for a cluster's cache to sync before the
	// cache is started again.
	SyncTimeout time.Duration
	// RetryInterval is the wait before a cache is started again, and how
	// often the local cluster's mounted token is read again.
	RetryInterval time.Duration
}

func (o Options) withDefaults() Options {
	if o.LocalTokenDirectory == "" {
		o.LocalTokenDirectory = DefaultLocalTokenDirectory
	}
	if o.LocalServer == "" {
		o.LocalServer = DefaultLocalServer
	}
	if o.SyncTimeout <= 0 {
		o.SyncTimeout = DefaultSyncTimeout
	}
	if o.RetryInterval <= 0 {
		o.RetryInterval = DefaultRetryInterval
	}
	return o
}

// The training resources each cluster's cache watches.
var (
	trainingPortalKind      = schema.GroupVersionKind{Group: "training.educates.dev", Version: "v1beta1", Kind: "TrainingPortal"}
	workshopEnvironmentKind = schema.GroupVersionKind{Group: "training.educates.dev", Version: "v1beta1", Kind: "WorkshopEnvironment"}
	workshopSessionKind     = schema.GroupVersionKind{Group: "training.educates.dev", Version: "v1beta1", Kind: "WorkshopSession"}
)

// Registry runs a cache for every cluster the configuration names. It is a
// manager runnable: caches start once the manager runs it, and all stop
// when the manager stops.
type Registry struct {
	inventory *inventory.Inventory
	opts      Options
	log       logr.Logger
	scheme    *runtime.Scheme

	mu sync.Mutex
	// ctx is the context caches run in, set once the registry is started.
	ctx     context.Context
	members map[string]*member
	running sync.WaitGroup
}

// member is a cluster the configuration names, with the supervisor of its
// cache.
type member struct {
	cluster config.Cluster
	info    inventory.ClusterInfo
	// reader reads the cluster's API server directly, nil while no cache of
	// the cluster has synced.
	reader client.Reader
	// cancel stops the supervisor, nil until it is started.
	cancel context.CancelFunc
}

// NewRegistry returns a registry that feeds inv.
func NewRegistry(inv *inventory.Inventory, opts Options, log logr.Logger) (*Registry, error) {
	scheme := runtime.NewScheme()
	if err := clientgoscheme.AddToScheme(scheme); err != nil {
		return nil, err
	}
	return &Registry{
		inventory: inv,
		opts:      opts.withDefaults(),
		log:       log,
		scheme:    scheme,
		members:   map[string]*member{},
	}, nil
}

// Start starts the caches of the clusters observed so far, and of those
// observed later, and stops them all once ctx is done.
func (r *Registry) Start(ctx context.Context) error {
	r.mu.Lock()
	r.ctx = ctx
	for _, m := range r.members {
		r.launch(m)
	}
	r.mu.Unlock()

	<-ctx.Done()
	r.running.Wait()
	return nil
}

// ObserveCluster starts the cluster's cache, or starts it again when its
// connection changed. A change to its labels alone only changes how tenants
// match it.
func (r *Registry) ObserveCluster(_ context.Context, c config.Cluster) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if m, ok := r.members[c.Name]; ok {
		if sameConnection(m.cluster, c) {
			m.cluster = c
			m.info.Labels = c.Labels
			r.inventory.SetCluster(c.Name, m.info)
			return
		}
		r.log.Info("The cluster's configuration changed: connecting again", "cluster", c.Name)
		r.stop(m)
		r.inventory.Disconnect(c.Name)
	}

	m := &member{
		cluster: c,
		info:    inventory.ClusterInfo{Labels: c.Labels, Local: c.Local(), Problem: "not connected yet"},
	}
	r.members[c.Name] = m
	r.inventory.SetCluster(c.Name, m.info)
	if r.ctx != nil {
		r.launch(m)
	}
}

// ForgetCluster stops the cluster's cache, and removes the cluster and its
// portals from the inventory at once.
func (r *Registry) ForgetCluster(_ context.Context, name string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	m, ok := r.members[name]
	if !ok {
		return
	}
	r.log.Info("The cluster's ClusterConfig is gone: disconnecting", "cluster", name)
	r.stop(m)
	delete(r.members, name)
	r.inventory.RemoveCluster(name)
}

// APIReader returns a reader of the cluster's API server that goes past its
// cache, for the objects the cache does not watch, such as a portal's robot
// credentials Secret. It reads as the identity the cluster is reached as.
// There is none until the cluster's cache has synced.
func (r *Registry) APIReader(name string) (client.Reader, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	m, ok := r.members[name]
	if !ok || m.reader == nil {
		return nil, false
	}
	return m.reader, true
}

// sameConnection reports whether two observations of a cluster give the
// same connection, so that its cache can keep running.
func sameConnection(a, b config.Cluster) bool {
	if a.UID != b.UID || a.Local() != b.Local() {
		return false
	}
	if a.Local() {
		return true
	}
	return a.Kubeconfig.Context == b.Kubeconfig.Context &&
		a.Kubeconfig.Problem == b.Kubeconfig.Problem &&
		bytes.Equal(a.Kubeconfig.Data, b.Kubeconfig.Data)
}

// launch starts the member's supervisor, unless the registry is stopping.
// It is called with the lock held.
func (r *Registry) launch(m *member) {
	if r.ctx.Err() != nil {
		return
	}
	ctx, cancel := context.WithCancel(r.ctx)
	m.cancel = cancel
	r.running.Go(func() { r.supervise(ctx, m) })
}

// stop stops the member's supervisor. It is called with the lock held.
func (r *Registry) stop(m *member) {
	if m.cancel != nil {
		m.cancel()
	}
}

// current runs update with the lock held if m is still the member of its
// cluster, reporting whether it was.
func (r *Registry) current(m *member, update func()) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.members[m.cluster.Name] != m {
		return false
	}
	update()
	return true
}

// setConnection records the kubeconfig the member is reached through, or
// why there is none.
func (r *Registry) setConnection(m *member, connection *kubeconfig.Config, problem string) {
	r.current(m, func() {
		m.info.Kubeconfig = connection
		m.info.Problem = problem
		r.inventory.SetCluster(m.cluster.Name, m.info)
		if connection == nil {
			m.reader = nil
			r.inventory.Disconnect(m.cluster.Name)
		}
	})
}

// supervise keeps the member's cache running until ctx is done: it
// connects, runs the cache, and connects again when the cache stops.
func (r *Registry) supervise(ctx context.Context, m *member) {
	log := r.log.WithValues("cluster", m.cluster.Name)
	ctx = logr.NewContext(ctx, log)
	problem := ""
	for ctx.Err() == nil {
		connection, err := r.kubeconfig(m.cluster)
		if err != nil {
			if err.Error() != problem {
				problem = err.Error()
				log.Error(err, "The cluster is not connected")
			}
			r.setConnection(m, nil, problem)
			if !m.cluster.Local() {
				// A remote cluster's kubeconfig changes only with its
				// ClusterConfig or Secret, which starts another supervisor.
				<-ctx.Done()
				return
			}
			sleep(ctx, r.opts.RetryInterval)
			continue
		}
		problem = ""
		r.setConnection(m, connection, "")
		if !r.run(ctx, m, connection) {
			sleep(ctx, r.opts.RetryInterval)
		}
	}
}

// kubeconfig returns the kubeconfig the cluster is reached through: for the
// local cluster, made from the mounted remote-access token, read again on
// every call; for a remote cluster, the selected context of its Secret's
// kubeconfig.
func (r *Registry) kubeconfig(c config.Cluster) (*kubeconfig.Config, error) {
	if c.Local() {
		token, err := readMounted(r.opts.LocalTokenDirectory, "token")
		if err != nil {
			return nil, err
		}
		ca, err := readMounted(r.opts.LocalTokenDirectory, "ca.crt")
		if err != nil {
			return nil, err
		}
		return kubeconfig.Local(c.Name, r.opts.LocalServer, token, ca), nil
	}
	if c.Kubeconfig.Problem != "" {
		return nil, errors.New(c.Kubeconfig.Problem)
	}
	selected, err := kubeconfig.Select(c.Kubeconfig.Data, c.Kubeconfig.Context)
	if err != nil {
		return nil, fmt.Errorf("the kubeconfig in Secret %s under key %s is refused: %w",
			c.Kubeconfig.SecretName, c.Kubeconfig.Key, err)
	}
	return selected, nil
}

// readMounted reads a file of the remote-access token mount, which is empty
// while the token's Secret does not exist.
func readMounted(directory, name string) ([]byte, error) {
	data, err := os.ReadFile(filepath.Join(directory, name))
	if errors.Is(err, os.ErrNotExist) || err == nil && len(bytes.TrimSpace(data)) == 0 {
		return nil, fmt.Errorf("the remote-access token is not mounted: %s has no %s", directory, name)
	}
	return data, err
}

// run runs one cache of the member's cluster until ctx is done, the cache
// does not sync in time or stops, or, for the local cluster, the mounted
// token changes. It reports whether to connect again at once, as for a new
// token, rather than after the retry interval.
func (r *Registry) run(ctx context.Context, m *member, connection *kubeconfig.Config) bool {
	log := logr.FromContextOrDiscard(ctx)
	restConfig, err := connection.RESTConfig()
	if err != nil {
		log.Error(err, "The cluster is not connected")
		return false
	}
	c, err := r.newCluster(restConfig, log)
	if err != nil {
		log.Error(err, "Cannot create the cluster's cache")
		return false
	}

	var feed *inventory.Feed
	if !r.current(m, func() { feed = r.inventory.Attach(m.cluster.Name) }) {
		return false
	}
	defer feed.Detach()

	synced, err := watchTraining(ctx, c.GetCache(), feed)
	if err != nil {
		log.Error(err, "Cannot watch the cluster's training resources")
		return false
	}

	cacheCtx, stopCache := context.WithCancel(ctx)
	stopped := make(chan error, 1)
	go func() { stopped <- c.Start(cacheCtx) }()
	defer func() {
		stopCache()
		<-stopped
		r.current(m, func() { m.reader = nil })
	}()

	syncCtx, cancelSync := context.WithTimeout(cacheCtx, r.opts.SyncTimeout)
	ok := toolscache.WaitForCacheSync(syncCtx.Done(), synced...)
	cancelSync()
	if !ok {
		if ctx.Err() == nil {
			log.Info("The cluster's cache has not synced in time: it contributes no portals until it does, and is started again",
				"timeout", r.opts.SyncTimeout.String())
		}
		return false
	}
	feed.MarkSynced()
	r.current(m, func() { m.reader = c.GetAPIReader() })
	log.Info("The cluster's cache has synced", "server", restConfig.Host)

	recheck := make(<-chan time.Time)
	if m.cluster.Local() {
		ticker := time.NewTicker(r.opts.RetryInterval)
		defer ticker.Stop()
		recheck = ticker.C
	}
	for {
		select {
		case <-ctx.Done():
			return false
		case err := <-stopped:
			stopped <- err
			log.Error(err, "The cluster's cache stopped")
			return false
		case <-recheck:
			latest, err := r.kubeconfig(m.cluster)
			if err != nil || !latest.Equal(connection) {
				log.Info("The mounted remote-access token changed: connecting again")
				return true
			}
		}
	}
}

// newCluster creates a controller-runtime cluster that reaches the API
// server with restConfig. Its cache watches only the training resources,
// read as unstructured objects; a mapper that knows them, and Secrets, saves
// the cluster from discovering the API server's resources.
func (r *Registry) newCluster(restConfig *rest.Config, log logr.Logger) (cluster.Cluster, error) {
	restConfig = rest.CopyConfig(restConfig)
	restConfig.UserAgent = "educates-lookup-service"
	return cluster.New(restConfig, func(o *cluster.Options) {
		o.Scheme = r.scheme
		o.Logger = log
		o.MapperProvider = func(*rest.Config, *http.Client) (meta.RESTMapper, error) {
			mapper := meta.NewDefaultRESTMapper(nil)
			for _, kind := range []schema.GroupVersionKind{trainingPortalKind, workshopEnvironmentKind, workshopSessionKind} {
				mapper.Add(kind, meta.RESTScopeRoot)
			}
			mapper.Add(corev1.SchemeGroupVersion.WithKind("Secret"), meta.RESTScopeNamespace)
			return mapper, nil
		}
		o.Cache = cache.Options{
			ByObject: map[client.Object]cache.ByObject{
				trainingObject(workshopEnvironmentKind): {Label: labelsPresent(inventory.PortalNameLabel)},
				trainingObject(workshopSessionKind): {
					Label: labelsPresent(inventory.PortalNameLabel, inventory.EnvironmentNameLabel),
				},
			},
			DefaultTransform:         cache.TransformStripManagedFields(),
			DefaultWatchErrorHandler: watchErrorHandler(log),
		}
	})
}

// watchErrorHandler logs why listing or watching a cluster's training
// resources failed, which the reflector then retries.
func watchErrorHandler(log logr.Logger) toolscache.WatchErrorHandlerWithContext {
	return func(_ context.Context, _ *toolscache.Reflector, err error) {
		switch {
		case errors.Is(err, io.EOF):
		case apierrors.IsResourceExpired(err) || apierrors.IsGone(err) || errors.Is(err, io.ErrUnexpectedEOF):
			log.V(1).Info("The watch of the cluster's training resources closed", "reason", err.Error())
		default:
			log.Error(err, "Cannot list or watch the cluster's training resources")
		}
	}
}

func trainingObject(kind schema.GroupVersionKind) *unstructured.Unstructured {
	u := &unstructured.Unstructured{}
	u.SetGroupVersionKind(kind)
	return u
}

// labelsPresent selects objects that carry every one of the labels.
func labelsPresent(keys ...string) labels.Selector {
	selector := labels.NewSelector()
	for _, key := range keys {
		requirement, err := labels.NewRequirement(key, selection.Exists, nil)
		if err != nil {
			panic(err)
		}
		selector = selector.Add(*requirement)
	}
	return selector
}

// watchTraining registers the cache's informers of the training resources,
// delivering what they see to feed, and returns the checks that report
// when feed has received what each held when the cache started.
func watchTraining(ctx context.Context, c cache.Cache, feed *inventory.Feed) ([]toolscache.InformerSynced, error) {
	handlers := []struct {
		kind   schema.GroupVersionKind
		set    func(*unstructured.Unstructured)
		delete func(string)
	}{
		{trainingPortalKind, feed.SetPortal, feed.DeletePortal},
		{workshopEnvironmentKind, feed.SetEnvironment, feed.DeleteEnvironment},
		{workshopSessionKind, feed.SetSession, feed.DeleteSession},
	}
	var synced []toolscache.InformerSynced
	for _, h := range handlers {
		informer, err := c.GetInformer(ctx, trainingObject(h.kind), cache.BlockUntilSynced(false))
		if err != nil {
			return nil, fmt.Errorf("watching %s: %w", h.kind.Kind, err)
		}
		registration, err := informer.AddEventHandler(toolscache.ResourceEventHandlerFuncs{
			AddFunc: func(obj any) {
				if u, ok := obj.(*unstructured.Unstructured); ok {
					h.set(u)
				}
			},
			UpdateFunc: func(_, obj any) {
				if u, ok := obj.(*unstructured.Unstructured); ok {
					h.set(u)
				}
			},
			DeleteFunc: func(obj any) {
				if tombstone, ok := obj.(toolscache.DeletedFinalStateUnknown); ok {
					obj = tombstone.Obj
				}
				if u, ok := obj.(*unstructured.Unstructured); ok {
					h.delete(u.GetName())
				}
			},
		})
		if err != nil {
			return nil, fmt.Errorf("watching %s: %w", h.kind.Kind, err)
		}
		synced = append(synced, registration.HasSynced)
	}
	return synced, nil
}

// sleep waits for d, or until ctx is done.
func sleep(ctx context.Context, d time.Duration) {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
	case <-timer.C:
	}
}
