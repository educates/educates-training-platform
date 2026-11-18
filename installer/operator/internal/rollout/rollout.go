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

// Package rollout tells whether a workload a Helm release installs has
// rolled out its current spec, by the rules of `kubectl rollout status`.
// The reconcilers gate a component's or cluster service's readiness on
// it: Available=True, or every pod being ready, is not enough, since
// right after a Helm upgrade changes a workload's spec its status still
// describes the pods of the previous one.
//
// Callers pass an uncached reader. They read a workload straight after
// their own Helm upgrade wrote it, before the informer cache has
// necessarily seen that write, and the cached copy of the previous spec
// would look rolled out.
//
// A missing workload is not rolled out rather than an error: the Helm
// install may not have created it yet. Neither is a rollout past its
// progress deadline: the workload's controller keeps trying it, and the
// reconciler keeps reporting it until it completes.
package rollout

import (
	"context"

	appsv1 "k8s.io/api/apps/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// DeploymentComplete reports whether the Deployment namespace/name has
// rolled out its current spec: the deployment controller has observed
// the spec, every replica runs it, no replica of an earlier spec is
// left, and the updated replicas are available.
func DeploymentComplete(ctx context.Context, reader client.Reader, namespace, name string) (bool, error) {
	dep := &appsv1.Deployment{}
	if err := reader.Get(ctx, types.NamespacedName{Namespace: namespace, Name: name}, dep); err != nil {
		if apierrors.IsNotFound(err) {
			return false, nil
		}
		return false, err
	}
	if dep.Status.ObservedGeneration < dep.Generation {
		return false, nil
	}
	replicas := int32(1)
	if dep.Spec.Replicas != nil {
		replicas = *dep.Spec.Replicas
	}
	return dep.Status.UpdatedReplicas >= replicas &&
		dep.Status.Replicas <= dep.Status.UpdatedReplicas &&
		dep.Status.AvailableReplicas >= dep.Status.UpdatedReplicas, nil
}

// DaemonSetComplete reports whether the DaemonSet namespace/name has
// rolled out its current spec: the DaemonSet controller has observed
// the spec, and on every node that should run the DaemonSet, a pod of
// that spec is scheduled and available. A DaemonSet no node should run
// is not rolled out: on a fresh cluster whose nodes aren't ready yet,
// the DaemonSet hasn't scheduled anything.
func DaemonSetComplete(ctx context.Context, reader client.Reader, namespace, name string) (bool, error) {
	ds := &appsv1.DaemonSet{}
	if err := reader.Get(ctx, types.NamespacedName{Namespace: namespace, Name: name}, ds); err != nil {
		if apierrors.IsNotFound(err) {
			return false, nil
		}
		return false, err
	}
	if ds.Status.ObservedGeneration < ds.Generation {
		return false, nil
	}
	desired := ds.Status.DesiredNumberScheduled
	return desired > 0 &&
		ds.Status.UpdatedNumberScheduled >= desired &&
		ds.Status.NumberAvailable >= desired, nil
}
