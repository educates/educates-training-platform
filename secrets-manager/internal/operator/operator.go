// Package operator composes the secrets-manager: one controller-runtime
// manager, cluster wide, running the reconciler of each secrets.educates.dev
// kind with the field indexes and map functions that turn events into its
// reconciles, the copy engine that writes copies, and the feedback that
// writes each resource's status and Events.
package operator

import (
	"context"
	"errors"
	"fmt"
	"net"
	"time"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	toolscache "k8s.io/client-go/tools/cache"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/config"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	secretsv1beta1 "github.com/educates/educates-training-platform/secrets-manager/api/v1beta1"
	"github.com/educates/educates-training-platform/secrets-manager/internal/convergence"
	"github.com/educates/educates-training-platform/secrets-manager/internal/copyengine"
	"github.com/educates/educates-training-platform/secrets-manager/internal/eventmap"
	"github.com/educates/educates-training-platform/secrets-manager/internal/exportimport"
	"github.com/educates/educates-training-platform/secrets-manager/internal/feedback"
	"github.com/educates/educates-training-platform/secrets-manager/internal/secretcopier"
	"github.com/educates/educates-training-platform/secrets-manager/internal/secretinjector"
)

// FieldManager is the field manager the secrets-manager applies copies as,
// whichever rule writes them.
const FieldManager = "educates-secrets-manager"

// Options configure the secrets-manager.
type Options struct {
	// RESTConfig reaches the API server of the cluster the secrets-manager
	// runs on.
	RESTConfig *rest.Config

	// Logger receives the secrets-manager's logs.
	Logger logr.Logger

	// Backstop is how long after its last reconcile each resource is
	// reconciled again, though no event asks for it, so that a change whose
	// event was missed or mapped to the wrong resource still converges. It
	// defaults to DefaultBackstop.
	Backstop time.Duration

	// BackoffCap caps how long a resource whose reconcile failed waits
	// before it is reconciled again. It defaults to DefaultBackoffCap.
	BackoffCap time.Duration

	// NotReadyGrace is how long a resource stays not Ready before the
	// secrets-manager raises a Warning Event about it, so that a copier
	// created just before its source, or an importer just before its
	// exporter, raises none. It defaults to DefaultNotReadyGrace.
	NotReadyGrace time.Duration

	// NewCache, when set, creates the cache the secrets-manager reads and
	// watches through, in place of controller-runtime's.
	NewCache cache.NewCacheFunc

	// ProbeListener, when set, is where the liveness probe, /healthz, and
	// the readiness probe, /readyz, are served. The secrets-manager is live
	// while its process serves the probes, and ready once its caches have
	// synced, whether or not it leads.
	ProbeListener net.Listener

	// LeaderElection has the secrets-manager reconcile only while it holds
	// the Lease LeaseName in LeaderElectionNamespace, so that of several
	// replicas only one writes. Every replica caches what it watches, so a
	// standby takes over with its caches synced. A replica that stops
	// releases the Lease, so another takes over at once.
	LeaderElection bool

	// LeaderElectionNamespace is the namespace of the Lease. Empty means the
	// namespace of the pod's service account.
	LeaderElectionNamespace string
}

// LeaseName names the Lease the replicas of the secrets-manager elect their
// leader with.
const LeaseName = "secrets-manager"

// DefaultBackstop, DefaultBackoffCap and DefaultNotReadyGrace are the
// backstop interval, the backoff cap and the grace period of the
// secrets-manager when Options leave them unset.
const (
	DefaultBackstop      = 5 * time.Minute
	DefaultBackoffCap    = 5 * time.Minute
	DefaultNotReadyGrace = 60 * time.Second
)

// Operator is the composed secrets-manager.
type Operator struct {
	manager manager.Manager
}

