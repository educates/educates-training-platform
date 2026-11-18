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

package platform

import (
	"context"

	appsv1 "k8s.io/api/apps/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// deploymentRolledOut reports whether the Deployment namespace/name has
// rolled out its current spec, as `kubectl rollout status` judges it:
// the deployment controller has observed the spec, every replica runs
// it, no replica of an earlier spec is left, and the updated replicas
// are available. Available=True alone isn't enough: right after a Helm
// upgrade changes the spec, it still describes the replicas of the
// previous one.
//
// reader must be uncached. The reconciler reads the Deployment straight
// after its own Helm upgrade wrote it, before the informer cache has
// necessarily seen that write, and the cached copy of the previous spec
// would look rolled out.
//
// A missing Deployment is not rolled out rather than an error: the Helm
// install may not have created it yet, or a deletion replay is in
// progress. Neither is a rollout past its progress deadline: the
// deployment controller keeps trying it, and the reconciler keeps
// reporting it until it completes.
func deploymentRolledOut(ctx context.Context, reader client.Reader, namespace, name string) (bool, error) {
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
