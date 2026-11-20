// Package operator composes the secrets-manager: one controller-runtime
// manager, cluster wide, running the reconciler of each secrets.educates.dev
// kind with the field indexes and map functions that turn events into its
// reconciles, and the copy engine that writes copies.
package operator

import (
	"context"
	"errors"
	"fmt"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	secretsv1beta1 "github.com/educates/educates-training-platform/secrets-manager/api/v1beta1"
	"github.com/educates/educates-training-platform/secrets-manager/internal/copyengine"
	"github.com/educates/educates-training-platform/secrets-manager/internal/eventmap"
	"github.com/educates/educates-training-platform/secrets-manager/internal/secretcopier"
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
}

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

	mgr, err := ctrl.NewManager(opts.RESTConfig, ctrl.Options{
		Scheme: scheme,
		Logger: opts.Logger,
		// Nothing the secrets-manager reads from the cache needs the
		// managed fields: the copy engine reads them from the API server.
		Cache: cache.Options{DefaultTransform: cache.TransformStripManagedFields()},
		// Secrets are cached as metadata only, so the cache never holds the
		// data of every Secret in the cluster; a Secret's data is read from
		// the API server.
		Client:                 client.Options{Cache: &client.CacheOptions{DisableFor: []client.Object{&corev1.Secret{}}}},
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

	secrets := copyengine.New(copyengine.Secret, mgr.GetAPIReader(), mgr.GetClient(), FieldManager)
	if err := secretcopier.New(mgr.GetClient(), secrets).SetupWithManager(mgr); err != nil {
		return nil, fmt.Errorf("setting up the SecretCopier reconciler: %w", err)
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
	secretMetadata := &metav1.PartialObjectMetadata{}
	secretMetadata.SetGroupVersionKind(corev1.SchemeGroupVersion.WithKind("Secret"))
	return []client.Object{
		&corev1.Namespace{},
		&corev1.ServiceAccount{},
		&secretsv1beta1.SecretCopier{},
		&secretsv1beta1.SecretExporter{},
		&secretsv1beta1.SecretImporter{},
		&secretsv1beta1.SecretInjector{},
		secretMetadata,
	}
}
