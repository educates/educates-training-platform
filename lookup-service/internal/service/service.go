// Package service composes the Lookup Service: the config watcher, the
// cluster registry and its inventory, the token store, the placement of
// session requests through the training portal client, the REST API and
// the probes, run as one process.
package service

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"strings"
	"sync/atomic"
	"time"

	"github.com/go-logr/logr"
	"golang.org/x/sync/errgroup"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	crconfig "sigs.k8s.io/controller-runtime/pkg/config"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	lookupv1beta1 "github.com/educates/educates-training-platform/lookup-service/api/v1beta1"
	"github.com/educates/educates-training-platform/lookup-service/internal/api"
	"github.com/educates/educates-training-platform/lookup-service/internal/clusters"
	"github.com/educates/educates-training-platform/lookup-service/internal/config"
	"github.com/educates/educates-training-platform/lookup-service/internal/inventory"
	"github.com/educates/educates-training-platform/lookup-service/internal/placement"
	"github.com/educates/educates-training-platform/lookup-service/internal/tokens"
	"github.com/educates/educates-training-platform/lookup-service/internal/trainingportal"
)

const (
	// ConfigNamespaceEnv names the environment variable that names the
	// config namespace.
	ConfigNamespaceEnv = "OPERATOR_NAMESPACE"

	// DefaultConfigNamespace is the config namespace when the environment
	// names none.
	DefaultConfigNamespace = "educates-config"

	// ServiceNamespaceEnv names the environment variable that names the
	// namespace the service runs in, where it keeps its token Secret. In a
	// pod the service account's namespace is used when it is not set.
	ServiceNamespaceEnv = "POD_NAMESPACE"

	serviceAccountNamespaceFile = "/var/run/secrets/kubernetes.io/serviceaccount/namespace"

	shutdownTimeout   = 10 * time.Second
	readHeaderTimeout = 10 * time.Second
)

// Options configure the service.
type Options struct {
	// RESTConfig reaches the API server of the cluster the service runs on.
	RESTConfig *rest.Config

	// ConfigNamespace is the namespace of the lookup.educates.dev resources.
	// Empty means the namespace ConfigNamespaceEnv names, or
	// DefaultConfigNamespace.
	ConfigNamespace string

	// ServiceNamespace is the namespace the service keeps its token Secret
	// in. Empty means the namespace ServiceNamespaceEnv names, or the
	// namespace of the pod's service account.
	ServiceNamespace string

	// APIListener is where the REST API is served.
	APIListener net.Listener

	// ProbeListener is where the liveness probe, /healthz, and the readiness
	// probe, /readyz, are served.
	ProbeListener net.Listener

	// Clusters configure how the service reaches the clusters its
	// ClusterConfigs name. Unset fields take their defaults: the local
	// cluster is the in-cluster API server, reached with the remote-access
	// token mounted under /opt/cluster-access-token.
	Clusters clusters.Options

	// Portals configure how the service reaches the training portals it
	// requests sessions from. Unset fields take their defaults: each call
	// times out after 10 seconds, and TLS trusts the system roots and the
	// ingress CA mounted under /opt/app-root/certs/ingress-ca.
	Portals trainingportal.Options

	// Placement configures how the service places session requests. Unset
	// fields take their defaults: a session request is answered within 12
	// seconds, whichever portals it contacts.
	Placement placement.Options

	// Logger receives the service's logs.
	Logger logr.Logger
}

// Service is the composed Lookup Service.
type Service struct {
	opts    Options
	log     logr.Logger
	manager manager.Manager
	api     *http.Server
	probes  *http.Server
	ready   atomic.Bool
}

