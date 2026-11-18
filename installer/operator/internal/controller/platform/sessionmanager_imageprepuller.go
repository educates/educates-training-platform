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
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	platformv1alpha1 "github.com/educates/educates-training-platform/installer/operator/api/platform/v1alpha1"
	"github.com/educates/educates-training-platform/installer/operator/internal/rollout"
)

// reportImagesPrePulled sets the ImagesPrePulled condition from the image
// pre-puller's DaemonSet, read uncached like the other workloads the
// reconciler reports on. A node's image-puller pod is available only once
// it has pulled every image, since each image is an init container, so
// the DaemonSet having rolled out means every image is pulled on every
// node it runs on. The condition never touches Ready or the phase, and a
// failed read leaves it Unknown and is logged rather than returned, so it
// never holds the reconcile back.
func (r *SessionManagerReconciler) reportImagesPrePulled(ctx context.Context, obj *platformv1alpha1.SessionManager) {
	if obj.Spec.ImagePrePuller == nil || !obj.Spec.ImagePrePuller.Enabled {
		r.markSMImagesPrePulled(obj, metav1.ConditionFalse, "PrePullerDisabled",
			"the image pre-puller is off, so no image is pulled ahead of time")
		return
	}
	ds := &appsv1.DaemonSet{}
	err := r.APIReader.Get(ctx, types.NamespacedName{Namespace: platformNamespace, Name: imagePullerDaemonSetName}, ds)
	switch {
	case apierrors.IsNotFound(err):
		r.markSMImagesPrePulled(obj, metav1.ConditionFalse, "Pulling",
			"the image-puller DaemonSet does not exist yet")
	case err != nil:
		logf.FromContext(ctx).Error(err, "read the image-puller DaemonSet")
		r.markSMImagesPrePulled(obj, metav1.ConditionUnknown, "DaemonSetUnreadable",
			fmt.Sprintf("get DaemonSet %s/%s: %v", platformNamespace, imagePullerDaemonSetName, err))
	case rollout.DaemonSetRolledOut(ds):
		r.markSMImagesPrePulled(obj, metav1.ConditionTrue, "Pulled",
			fmt.Sprintf("every image is pulled on each of the %d nodes the image-puller DaemonSet runs on",
				ds.Status.DesiredNumberScheduled))
	case rollout.DaemonSetScheduledNowhere(ds):
		r.markSMImagesPrePulled(obj, metav1.ConditionFalse, "NoEligibleNodes",
			"the image-puller DaemonSet runs on no node: every node carries a taint it does not tolerate")
	default:
		r.markSMImagesPrePulled(obj, metav1.ConditionFalse, "Pulling",
			fmt.Sprintf("the image-puller DaemonSet has not pulled every image on every node yet; "+
				"of its %d pods, %d run the current image list and %d are available",
				ds.Status.DesiredNumberScheduled, ds.Status.UpdatedNumberScheduled, ds.Status.NumberAvailable))
	}
}

func (r *SessionManagerReconciler) markSMImagesPrePulled(obj *platformv1alpha1.SessionManager, status metav1.ConditionStatus, reason, message string) {
	meta.SetStatusCondition(&obj.Status.Conditions, metav1.Condition{
		Type:               conditionImagesPrePulled,
		Status:             status,
		ObservedGeneration: obj.Generation,
		Reason:             reason,
		Message:            message,
	})
}
