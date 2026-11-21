package operator

import (
	"context"
	"net/http"
	"sync/atomic"
	"time"

	"sigs.k8s.io/controller-runtime/pkg/cache"
)

const (
	// probeTimeout bounds how long a probe request takes to be read and
	// answered, and how long the probe server takes to shut down.
	probeTimeout = 10 * time.Second
	// probeIdleTimeout is how long a probe connection is kept open between
	// requests.
	probeIdleTimeout = 2 * time.Minute
)

// probes serves the liveness and readiness probes. The secrets-manager is
// live while its process serves them, and ready once its caches have synced.
// Readiness never depends on leading: a standby that waits for the Lease is
// ready, or a rolling update would wait forever for the new pod to become
// ready while the old one holds the Lease.
type probes struct {
	cache  cache.Cache
	synced atomic.Bool
}

// Start marks the secrets-manager ready once its caches have synced. The
// manager runs it on every replica, leading or not.
func (p *probes) Start(ctx context.Context) error {
	if p.cache.WaitForCacheSync(ctx) {
		p.synced.Store(true)
	}
	return nil
}

// NeedLeaderElection is false: readiness is reported by every replica.
func (p *probes) NeedLeaderElection() bool {
	return false
}

// server returns the HTTP server of the probes.
func (p *probes) server() *http.Server {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("ok"))
	})
	mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, _ *http.Request) {
		if !p.synced.Load() {
			http.Error(w, "the caches have not synced", http.StatusServiceUnavailable)
			return
		}
		_, _ = w.Write([]byte("ok"))
	})
	return &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: probeTimeout,
		ReadTimeout:       probeTimeout,
		WriteTimeout:      probeTimeout,
		IdleTimeout:       probeIdleTimeout,
	}
}
