/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

// Package platformnamespace names the namespace the operator installs
// the Educates runtime components into, and creates it. The platform
// reconcilers install their charts there, and the EducatesClusterConfig
// reconciler copies the image pull Secrets there before any of them
// does, so both create it the same way.
package platformnamespace

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/educates/educates-training-platform/installer/operator/internal/ownership"
	"github.com/educates/educates-training-platform/installer/operator/internal/podsecurity"
)

// Name is the shared platform namespace. Mirrors v3 behavior: the
// umbrella `educates-training-platform` Helm chart has historically been
// `helm install -n educates`, and the runtime components share it.
const Name = "educates"

// Ensure creates the shared platform namespace if absent and reconciles
// the operator-owned labels on it: the managed-by label and a privileged
// Pod Security admission level. Idempotent and ownerless on purpose:
// three platform CRs share the namespace, so setting any one of them as
// the owner would let a CR delete cascade-drop the namespace and break
// its siblings. Cleanup of the namespace itself is left to operator
// uninstall.
//
// The namespace is privileged because node-ca-injector, which
// SessionManager installs whenever an ingress CA is configured, runs a
// privileged container with a hostPath volume, which the baseline level
// forbids. On an existing namespace, missing operator-owned labels are
// added and changed ones restored; labels the operator doesn't own are
// left alone.
//
// The helm SDK's `CreateNamespace: true` install flag would handle
// the create-on-install case, but only on first install; concurrent
// reconciles racing to install three platform components in the same
// namespace can still produce a "namespace not found" if Helm checks
// existence before its own create-namespace handler fires. Doing the
// create ourselves before each install removes the race.
func Ensure(ctx context.Context, c client.Client) error {
	desiredLabels := podsecurity.PrivilegedNamespaceLabels()
	desiredLabels[ownership.ManagedByLabel] = ownership.ManagedByValue

	ns := &corev1.Namespace{}
	err := c.Get(ctx, types.NamespacedName{Name: Name}, ns)
	if apierrors.IsNotFound(err) {
		ns = &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name:   Name,
				Labels: desiredLabels,
			},
		}
		if createErr := c.Create(ctx, ns); createErr != nil && !apierrors.IsAlreadyExists(createErr) {
			return fmt.Errorf("create Namespace %q: %w", Name, createErr)
		}
		return nil
	}
	if err != nil {
		return fmt.Errorf("get Namespace %q: %w", Name, err)
	}

	patch := client.MergeFrom(ns.DeepCopy())
	if !ownership.RestoreLabels(ns, desiredLabels) {
		return nil
	}
	if err := c.Patch(ctx, ns, patch); err != nil {
		return fmt.Errorf("patch Namespace %q labels: %w", Name, err)
	}
	return nil
}
