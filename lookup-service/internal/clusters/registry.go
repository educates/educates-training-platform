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
// its cluster has no portals. A cluster whose cache has synced is probed
// from then on, and one whose API server fails every probe for a bounded
// time is given up on: its cache is stopped, so that it has no portals,
// and started again until it syncs. No cluster ever waits for another, the
// REST API waits for none, and readiness waits for the clusters only for a
// bounded time, and not at all for one whose configuration gives no
// connection.
//
// A cluster that is down says so at error level once for each thing that
// fails, and repeats it at debug level on every further attempt, with a
// reminder at error level now and then. Its cache syncing, which is logged
// at info level, ends that.
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
	DefaultReadyTimeout        = 10 * time.Second
	DefaultProbeInterval       = 10 * time.Second
	DefaultProbeTimeout        = 5 * time.Second
	DefaultUnreachableAfter    = 30 * time.Second
	DefaultDownReminder        = 10 * time.Minute
)

// syncPollInterval is how often WaitForSync looks at the clusters.
const syncPollInterval = 50 * time.Millisecond

// Options configure the cluster registry.
type Options struct {
	// LocalTokenDirectory is where the remote-access token the local
	// cluster is reached with is mounted, as the files token and ca.crt.
	LocalTokenDirectory string
	// LocalServer is the URL the local cluster's API server is reached at.
	LocalServer string
	// LocalKubeconfigServer is the server the local cluster's kubeconfig
	// names, as the REST API shows it: LocalServer unless set.
	LocalKubeconfigServer string
	// SyncTimeout bounds the wait for a cluster's cache to sync before the
	// cache is started again.
	SyncTimeout time.Duration
	// RetryInterval is the wait before a cache is started again, and how
	// often the local cluster's mounted token is read again.
	RetryInterval time.Duration
	// ReadyTimeout bounds how long WaitForSync waits for clusters' caches
	// to sync, so that a cluster that cannot be reached holds the service's
	// readiness back no longer. A cluster whose configuration gives no
	// connection is not waited for.
	ReadyTimeout time.Duration
	// ProbeInterval is how often the API server of a cluster whose cache
	// has synced is probed, and ProbeTimeout bounds each probe.
	ProbeInterval time.Duration
	ProbeTimeout  time.Duration
	// UnreachableAfter is how long a cluster's API server may fail every
	// probe before the cluster is given up on, and contributes no portals
	// until its cache has synced again.
	UnreachableAfter time.Duration
	// DownReminder is how often a cluster that stays down says so again at
	// error level. Between reminders, the attempts that fail the way an
	// earlier one did are logged at debug level.
	DownReminder time.Duration
}

