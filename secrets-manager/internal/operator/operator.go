// Package operator composes the secrets-manager: one controller-runtime
// manager, cluster wide, running the reconciler of each secrets.educates.dev
// kind with the field indexes and map functions that turn events into its
// reconciles, and the copy engine that writes copies.
package operator

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
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

	// NewCache, when set, creates the cache the secrets-manager reads and
	// watches through, in place of controller-runtime's.
	NewCache cache.NewCacheFunc
}

// DefaultBackstop and DefaultBackoffCap are the backstop interval and the
// backoff cap of the secrets-manager when Options leave them unset.
const (
	DefaultBackstop   = 5 * time.Minute
	DefaultBackoffCap = 5 * time.Minute
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
		// Nothing the secrets-manager reads from the cache needs the
		// managed fields: the copy engine reads them from the API server.
		Cache: cache.Options{DefaultTransform: cache.TransformStripManagedFields()},
		// Secrets are cached as metadata only, so the cache never holds the
		// data of every Secret in the cluster; a Secret's data is read from
		// the API server.
		Client:                 client.Options{Cache: &client.CacheOptions{DisableFor: []client.Object{&corev1.Secret{}}}},
		NewCache:               opts.NewCache,
		Metrics:                metricsserver.Options{BindAddress: "0"},
		HealthProbeBindAddress: "0",
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

	// The client reads Secrets from the API server; the cache serves their
	// metadata.
	secrets := copyengine.New(copyengine.Secret, mgr.GetCache(), mgr.GetAPIReader(), mgr.GetClient(),
		FieldManager, lookupRules(mgr.GetClient()))
	if err := forgetDeletedCopies(ctx, mgr.GetCache(), secrets); err != nil {
		return nil, err
	}
	policy := convergence.Policy{Backstop: opts.Backstop, BackoffCap: opts.BackoffCap}
	if policy.Backstop <= 0 {
		policy.Backstop = DefaultBackstop
	}
	if policy.BackoffCap <= 0 {
		policy.BackoffCap = DefaultBackoffCap
	}

	if err := secretcopier.New(mgr.GetClient(), secrets, policy).SetupWithManager(mgr); err != nil {
		return nil, fmt.Errorf("setting up the SecretCopier reconciler: %w", err)
	}
	injectors := secretinjector.New(mgr.GetCache(), mgr.GetAPIReader(), mgr.GetClient(), policy)
	if err := injectors.SetupWithManager(mgr); err != nil {
		return nil, fmt.Errorf("setting up the SecretInjector reconciler: %w", err)
	}

	return &Operator{manager: mgr}, nil
}

// Run runs the secrets-manager until ctx is done or it fails.
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

// secretMetadata is the object the cache holds a Secret's metadata in.
func secretMetadata() *metav1.PartialObjectMetadata {
	metadata := &metav1.PartialObjectMetadata{}
	metadata.SetGroupVersionKind(corev1.SchemeGroupVersion.WithKind("Secret"))
	return metadata
}

// forgetDeletedCopies has engine forget each Secret the cache sees
// deleted, so that what it remembers of copies stays bounded by the
// Secrets that exist.
func forgetDeletedCopies(ctx context.Context, c cache.Cache, engine *copyengine.Engine) error {
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
				engine.Forget(client.ObjectKeyFromObject(deleted))
			}
		},
	})
	if err != nil {
		return fmt.Errorf("watching Secret deletions: %w", err)
	}
	return nil
}
