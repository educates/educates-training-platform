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

// envtestTiming allows for a secrets-manager that reacts to events within
// moments, on an API server with nothing else running.
var envtestTiming = Timing{
	Converge: 30 * time.Second,
	Hold:     3 * time.Second,
	Poll:     200 * time.Millisecond,
}

// targetFieldScenarios is the container of the targetSecret.annotations and
// targetSecret.keys scenarios.
const targetFieldScenarios = "targetSecret.annotations and targetSecret.keys "

// noFinalizerScenario checks every kind for finalizers, so it waits for
// both exporting and injecting.
const noFinalizerScenario = "Copy ownership and reclaim adds no finalizer to any of the four kinds"

// awaitingOnEnvtest names, under the behavior they wait for, the scenarios
// the secrets-manager does not pass yet, by their full text or by the full
// text of a container that holds them. They skip on envtest.
var awaitingOnEnvtest = map[string][]string{
	"export and import": {
		"SecretExporter and SecretImporter",
		"SecretImporter gating SecretCopier rules",
		noFinalizerScenario,
	},
	"secret injection": {
		"SecretInjector",
		noFinalizerScenario,
	},
	"targetSecret.annotations and targetSecret.keys": {
		targetFieldScenarios + "sets targetSecret.annotations on every copy",
		targetFieldScenarios + "removes an annotation dropped from targetSecret.annotations from the copies",
		targetFieldScenarios + "copies only the keys targetSecret.keys lists that the source has",
		targetFieldScenarios + "removes keys outside targetSecret.keys from existing copies",
		targetFieldScenarios + "applies targetSecret.annotations and targetSecret.keys on SecretExporter rules",
		targetFieldScenarios + "fails only the target whose copy the API server rejects",
		"Admission rejects at admission",
	},
	"status and Events": {
		"Status, printer columns and Events",
	},
}

// newEnvtestTarget starts an envtest API server with the four kinds loaded
// from the subchart's CRDs, and the secrets-manager's composition in this
// process against it. envtest runs no garbage collector and no namespace
// controller: a deleted namespace stays Terminating. The requests of the
// secrets-manager are counted, and it can be restarted.
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
	secretsManager := &inProcess{options: operator.Options{
		RESTConfig: composedConfig,
		Logger:     logf.Log.WithName("secrets-manager"),
	}}
	if err := secretsManager.start(); err != nil {
		return nil, errors.Join(err, env.Stop())
	}

	return &Target{
		Name:     "envtest API server, with the secrets-manager in process",
		Config:   config,
		Client:   c,
		Timing:   envtestTiming,
		Awaiting: awaitingOnEnvtest,
		APICalls: calls,
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