// New composes the service. It opens the token store, creating its Secret on
// the first start, so it needs the API server.
func New(ctx context.Context, opts Options) (*Service, error) {
	if opts.RESTConfig == nil || opts.APIListener == nil || opts.ProbeListener == nil {
		return nil, errors.New("the REST config and both listeners are required")
	}
	var err error
	if opts.ConfigNamespace == "" {
		opts.ConfigNamespace = configNamespaceFromEnvironment()
	}
	if opts.ServiceNamespace == "" {
		if opts.ServiceNamespace, err = serviceNamespaceFromEnvironment(); err != nil {
			return nil, err
		}
	}
	log := opts.Logger
	s := &Service{opts: opts, log: log}

	scheme := runtime.NewScheme()
	if err := clientgoscheme.AddToScheme(scheme); err != nil {
		return nil, err
	}
	if err := lookupv1beta1.AddToScheme(scheme); err != nil {
		return nil, err
	}

	s.manager, err = ctrl.NewManager(opts.RESTConfig, ctrl.Options{
		Scheme: scheme,
		Logger: log,
		Cache: cache.Options{
			DefaultNamespaces: map[string]cache.Config{opts.ConfigNamespace: {}},
		},
		// The REST API and the probes are served by the service itself.
		Metrics:                metricsserver.Options{BindAddress: "0"},
		HealthProbeBindAddress: "0",
		// Controller names are unique per process unless validation is
		// skipped, and tests compose the service more than once.
		Controller: crconfig.Controller{SkipNameValidation: new(true)},
	})
	if err != nil {
		return nil, fmt.Errorf("creating the manager: %w", err)
	}

	direct, err := client.New(opts.RESTConfig, client.Options{Scheme: scheme})
	if err != nil {
		return nil, fmt.Errorf("creating a client: %w", err)
	}
	store, err := tokens.Open(logr.NewContext(ctx, log.WithName("tokens")), direct, direct, opts.ServiceNamespace)
	if err != nil {
		return nil, err
	}

	inv := inventory.New()
	registry, err := clusters.NewRegistry(inv, opts.Clusters, log.WithName("clusters"))
	if err != nil {
		return nil, err
	}
	if err := s.manager.Add(registry); err != nil {
		return nil, err
	}

	model := config.NewModel(s.manager.GetClient(), opts.ConfigNamespace)
	watcher, err := config.NewWatcher(s.manager, model, store, registry)
	if err != nil {
		return nil, err
	}
	err = s.manager.Add(manager.RunnableFunc(func(ctx context.Context) error {
		if err := watcher.WaitForSync(ctx); err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return err
		}
		s.ready.Store(true)
		log.Info("Ready: the configuration caches have synced", "configNamespace", opts.ConfigNamespace)
		<-ctx.Done()
		return nil
	}))
	if err != nil {
		return nil, err
	}

	portals, err := trainingportal.NewClient(opts.Portals, registry.APIReader, log.WithName("portals"))
	if err != nil {
		return nil, err
	}
	placer := placement.NewPlacer(inv, portals, opts.Placement, log.WithName("placement"))

	s.api = &http.Server{
		Handler:           api.NewServer(model, inv, store, placer, log.WithName("api")),
		ReadHeaderTimeout: readHeaderTimeout,
	}
	s.probes = &http.Server{
		Handler:           s.probeHandler(),
		ReadHeaderTimeout: readHeaderTimeout,
	}
	return s, nil
}

// Run serves the REST API and the probes and runs the watches until ctx is
// done or one of them fails, then stops them all.
func (s *Service) Run(ctx context.Context) error {
	group, ctx := errgroup.WithContext(ctx)

	group.Go(func() error { return s.manager.Start(ctx) })
	for _, server := range []struct {
		name     string
		server   *http.Server
		listener net.Listener
	}{
		{"REST API", s.api, s.opts.APIListener},
		{"probes", s.probes, s.opts.ProbeListener},
	} {
		group.Go(func() error {
			s.log.Info("Serving", "server", server.name, "address", server.listener.Addr().String())
			if err := server.server.Serve(server.listener); !errors.Is(err, http.ErrServerClosed) {
				return fmt.Errorf("serving the %s: %w", server.name, err)
			}
			return nil
		})
		group.Go(func() error {
			<-ctx.Done()
			shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), shutdownTimeout)
			defer cancel()
			return server.server.Shutdown(shutdownCtx)
		})
	}

	return group.Wait()
}

func (s *Service) probeHandler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("ok"))
	})
	mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, _ *http.Request) {
		if !s.ready.Load() {
			http.Error(w, "the configuration caches have not synced", http.StatusServiceUnavailable)
			return
		}
		_, _ = w.Write([]byte("ok"))
	})
	return mux
}

func configNamespaceFromEnvironment() string {
	if namespace := os.Getenv(ConfigNamespaceEnv); namespace != "" {
		return namespace
	}
	return DefaultConfigNamespace
}

func serviceNamespaceFromEnvironment() (string, error) {
	if namespace := os.Getenv(ServiceNamespaceEnv); namespace != "" {
		return namespace, nil
	}
	data, err := os.ReadFile(serviceAccountNamespaceFile)
	if err != nil {
		return "", fmt.Errorf("the service's namespace is unknown: set %s", ServiceNamespaceEnv)
	}
	return strings.TrimSpace(string(data)), nil
}
