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
// describes the pods of the previous one. DaemonSetCompleteOnReadyNodes
// relaxes the DaemonSet rule for the pods on nodes that are not Ready.
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
	"fmt"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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

// DaemonSetCompleteOnReadyNodes reports whether the DaemonSet
// namespace/name has rolled out its current spec on every node that is
// Ready: DaemonSetRolledOut, with one difference. A pod that is not
// available because its node is not Ready, or no longer exists, does not
// count against the rollout, since such a node runs no new workload pods.
// Every node the DaemonSet should run on must still run a pod of the
// current spec: during an upgrade the DaemonSet controller cannot replace
// the pod on a node that is not Ready, and with the default
// maxUnavailable it then stops replacing them on the others too.
//
// Only a DaemonSet with an unavailable pod costs more than one read: its
// pods are then listed, and the nodes of the unavailable ones read.
func DaemonSetCompleteOnReadyNodes(ctx context.Context, reader client.Reader, namespace, name string) (bool, error) {
	ds := &appsv1.DaemonSet{}
	if err := reader.Get(ctx, types.NamespacedName{Namespace: namespace, Name: name}, ds); err != nil {
		if apierrors.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("get DaemonSet %s/%s: %w", namespace, name, err)
	}
	if DaemonSetRolledOut(ds) {
		return true, nil
	}
	desired := ds.Status.DesiredNumberScheduled
	if ds.Status.ObservedGeneration < ds.Generation || desired == 0 || ds.Status.UpdatedNumberScheduled < desired {
		return false, nil
	}
	down, err := podDownOnReadyNode(ctx, reader, ds)
	if err != nil {
		return false, err
	}
	return !down, nil
}

// podDownOnReadyNode reports whether a pod of ds that is not available
// sits on a node that is Ready, or on no node yet. Pods the DaemonSet does
// not control, and pods being deleted, are left out: the DaemonSet
// replaces a pod it deletes, and UpdatedNumberScheduled already says
// whether every node has its replacement.
func podDownOnReadyNode(ctx context.Context, reader client.Reader, ds *appsv1.DaemonSet) (bool, error) {
	selector, err := metav1.LabelSelectorAsSelector(ds.Spec.Selector)
	if err != nil {
		return false, fmt.Errorf("DaemonSet %s/%s selector: %w", ds.Namespace, ds.Name, err)
	}
	pods := &corev1.PodList{}
	if err := reader.List(ctx, pods, client.InNamespace(ds.Namespace), client.MatchingLabelsSelector{Selector: selector}); err != nil {
		return false, fmt.Errorf("list pods of DaemonSet %s/%s: %w", ds.Namespace, ds.Name, err)
	}
	now := time.Now()
	readyNodes := map[string]bool{}
	for i := range pods.Items {
		pod := &pods.Items[i]
		if !metav1.IsControlledBy(pod, ds) || !pod.DeletionTimestamp.IsZero() || podAvailable(pod, ds.Spec.MinReadySeconds, now) {
			continue
		}
		if pod.Spec.NodeName == "" {
			return true, nil
		}
		ready, seen := readyNodes[pod.Spec.NodeName]
		if !seen {
			if ready, err = nodeReady(ctx, reader, pod.Spec.NodeName); err != nil {
				return false, err
			}
			readyNodes[pod.Spec.NodeName] = ready
		}
		if ready {
			return true, nil
		}
	}
	return false, nil
}

// podAvailable reports whether pod has been Ready for minReadySeconds, as
// the DaemonSet controller counts an available pod.
func podAvailable(pod *corev1.Pod, minReadySeconds int32, now time.Time) bool {
	for _, c := range pod.Status.Conditions {
		if c.Type != corev1.PodReady {
			continue
		}
		if c.Status != corev1.ConditionTrue {
			return false
		}
		return minReadySeconds == 0 ||
			(!c.LastTransitionTime.IsZero() && !c.LastTransitionTime.Add(time.Duration(minReadySeconds)*time.Second).After(now))
	}
	return false
}

// nodeReady reports whether the node name exists and is Ready.
func nodeReady(ctx context.Context, reader client.Reader, name string) (bool, error) {
	node := &corev1.Node{}
	if err := reader.Get(ctx, types.NamespacedName{Name: name}, node); err != nil {
		if apierrors.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("get node %s: %w", name, err)
	}
	return NodeReady(node), nil
}

// NodeReady reports whether node's Ready condition is True. A node with no
// Ready condition is not Ready.
func NodeReady(node *corev1.Node) bool {
	for _, c := range node.Status.Conditions {
		if c.Type == corev1.NodeReady {
			return c.Status == corev1.ConditionTrue
		}
	}
	return false
}

// DaemonSetScheduledNowhere reports whether ds, its current spec observed,
// should run on no node, as when every node carries a taint it does not
// tolerate.
func DaemonSetScheduledNowhere(ds *appsv1.DaemonSet) bool {
	return ds.Status.ObservedGeneration >= ds.Generation && ds.Status.DesiredNumberScheduled == 0
}

// DaemonSetRolledOut reports whether ds has rolled out its current spec:
// the DaemonSet controller has observed the spec, and on every node that
// should run the DaemonSet, a pod of that spec is scheduled and
// available. A DaemonSet no node should run is not rolled out: on a fresh
// cluster whose nodes aren't ready yet, the DaemonSet hasn't scheduled
// anything.
func DaemonSetRolledOut(ds *appsv1.DaemonSet) bool {
	if ds.Status.ObservedGeneration < ds.Generation {
		return false
	}
	desired := ds.Status.DesiredNumberScheduled
	return desired > 0 &&
		ds.Status.UpdatedNumberScheduled >= desired &&
		ds.Status.NumberAvailable >= desired
}
