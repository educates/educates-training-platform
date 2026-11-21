package scenarios

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/educates/educates-training-platform/secrets-manager/internal/operator"
)

// chartCRDs is the crds/ directory of the secrets-manager subchart, which
// holds the schema of record of the four kinds.
var chartCRDs = filepath.Join("..", "..", "..", "installer", "charts", "educates-training-platform", "charts",
	"secrets-manager", "crds")

// envtestBackstop and envtestBackoffCap shorten the secrets-manager's
// backstop and backoff cap on envtest. The backstop stays longer than the
// time scenarios allow to converge, so that no scenario passes on the
// backstop where events should have converged it.
const (
	envtestBackstop   = 45 * time.Second
	envtestBackoffCap = time.Second
)

// envtestNotReadyGrace shortens the grace period before a Warning Event on
// envtest, leaving a scenario time to create a source well within it.
const envtestNotReadyGrace = 6 * time.Second

// envtestTiming allows for a secrets-manager that reacts to events within
// moments, on an API server with nothing else running.
var envtestTiming = Timing{
	Converge:      30 * time.Second,
	Hold:          3 * time.Second,
	Poll:          200 * time.Millisecond,
	NotReadyGrace: envtestNotReadyGrace,
	Backstop:      envtestBackstop,
	BackoffCap:    envtestBackoffCap,
}

// newEnvtestTarget starts an envtest API server with the four kinds loaded
// from the subchart's CRDs, and the secrets-manager's composition in this
// process against it. envtest runs no garbage collector and no namespace
// controller: a deleted namespace stays Terminating. The requests of the
// secrets-manager are counted, and it can be made to miss events and be
// restarted.
func newEnvtestTarget() (*Target, error) {
	env := &envtest.Environment{CRDDirectoryPaths: []string{chartCRDs}, ErrorIfCRDPathMissing: true}
	if dir := firstEnvTestBinaryDir(); dir != "" {
		env.BinaryAssetsDirectory = dir
	}
	config, err := env.Start()
	if err != nil {
		return nil, fmt.Errorf("starting envtest: %w", err)
	}
	config.QPS = 50
	config.Burst = 100

	c, err := client.New(config, client.Options{Scheme: newScheme()})
	if err != nil {
		return nil, errors.Join(fmt.Errorf("creating a client for envtest: %w", err), env.Stop())
	}

	calls := &APICalls{}
	composedConfig := rest.CopyConfig(config)
	calls.instrument(composedConfig)
	missed := &MissedEvents{}
	secretsManager := &inProcess{options: operator.Options{
		RESTConfig:    composedConfig,
		Logger:        logf.Log.WithName("secrets-manager"),
		Backstop:      envtestBackstop,
		BackoffCap:    envtestBackoffCap,
		NotReadyGrace: envtestNotReadyGrace,
		NewCache:      missed.newCache,
	}}
	if err := secretsManager.start(); err != nil {
		return nil, errors.Join(err, env.Stop())
	}

	return &Target{
		Name:         "envtest API server, with the secrets-manager in process",
		Config:       config,
		Client:       c,
		Timing:       envtestTiming,
		APICalls:     calls,
		MissedEvents: missed,
		Restart: func() error {
			if err := secretsManager.stop(); err != nil {
				return err
			}
			return secretsManager.start()
		},
		stop: func() error {
			return errors.Join(secretsManager.stop(), env.Stop())
		},
	}, nil
}

// inProcess runs the secrets-manager's composition in this process.
type inProcess struct {
	options operator.Options
	cancel  context.CancelFunc
	stopped chan error
}

// start composes the secrets-manager anew and runs it.
func (p *inProcess) start() error {
	ctx, cancel := context.WithCancel(context.Background())
	secretsManager, err := operator.New(ctx, p.options)
	if err != nil {
		cancel()
		return fmt.Errorf("composing the secrets-manager: %w", err)
	}
	p.cancel, p.stopped = cancel, make(chan error, 1)
	go func() { p.stopped <- secretsManager.Run(ctx) }()
	return nil
}

// stop stops the secrets-manager, when it runs, and waits until it has
// stopped.
func (p *inProcess) stop() error {
	if p.cancel == nil {
		return nil
	}
	p.cancel()
	p.cancel = nil
	return <-p.stopped
}

// firstEnvTestBinaryDir finds the envtest binaries make downloads, for a run
// without KUBEBUILDER_ASSETS set, such as from an editor.
func firstEnvTestBinaryDir() string {
	base := filepath.Join("..", "..", "bin", "k8s")
	entries, err := os.ReadDir(base)
	if err != nil {
		return ""
	}
	for _, entry := range entries {
		if entry.IsDir() {
			return filepath.Join(base, entry.Name())
		}
	}
	return ""
}