func (o Options) withDefaults() Options {
	if o.LocalTokenDirectory == "" {
		o.LocalTokenDirectory = DefaultLocalTokenDirectory
	}
	if o.LocalServer == "" {
		o.LocalServer = DefaultLocalServer
	}
	if o.LocalKubeconfigServer == "" {
		o.LocalKubeconfigServer = o.LocalServer
	}
	if o.SyncTimeout <= 0 {
		o.SyncTimeout = DefaultSyncTimeout
	}
	if o.RetryInterval <= 0 {
		o.RetryInterval = DefaultRetryInterval
	}
	if o.ReadyTimeout <= 0 {
		o.ReadyTimeout = DefaultReadyTimeout
	}
	if o.ProbeInterval <= 0 {
		o.ProbeInterval = DefaultProbeInterval
	}
	if o.ProbeTimeout <= 0 {
		o.ProbeTimeout = DefaultProbeTimeout
	}
	if o.UnreachableAfter <= 0 {
		o.UnreachableAfter = DefaultUnreachableAfter
	}
	if o.DownReminder <= 0 {
		o.DownReminder = DefaultDownReminder
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
	// cluster is the cluster as first observed, which its supervisor reads
	// without the lock: it never changes, since an observation that changes
	// the connection makes a new member.
	cluster config.Cluster
	// info is what the inventory shows of the cluster, its labels as last
	// observed included.
	info inventory.ClusterInfo
	// reader reads the cluster's API server directly, nil while no cache of
	// the cluster has synced.
	reader client.Reader
	// noConnection reports that the cluster's configuration gives no
	// connection to try, for the reason info.Problem gives: the cluster's
	// cache cannot sync until the configuration changes or, for the local
	// cluster, until the remote-access token is mounted.
	noConnection bool
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
		m.noConnection = connection == nil
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
	// problem is why the cluster has no connection, as last logged, and
	// unreachable why its API server was given up on, until a cache of the
	// cluster syncs again.
	problem, unreachable := "", ""
	down := &outage{reminder: r.opts.DownReminder}
	for ctx.Err() == nil {
		conn, err := r.connect(m.cluster, log, down)
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
		r.setConnection(m, conn.kubeconfig, unreachable)
		result := r.run(ctx, m, conn, down)
		switch {
		case result.unreachable != "":
			unreachable = result.unreachable
		case result.synced:
			unreachable = ""
		}
		if !result.reconnectNow {
			sleep(ctx, r.opts.RetryInterval)
		}
	}
}

// outage sets the level of what a cluster logs while it is down, which is
// from the first failure reported until the cluster is back. Each message
// is logged at error level the first time, and at debug level from then on,
// except that once the reminder interval has passed since the message was
// last logged at error level, it is logged at error level again, saying
// since when the cluster has been down.
type outage struct {
	reminder time.Duration

	mu sync.Mutex
	// since is when the cluster went down, zero while it is not down.
	since time.Time
	// said is when each message logged since the cluster went down was last
	// logged at error level.
	said map[string]time.Time
}

// report logs a failure of the cluster, with the error that caused it, if
// any.
func (o *outage) report(log logr.Logger, err error, msg string, keysAndValues ...any) {
	now := time.Now()
	o.mu.Lock()
	if o.since.IsZero() {
		o.since, o.said = now, map[string]time.Time{}
	}
	last, repeated := o.said[msg]
	remind := repeated && now.Sub(last) >= o.reminder
	if !repeated || remind {
		o.said[msg] = now
	}
	since := o.since
	o.mu.Unlock()

	switch {
	case !repeated:
		log.Error(err, msg, keysAndValues...)
	case remind:
		log.Error(err, msg, append(keysAndValues, "downSince", since.UTC().Format(time.RFC3339))...)
	default:
		if err != nil {
			keysAndValues = append(keysAndValues, "reason", err.Error())
		}
		log.V(1).Info(msg, keysAndValues...)
	}
}

// over records that the cluster is back.
func (o *outage) over() {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.since = time.Time{}
}

// connection is what a cluster is reached through: its kubeconfig, the
// client configuration made from it, and the controller-runtime cluster
// whose cache watches it.
type connection struct {
	kubeconfig *kubeconfig.Config
	restConfig *rest.Config
	cluster    cluster.Cluster
}

// connect makes the connection the cluster's configuration gives, without
// calling its API server. It fails when the configuration gives no
// kubeconfig, or one that is refused or cannot be used, such as one whose
// CA data is no certificate.
func (r *Registry) connect(c config.Cluster, log logr.Logger, down *outage) (*connection, error) {
	selected, err := r.kubeconfig(c)
	if err != nil {
		return nil, err
	}
	conn := &connection{kubeconfig: selected}
	if conn.restConfig, err = selected.RESTConfig(); err == nil {
		if c.Local() {
			conn.restConfig.Host = r.opts.LocalServer
		}
		conn.cluster, err = r.newCluster(conn.restConfig, log, down)
	}
	switch {
	case err == nil:
		return conn, nil
	case c.Local():
		return nil, fmt.Errorf("the remote-access token mounted at %s cannot be used: %w",
			r.opts.LocalTokenDirectory, err)
	default:
		return nil, fmt.Errorf("the kubeconfig in Secret %s under key %s cannot be used: %w",
			c.Kubeconfig.SecretName, c.Kubeconfig.Key, err)
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
		return kubeconfig.Local(c.Name, r.opts.LocalKubeconfigServer, token, ca), nil
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

// outcome is how one cache of a cluster ended.
type outcome struct {
	// reconnectNow asks to connect again at once, as for a new token,
	// rather than after the retry interval.
	reconnectNow bool
	// synced reports that the cache synced before it ended.
	synced bool
	// unreachable says why the cluster's API server was given up on, empty
	// when the cache ended for another reason.
	unreachable string
}

// run runs one cache of the member's cluster until ctx is done, the cache
// does not sync in time or stops, the cluster's API server has failed every
// probe for too long, or, for the local cluster, the mounted token changes.
// While the cache has not synced, and once it has ended, the cluster has no
// portals. What fails is reported to down, which the cache syncing, and a
// probe that is answered, end.
func (r *Registry) run(ctx context.Context, m *member, conn *connection, down *outage) outcome {
	log := logr.FromContextOrDiscard(ctx)
	c := conn.cluster

	var feed *inventory.Feed
	if !r.current(m, func() { feed = r.inventory.Attach(m.cluster.Name) }) {
		return outcome{}
	}
	defer feed.Detach()

	synced, err := watchTraining(ctx, c.GetCache(), feed)
	if err != nil {
		down.report(log, err, "Cannot watch the cluster's training resources")
		return outcome{}
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
			down.report(log, nil, "The cluster's cache has not synced in time: it contributes no portals until it "+
				"does, and is started again", "timeout", r.opts.SyncTimeout.String())
		}
		return outcome{}
	}
	feed.MarkSynced()
	r.current(m, func() {
		m.reader = c.GetAPIReader()
		m.info.Problem = ""
		r.inventory.SetCluster(m.cluster.Name, m.info)
	})
	down.over()
	log.Info("The cluster's cache has synced", "server", conn.restConfig.Host)

	recheck := make(<-chan time.Time)
	if m.cluster.Local() {
		ticker := time.NewTicker(r.opts.RetryInterval)
		defer ticker.Stop()
		recheck = ticker.C
	}
	probes := time.NewTicker(r.opts.ProbeInterval)
	defer probes.Stop()
	// failingSince is when the first of the probes that have failed in a
	// row was sent, zero while the last probe succeeded.
	var failingSince time.Time
	for {
		select {
		case <-ctx.Done():
			return outcome{synced: true}
		case err := <-stopped:
			stopped <- err
			down.report(log, err, "The cluster's cache stopped")
			return outcome{synced: true}
		case <-recheck:
			latest, err := r.kubeconfig(m.cluster)
			if err != nil || !latest.Equal(conn.kubeconfig) {
				log.Info("The mounted remote-access token changed: connecting again")
				return outcome{synced: true, reconnectNow: true}
			}
		case <-probes.C:
			sent := time.Now()
			err := r.probe(ctx, c)
			switch {
			case err == nil:
				if !failingSince.IsZero() {
					log.Info("The cluster's API server answers again")
				}
				failingSince = time.Time{}
				down.over()
			case ctx.Err() != nil:
				return outcome{synced: true}
			case failingSince.IsZero():
				failingSince = sent
				down.report(log, err, "The cluster's API server did not answer a probe")
			case time.Since(failingSince) >= r.opts.UnreachableAfter:
				down.report(log, err, "The cluster's API server has not answered for a while: the cluster contributes "+
					"no portals until its cache has synced again", "for", time.Since(failingSince).Round(time.Second).String())
				return outcome{synced: true, unreachable: fmt.Sprintf(
					"the cluster's API server has not answered since %s: %v",
					failingSince.UTC().Format(time.RFC3339), err)}
			}
		}
	}
}

// probe checks that the cluster's API server answers, and still lets the
// identity the cluster is reached as list training portals, within the
// probe timeout.
func (r *Registry) probe(ctx context.Context, c cluster.Cluster) error {
	ctx, cancel := context.WithTimeout(ctx, r.opts.ProbeTimeout)
	defer cancel()
	portals := &unstructured.UnstructuredList{}
	portals.SetGroupVersionKind(trainingPortalKind.GroupVersion().WithKind(trainingPortalKind.Kind + "List"))
	return c.GetAPIReader().List(ctx, portals, client.Limit(1))
}

// WaitForSync waits until each of the clusters of those names has settled,
// for the ready timeout at most: its cache has synced, or its configuration
// gives no connection, so that waiting would not let it sync. It returns
// the names of the clusters whose caches have not synced: those without a
// connection, those that cannot be reached, and those the configuration no
// longer names.
func (r *Registry) WaitForSync(ctx context.Context, names []string) []string {
	ctx, cancel := context.WithTimeout(ctx, r.opts.ReadyTimeout)
	defer cancel()
	ticker := time.NewTicker(syncPollInterval)
	defer ticker.Stop()
	for {
		unsynced, awaited := r.unsynced(names)
		if !awaited {
			return unsynced
		}
		select {
		case <-ctx.Done():
			return unsynced
		case <-ticker.C:
		}
	}
}

// unsynced returns those of the names that are not the name of a cluster
// whose cache has synced, and whether any of them is still worth waiting
// for, which a cluster whose configuration gives no connection is not.
func (r *Registry) unsynced(names []string) (unsynced []string, awaited bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, name := range names {
		m, ok := r.members[name]
		if ok && m.reader != nil {
			continue
		}
		unsynced = append(unsynced, name)
		if !ok || !m.noConnection {
			awaited = true
		}
	}
	return unsynced, awaited
}

// newCluster creates a controller-runtime cluster that reaches the API
// server with restConfig. Its cache watches only the training resources,
// read as unstructured objects; a mapper that knows them, and Secrets, saves
// the cluster from discovering the API server's resources.
func (r *Registry) newCluster(restConfig *rest.Config, log logr.Logger, down *outage) (cluster.Cluster, error) {
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
			DefaultWatchErrorHandler: watchErrorHandler(log, down),
		}
	})
}

// watchErrorHandler reports to down why listing or watching a cluster's
// training resources failed, which the reflector then retries.
func watchErrorHandler(log logr.Logger, down *outage) toolscache.WatchErrorHandlerWithContext {
	return func(_ context.Context, _ *toolscache.Reflector, err error) {
		switch {
		case errors.Is(err, io.EOF):
		case apierrors.IsResourceExpired(err) || apierrors.IsGone(err) || errors.Is(err, io.ErrUnexpectedEOF):
			log.V(1).Info("The watch of the cluster's training resources closed", "reason", err.Error())
		default:
			down.report(log, err, "Cannot list or watch the cluster's training resources")
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
