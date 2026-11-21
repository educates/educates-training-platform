package secretinjector

import (
	"sync"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
)

// secretTypes remembers, for each SecretInjector, the types of the Secrets
// its last reconcile injected, by UID. The cache holds Secrets as metadata
// only, and a Secret's type never changes, so each Secret's type is read
// from the API server once. Each reconcile keeps only the Secrets it used,
// so what is remembered is bounded by what the injectors select.
type secretTypes struct {
	mu         sync.Mutex
	byInjector map[string]map[types.UID]corev1.SecretType
}

func newSecretTypes() *secretTypes {
	return &secretTypes{byInjector: map[string]map[types.UID]corev1.SecretType{}}
}

// remembered returns the types the last reconcile of the SecretInjector
// named injector used. The caller must not change the map.
func (t *secretTypes) remembered(injector string) map[types.UID]corev1.SecretType {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.byInjector[injector]
}

// remember replaces the types remembered for the SecretInjector named
// injector.
func (t *secretTypes) remember(injector string, byUID map[types.UID]corev1.SecretType) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.byInjector[injector] = byUID
}

// forget drops the types remembered for the SecretInjector named injector,
// once it no longer exists.
func (t *secretTypes) forget(injector string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	delete(t.byInjector, injector)
}