// New composes the secrets-manager. It does not reach the API server until
// Run.
func New(ctx context.Context, opts Options) (*Operator, error) {
	if opts.RESTConfig == nil {
		return nil, errors.New("the REST config is required")
	}

	scheme := runtime.NewScheme()
	if err := clientgoscheme.AddToScheme(scheme); err != nil {
		return nil, err
	}
	if err := secretsv1beta1.AddToScheme(scheme); err != nil {
		return nil, err
	}

	// Controller names are unique within the composition, which runs no
	// metrics server, so the process-wide check of their names is skipped:
	// it would refuse to compose the secrets-manager a second time in one
	// process.
	skipNameValidation := true
	mgr, err := ctrl.NewManager(opts.RESTConfig, ctrl.Options{
		Scheme:     scheme,
		Logger:     opts.Logger,
		Controller: config.Controller{SkipNameValidation: &skipNameValidation},
		Cache:      cache.Options{DefaultTransform: cacheTransform()},
		// Secrets are cached as metadata only, so the cache never holds the
		// data of every Secret in the cluster; a Secret's data is read from
		// the API server.
		Client:   client.Options{Cache: &client.CacheOptions{DisableFor: []client.Object{&corev1.Secret{}}}},
		NewCache: opts.NewCache,
		// The secrets-manager serves its probes itself, and no metrics.
		Metrics:                       metricsserver.Options{BindAddress: "0"},
		HealthProbeBindAddress:        "0",
		LeaderElection:                opts.LeaderElection,
		LeaderElectionID:              LeaseName,
		LeaderElectionNamespace:       opts.LeaderElectionNamespace,
		LeaderElectionReleaseOnCancel: true,
	})
	if err != nil {
		return nil, fmt.Errorf("creating the manager: %w", err)
	}

	for _, obj := range cachedKinds() {
		if _, err := mgr.GetCache().GetInformer(ctx, obj); err != nil {
			return nil, fmt.Errorf("caching %T: %w", obj, err)
		}
	}
	if err := eventmap.IndexSecretCopiers(ctx, mgr.GetFieldIndexer()); err != nil {
		return nil, fmt.Errorf("indexing SecretCopiers: %w", err)
	}
	if err := eventmap.IndexSecretExporters(ctx, mgr.GetFieldIndexer()); err != nil {
		return nil, fmt.Errorf("indexing SecretExporters: %w", err)
	}
	if err := eventmap.IndexSecretImporters(ctx, mgr.GetFieldIndexer()); err != nil {
		return nil, fmt.Errorf("indexing SecretImporters: %w", err)
	}

	// The client reads Secrets from the API server; the cache serves their
	// metadata.
	secrets := copyengine.New(copyengine.Secret, mgr.GetCache(), mgr.GetAPIReader(), mgr.GetClient(),
		FieldManager, lookupRules(mgr.GetClient()))
	policy := convergence.Policy{Backstop: opts.Backstop, BackoffCap: opts.BackoffCap}
	if policy.Backstop <= 0 {
		policy.Backstop = DefaultBackstop
	}
	if policy.BackoffCap <= 0 {
		policy.BackoffCap = DefaultBackoffCap
	}

	grace := opts.NotReadyGrace
	if grace <= 0 {
		grace = DefaultNotReadyGrace
	}
	reports := feedback.New(mgr.GetClient(), mgr.GetAPIReader(), mgr.GetEventRecorder(FieldManager), scheme, grace)
	if err := mgr.Add(reports); err != nil {
		return nil, fmt.Errorf("adding the status and Events writer: %w", err)
	}
	if err := forgetDeletedCopies(ctx, mgr.GetCache(), secrets.Forget, reports.ForgetCopy); err != nil {
		return nil, err
	}

	if err := secretcopier.New(mgr.GetClient(), secrets, policy, reports).SetupWithManager(mgr); err != nil {
		return nil, fmt.Errorf("setting up the SecretCopier reconciler: %w", err)
	}
	exporters := exportimport.NewExporterReconciler(mgr.GetClient(), secrets, policy, reports)
	if err := exporters.SetupWithManager(mgr); err != nil {
		return nil, fmt.Errorf("setting up the SecretExporter reconciler: %w", err)
	}
	// The SecretImporter reconciler reads the metadata of Secrets, which
	// only the cache itself serves: the client reads Secrets from the API
	// server.
	if err := exportimport.NewImporterReconciler(mgr.GetCache(), policy, reports).SetupWithManager(mgr); err != nil {
		return nil, fmt.Errorf("setting up the SecretImporter reconciler: %w", err)
	}
	injectors := secretinjector.New(mgr.GetCache(), mgr.GetAPIReader(), mgr.GetClient(), policy, reports)
	if err := injectors.SetupWithManager(mgr); err != nil {
		return nil, fmt.Errorf("setting up the SecretInjector reconciler: %w", err)
	}

	if opts.ProbeListener != nil {
		if err := addProbes(mgr, opts.ProbeListener); err != nil {
			return nil, err
		}
	}

	return &Operator{manager: mgr}, nil
}

