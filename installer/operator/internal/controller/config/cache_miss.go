/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0
*/

package config

import (
	"context"
	"sort"

	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

// warnIfCacheMiss logs a structured message at Info level (logr has no
// warning level) when a Secret ref points at a namespace the operator's
// Secret informer doesn't cover.
// APIReader reads (used everywhere cross-namespace) succeed regardless,
// so the install proceeds — but Secret-change watches in that namespace
// will never fire, so rotations won't trigger re-reconciliation until
// pod restart or the 10h relist.
//
// The cache scope is set at operator startup (cmd/secretcache.go) from
// (operatorNamespace ∪ namespaces referenced by the ECC at boot). A user editing the ECC to point at a fresh namespace
// post-deploy is the case this warns about.
//
// Empty CachedSecretNamespaces disables the message, as used by tests
// that don't populate the field.
//
// TODO(followup): once the operator gains an EventRecorder, also emit
// a Warning event on the EducatesClusterConfig so the message shows up
// in `kubectl describe`.
func (r *EducatesClusterConfigReconciler) warnIfCacheMiss(ctx context.Context, ns, field string) {
	if len(r.CachedSecretNamespaces) == 0 {
		return
	}
	if r.CachedSecretNamespaces[ns] {
		return
	}
	cached := make([]string, 0, len(r.CachedSecretNamespaces))
	for k := range r.CachedSecretNamespaces {
		cached = append(cached, k)
	}
	sort.Strings(cached)

	logf.FromContext(ctx).Info(
		"Secret informer cache miss: rotations in this namespace won't trigger reconciliation until operator restart",
		"field", field,
		"refNamespace", ns,
		"cachedNamespaces", cached,
		"action", "restart the operator pod (or move the Secret to one of the cached namespaces) to enable change detection",
	)
}
