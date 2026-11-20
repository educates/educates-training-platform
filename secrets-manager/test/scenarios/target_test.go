package scenarios

import (
	"context"
	"fmt"
	"os"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	// liveKubeconfigEnv names the kubeconfig of a live cluster to run the
	// scenarios against. Without it the suite has no target and skips.
	liveKubeconfigEnv = "SECRETS_MANAGER_LIVE_KUBECONFIG"

	// liveContextEnv names the context of that kubeconfig to use, its
	// current context when unset.
	liveContextEnv = "SECRETS_MANAGER_LIVE_CONTEXT"
)

// Target is a Kubernetes API server with a secrets-manager reconciling the
// secrets.educates.dev resources on it. Scenarios reach it only through
// its API, as a workshop author or platform operator does, and ask it what
// the cluster around the API server does on its own.
type Target struct {
	// Name says what the target is, for messages.
	Name string

	// Config and Client reach the target's API server. Client reads
	// through to the API server, with no cache.
	Config *rest.Config
	Client client.Client

	// GarbageCollector reports whether Kubernetes garbage collection runs
	// on the target, removing the dependents of a deleted owner.
	GarbageCollector bool

	// NamespaceController reports whether the namespace controller runs
	// on the target, emptying and removing a deleted namespace.
	NamespaceController bool

	// Timing bounds how long scenarios wait on the target.
	Timing Timing

	// stop releases what the target started, when anything.
	stop func() error
}

// Timing bounds how long scenarios wait for the secrets-manager.
type Timing struct {
	// Converge is how long a scenario waits for the secrets-manager to
	// bring the cluster to what it expects.
	Converge time.Duration

	// Hold is how long a scenario watches for something the
	// secrets-manager must not do, long enough for any periodic pass of
	// the secrets-manager to have run after the change it made.
	Hold time.Duration

	// Poll is how often a scenario looks again while it waits.
	Poll time.Duration

	// NotReadyGrace is how long a resource stays not Ready before the
	// secrets-manager raises a Warning Event about it.
	NotReadyGrace time.Duration
}

// liveTiming allows for a secrets-manager that, besides reacting to
// events, passes over each resource every 60 seconds, the first pass 30
// seconds after it sees the resource.
var liveTiming = Timing{
	Converge:      150 * time.Second,
	Hold:          75 * time.Second,
	Poll:          time.Second,
	NotReadyGrace: 60 * time.Second,
}

// selectTarget picks the target of this run: the live cluster the
// environment names, or none.
func selectTarget() (*Target, error) {
	if kubeconfig := os.Getenv(liveKubeconfigEnv); kubeconfig != "" {
		return newLiveTarget(kubeconfig, os.Getenv(liveContextEnv))
	}
	return nil, nil
}

// newLiveTarget reaches a running cluster through the context kubeContext
// of the kubeconfig file kubeconfig. It fails unless the cluster serves the
// four secrets.educates.dev kinds.
func newLiveTarget(kubeconfig, kubeContext string) (*Target, error) {
	loader := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
		&clientcmd.ClientConfigLoadingRules{ExplicitPath: kubeconfig},
		&clientcmd.ConfigOverrides{CurrentContext: kubeContext},
	)
	config, err := loader.ClientConfig()
	if err != nil {
		return nil, fmt.Errorf("loading kubeconfig %s: %w", kubeconfig, err)
	}
	config.QPS = 50
	config.Burst = 100

	c, err := client.New(config, client.Options{Scheme: newScheme()})
	if err != nil {
		return nil, fmt.Errorf("creating a client for %s: %w", kubeconfig, err)
	}
	if err := checkKindsServed(c); err != nil {
		return nil, err
	}

	name := "live cluster"
	if raw, err := loader.RawConfig(); err == nil {
		current := kubeContext
		if current == "" {
			current = raw.CurrentContext
		}
		name = fmt.Sprintf("live cluster %q", current)
	}

	return &Target{
		Name:                name,
		Config:              config,
		Client:              c,
		GarbageCollector:    true,
		NamespaceController: true,
		Timing:              liveTiming,
	}, nil
}

// Stop releases what the target started.
func (t *Target) Stop() error {
	if t == nil || t.stop == nil {
		return nil
	}
	return t.stop()
}

// newScheme holds the built-in kinds. The secrets.educates.dev kinds are
// handled as unstructured objects, as a client without their Go types
// handles them.
func newScheme() *runtime.Scheme {
	scheme := runtime.NewScheme()
	if err := clientgoscheme.AddToScheme(scheme); err != nil {
		panic(err)
	}
	return scheme
}

// checkKindsServed fails unless the API server serves each of the four
// kinds.
func checkKindsServed(c client.Client) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	for _, kind := range []string{kindSecretCopier, kindSecretExporter, kindSecretImporter, kindSecretInjector} {
		list := &unstructured.UnstructuredList{}
		list.SetGroupVersionKind(schema.GroupVersionKind{Group: group, Version: version, Kind: kind + "List"})
		if err := c.List(ctx, list, client.Limit(1)); err != nil {
			if apierrors.IsNotFound(err) || meta.IsNoMatchError(err) {
				return fmt.Errorf("the cluster does not serve %s.%s/%s", kind, group, version)
			}
			return fmt.Errorf("listing %s: %w", kind, err)
		}
	}
	return nil
}