// addProbes serves the probes on listener. The manager starts its HTTP
// servers before its caches, so the liveness probe answers while the caches
// sync.
func addProbes(mgr manager.Manager, listener net.Listener) error {
	p := &probes{cache: mgr.GetCache()}
	if err := mgr.Add(p); err != nil {
		return fmt.Errorf("adding the readiness check: %w", err)
	}
	shutdownTimeout := probeTimeout
	server := &manager.Server{Name: "probes", Server: p.server(), Listener: listener, ShutdownTimeout: &shutdownTimeout}
	if err := mgr.Add(server); err != nil {
		return fmt.Errorf("adding the probe server: %w", err)
	}
	return nil
}

// Run runs the secrets-manager until ctx is done or it fails. Under leader
// election, losing the Lease is a failure: the process is meant to exit and
// be restarted as a standby.
func (o *Operator) Run(ctx context.Context) error {
	return o.manager.Start(ctx)
}

// cachedKinds are the kinds the cache holds from the start, whichever
// reconciler reads them: namespaces, service accounts and the four
// secrets.educates.dev kinds in full, and Secrets as metadata only.
func cachedKinds() []client.Object {
	return []client.Object{
		&corev1.Namespace{},
		&corev1.ServiceAccount{},
		&secretsv1beta1.SecretCopier{},
		&secretsv1beta1.SecretExporter{},
		&secretsv1beta1.SecretImporter{},
		&secretsv1beta1.SecretInjector{},
		secretMetadata(),
	}
}

// lastAppliedAnnotation is where kubectl apply keeps the manifest it last
// applied to an object: for a Secret, its data.
const lastAppliedAnnotation = "kubectl.kubernetes.io/last-applied-configuration"

// cacheTransform is what the cache drops from each object before holding
// it. Nothing the secrets-manager reads from the cache needs the managed
// fields: the copy engine reads them from the API server. From the metadata
// of Secrets it also drops the manifest kubectl apply keeps in an
// annotation, which holds the Secret's data.
func cacheTransform() toolscache.TransformFunc {
	stripManagedFields := cache.TransformStripManagedFields()
	return func(obj any) (any, error) {
		if metadata, ok := obj.(*metav1.PartialObjectMetadata); ok {
			delete(metadata.Annotations, lastAppliedAnnotation)
		}
		return stripManagedFields(obj)
	}
}

// secretMetadata is the object the cache holds a Secret's metadata in.
func secretMetadata() *metav1.PartialObjectMetadata {
	metadata := &metav1.PartialObjectMetadata{}
	metadata.SetGroupVersionKind(corev1.SchemeGroupVersion.WithKind("Secret"))
	return metadata
}

// forgetDeletedCopies calls each of forget with the key of each Secret the
// cache sees deleted, so that what the copy engine and the feedback
// remember of copies stays bounded by the Secrets that exist.
func forgetDeletedCopies(ctx context.Context, c cache.Cache, forget ...func(types.NamespacedName)) error {
	informer, err := c.GetInformer(ctx, secretMetadata())
	if err != nil {
		return fmt.Errorf("caching Secret metadata: %w", err)
	}
	_, err = informer.AddEventHandler(toolscache.ResourceEventHandlerFuncs{
		DeleteFunc: func(obj any) {
			if tombstone, ok := obj.(toolscache.DeletedFinalStateUnknown); ok {
				obj = tombstone.Obj
			}
			if deleted, ok := obj.(client.Object); ok {
				for _, f := range forget {
					f(client.ObjectKeyFromObject(deleted))
				}
			}
		},
	})
	if err != nil {
		return fmt.Errorf("watching Secret deletions: %w", err)
	}
	return nil
}
