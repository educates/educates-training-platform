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

package rollout

import (
	"context"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
)

const (
	testNamespace = "contour"
	testDaemonSet = "contour-envoy"
)

// daemonSet is a DaemonSet at generation 2 that should run on two nodes,
// with updated pods of its current spec and available pods, its spec
// observed.
func daemonSet(updated, available int32) *appsv1.DaemonSet {
	return &appsv1.DaemonSet{
		ObjectMeta: metav1.ObjectMeta{
			Name: testDaemonSet, Namespace: testNamespace,
			UID: types.UID("daemonset-uid"), Generation: 2,
		},
		Spec: appsv1.DaemonSetSpec{
			Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "envoy"}},
		},
		Status: appsv1.DaemonSetStatus{
			ObservedGeneration:     2,
			DesiredNumberScheduled: 2,
			CurrentNumberScheduled: 2,
			UpdatedNumberScheduled: updated,
			NumberReady:            available,
			NumberAvailable:        available,
		},
	}
}

// pod is a pod of ds on nodeName, ready since an hour ago or not ready.
func pod(ds *appsv1.DaemonSet, name, nodeName string, ready bool) *corev1.Pod {
	status := corev1.ConditionFalse
	if ready {
		status = corev1.ConditionTrue
	}
	controller := true
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name: name, Namespace: testNamespace,
			Labels: map[string]string{"app": "envoy"},
			OwnerReferences: []metav1.OwnerReference{{
				APIVersion: "apps/v1", Kind: "DaemonSet",
				Name: ds.Name, UID: ds.UID, Controller: &controller,
			}},
		},
		Spec: corev1.PodSpec{NodeName: nodeName},
		Status: corev1.PodStatus{
			Conditions: []corev1.PodCondition{{
				Type: corev1.PodReady, Status: status,
				LastTransitionTime: metav1.NewTime(time.Now().Add(-time.Hour)),
			}},
		},
	}
}

// node is a node whose Ready condition is ready.
func node(name string, ready bool) *corev1.Node {
	status := corev1.ConditionFalse
	if ready {
		status = corev1.ConditionTrue
	}
	return &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Status: corev1.NodeStatus{
			Conditions: []corev1.NodeCondition{{Type: corev1.NodeReady, Status: status}},
		},
	}
}

// clientBuilder starts a fake client serving objs as the cluster's state.
func clientBuilder(t *testing.T, objs ...client.Object) *fake.ClientBuilder {
	t.Helper()
	scheme := runtime.NewScheme()
	if err := clientgoscheme.AddToScheme(scheme); err != nil {
		t.Fatalf("scheme: %v", err)
	}
	return fake.NewClientBuilder().WithScheme(scheme).WithObjects(objs...)
}

// complete runs DaemonSetCompleteOnReadyNodes against a cluster holding
// objs and fails the test on an error.
func complete(t *testing.T, objs ...client.Object) bool {
	t.Helper()
	return completeWith(t, clientBuilder(t, objs...).Build())
}

func completeWith(t *testing.T, reader client.Reader) bool {
	t.Helper()
	done, err := DaemonSetCompleteOnReadyNodes(context.Background(), reader, testNamespace, testDaemonSet)
	if err != nil {
		t.Fatalf("DaemonSetCompleteOnReadyNodes: %v", err)
	}
	return done
}

func TestDaemonSetCompleteOnReadyNodes_IgnoresAnUnavailablePodOnANodeThatIsNotReady(t *testing.T) {
	ds := daemonSet(2, 1)
	if !complete(t, ds,
		node("ready", true), pod(ds, "envoy-a", "ready", true),
		node("not-ready", false), pod(ds, "envoy-b", "not-ready", false),
	) {
		t.Error("not rolled out, want rolled out: the only unavailable pod is on a node that is not Ready")
	}
}

func TestDaemonSetCompleteOnReadyNodes_HoldsForAnUnavailablePodOnAReadyNode(t *testing.T) {
	ds := daemonSet(2, 1)
	if complete(t, ds,
		node("ready-a", true), pod(ds, "envoy-a", "ready-a", true),
		node("ready-b", true), pod(ds, "envoy-b", "ready-b", false),
	) {
		t.Error("rolled out, want not: a Ready node's pod is unavailable")
	}
}

