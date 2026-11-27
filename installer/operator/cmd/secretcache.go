/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0
*/

package main

import (
	"context"
	"sort"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"

	configv1alpha1 "github.com/educates/educates-training-platform/installer/operator/api/config/v1alpha1"
	platformv1alpha1 "github.com/educates/educates-training-platform/installer/operator/api/platform/v1alpha1"
	configcontroller "github.com/educates/educates-training-platform/installer/operator/internal/controller/config"
)

// singletonName is the CEL-enforced name of the EducatesClusterConfig
// and SessionManager singletons.
const singletonName = "cluster"

// discoverCachedSecretNamespaces returns the deduped, sorted set of
// namespaces the operator should populate informers for, given:
//
//   - operatorNamespace (always included: user-supplied Secrets like
//     TLS, image-pull and CustomCA live here by default, and the CLI
//     syncs its local secrets cache here)
//   - any additional namespaces named by the SecretReference fields
//     the current EducatesClusterConfig singleton uses for its mode and
//     certificate provider (configcontroller.ReferencedSecretNamespaces,
//     the same references its Secret watch maps) and by the
//     SessionManager singleton's CA override
//
// Boot-time only: if the user later edits either singleton to point at a
// new namespace, the operator needs to restart to pick up watches there.
// The EducatesClusterConfig reconciler logs a message at Info level when
// one of its own references names such a namespace; a SessionManager CA
// override there is not reported.
func discoverCachedSecretNamespaces(
	ctx context.Context,
	restCfg *rest.Config,
	scheme *runtime.Scheme,
	operatorNamespace string,
) ([]string, error) {
	// One-shot uncached client just for this read. Manager isn't built
	// yet, so the regular cached client isn't available; the requests
	// are cheap (one Get per cluster-scoped singleton) and only happen
	// at startup.
	c, err := client.New(restCfg, client.Options{Scheme: scheme})
	if err != nil {
		return nil, err
	}
	return collectFromClient(ctx, c, operatorNamespace)
}

// collectFromClient is the pure-logic core of discoverCachedSecretNamespaces,
// factored so tests can drive it with a fake client without needing
// envtest or a real REST config.
func collectFromClient(ctx context.Context, c client.Reader, operatorNamespace string) ([]string, error) {
	set := map[string]struct{}{
		operatorNamespace: {},
	}

	// A missing singleton contributes nothing: the first reconcile after
	// it is created uses APIReader for the actual Secret fetch; only the
	// watch-driven enqueue depends on the cache scope, and the CLI's
	// installs reference Secrets in the operator namespace, which is
	// already in the set.
	ecc := &configv1alpha1.EducatesClusterConfig{}
	if err := c.Get(ctx, types.NamespacedName{Name: singletonName}, ecc); err == nil {
		for _, ns := range configcontroller.ReferencedSecretNamespaces(ecc) {
			set[ns] = struct{}{}
		}
	} else if !apierrors.IsNotFound(err) {
		return nil, err
	}

	sm := &platformv1alpha1.SessionManager{}
	if err := c.Get(ctx, types.NamespacedName{Name: singletonName}, sm); err == nil {
		if sm.Spec.IngressOverrides != nil {
			if ref := sm.Spec.IngressOverrides.CACertificateSecretRef; ref != nil && ref.Namespace != "" {
				set[ref.Namespace] = struct{}{}
			}
		}
	} else if !apierrors.IsNotFound(err) {
		return nil, err
	}

	return setToSortedSlice(set), nil
}

func setToSortedSlice(s map[string]struct{}) []string {
	out := make([]string, 0, len(s))
	for k := range s {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
