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

package config

import (
	"context"
	"fmt"
	"maps"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	configv1alpha1 "github.com/educates/educates-training-platform/installer/operator/api/config/v1alpha1"
	"github.com/educates/educates-training-platform/installer/operator/internal/ownership"
)

// ensureNamespace creates the named namespace if absent and reconciles
// the operator-owned labels on it: the managed-by label, plus
// podSecurityLabels, the Pod Security admission level its workloads
// need, or nil to leave the namespace at the cluster's default level.
// Owner reference is set to the EducatesClusterConfig singleton
// (cluster-scoped → cluster-scoped is permitted), so `kubectl delete
// educatesclusterconfig cluster` cascades to the namespace even if the
// operator's finalizer drain path doesn't complete.
//
// Idempotent: on subsequent calls, missing operator-owned labels are
// added and changed ones restored via controller-runtime patch; labels
// the operator doesn't own are left alone.
func (r *EducatesClusterConfigReconciler) ensureNamespace(ctx context.Context, name string, owner *configv1alpha1.EducatesClusterConfig, podSecurityLabels map[string]string) error {
	desiredLabels := map[string]string{
		ownership.ManagedByLabel: ownership.ManagedByValue,
	}
	maps.Copy(desiredLabels, podSecurityLabels)

	ns := &corev1.Namespace{}
	err := r.Get(ctx, types.NamespacedName{Name: name}, ns)
	if apierrors.IsNotFound(err) {
		ns = &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name:   name,
				Labels: desiredLabels,
			},
		}
		if err := controllerutil.SetControllerReference(owner, ns, r.Scheme); err != nil {
			return fmt.Errorf("set owner reference on Namespace %q: %w", name, err)
		}
		if err := r.Create(ctx, ns); err != nil {
			return fmt.Errorf("create Namespace %q: %w", name, err)
		}
		return nil
	}
	if err != nil {
		return fmt.Errorf("get Namespace %q: %w", name, err)
	}

	patch := client.MergeFrom(ns.DeepCopy())
	if !ownership.RestoreLabels(ns, desiredLabels) {
		return nil
	}
	if err := r.Patch(ctx, ns, patch); err != nil {
		return fmt.Errorf("patch Namespace %q labels: %w", name, err)
	}
	return nil
}