func TestDaemonSetCompleteOnReadyNodes_HoldsForAReadyNodesPodWhenANodeThatIsNotReadyLeftTheDesiredCount(t *testing.T) {
	// The node that is not Ready no longer counts towards the two nodes
	// the DaemonSet should run on, but its pod still runs there.
	ds := daemonSet(2, 1)
	if complete(t, ds,
		node("ready-a", true), pod(ds, "envoy-a", "ready-a", true),
		node("ready-b", true), pod(ds, "envoy-b", "ready-b", false),
		node("not-ready", false), pod(ds, "envoy-c", "not-ready", false),
	) {
		t.Error("rolled out, want not: a Ready node's pod is unavailable, whatever the node that is not Ready runs")
	}
}

func TestDaemonSetCompleteOnReadyNodes_IgnoresAnUnavailablePodWhoseNodeIsGone(t *testing.T) {
	ds := daemonSet(2, 1)
	if !complete(t, ds,
		node("ready", true), pod(ds, "envoy-a", "ready", true),
		pod(ds, "envoy-b", "deleted-node", false),
	) {
		t.Error("not rolled out, want rolled out: the unavailable pod's node no longer exists")
	}
}

func TestDaemonSetCompleteOnReadyNodes_HoldsForAPodNotYetOnANode(t *testing.T) {
	ds := daemonSet(2, 1)
	if complete(t, ds,
		node("ready", true), pod(ds, "envoy-a", "ready", true),
		pod(ds, "envoy-b", "", false),
	) {
		t.Error("rolled out, want not: a pod is still pending, on no node yet")
	}
}

func TestDaemonSetCompleteOnReadyNodes_HoldsForAPodReadyForLessThanMinReadySeconds(t *testing.T) {
	ds := daemonSet(2, 1)
	ds.Spec.MinReadySeconds = 60
	justReady := pod(ds, "envoy-b", "ready-b", true)
	justReady.Status.Conditions[0].LastTransitionTime = metav1.Now()
	if complete(t, ds,
		node("ready-a", true), pod(ds, "envoy-a", "ready-a", true),
		node("ready-b", true), justReady,
	) {
		t.Error("rolled out, want not: a Ready node's pod has been Ready for less than minReadySeconds")
	}
}

func TestDaemonSetCompleteOnReadyNodes_HoldsUntilEveryNodeRunsTheCurrentSpec(t *testing.T) {
	// The node that is not Ready still runs a pod of the previous spec.
	ds := daemonSet(1, 1)
	if complete(t, ds,
		node("ready", true), pod(ds, "envoy-a", "ready", true),
		node("not-ready", false), pod(ds, "envoy-b", "not-ready", false),
	) {
		t.Error("rolled out, want not: one node has no pod of the current spec")
	}
}

func TestDaemonSetCompleteOnReadyNodes_IgnoresPodsItDoesNotControlOrIsDeleting(t *testing.T) {
	ds := daemonSet(2, 1)
	stray := pod(ds, "stray", "ready-a", false)
	stray.OwnerReferences = nil
	deleting := pod(ds, "envoy-old", "ready-a", false)
	now := metav1.Now()
	deleting.DeletionTimestamp = &now
	deleting.Finalizers = []string{"test/keep"}
	if !complete(t, ds,
		node("ready-a", true), pod(ds, "envoy-a", "ready-a", true), stray, deleting,
		node("not-ready", false), pod(ds, "envoy-b", "not-ready", false),
	) {
		t.Error("not rolled out, want rolled out: the unavailable pods on the Ready node are not the DaemonSet's own, or are being deleted")
	}
}

func TestDaemonSetCompleteOnReadyNodes_ReadsNoPodsWhenEveryPodIsAvailable(t *testing.T) {
	ds := daemonSet(2, 2)
	r := clientBuilder(t, ds).WithInterceptorFuncs(interceptor.Funcs{
		List: func(context.Context, client.WithWatch, client.ObjectList, ...client.ListOption) error {
			t.Error("listed pods of a DaemonSet whose every pod is available")
			return nil
		},
	}).Build()
	if !completeWith(t, r) {
		t.Error("not rolled out, want rolled out: every pod is available")
	}
}
