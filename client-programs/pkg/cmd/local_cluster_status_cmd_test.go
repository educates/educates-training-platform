package cmd

import (
	"slices"
	"testing"

	apiv1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// statusNode is a node of a kind cluster as its API server reports it,
// with the labels the kubelet puts on every node besides labels, and the
// Ready condition ready.
func statusNode(name string, labels map[string]string, ready apiv1.ConditionStatus, taints ...apiv1.Taint) apiv1.Node {
	all := map[string]string{
		"beta.kubernetes.io/arch": "arm64",
		"beta.kubernetes.io/os":   "linux",
		"kubernetes.io/arch":      "arm64",
		"kubernetes.io/hostname":  name,
		"kubernetes.io/os":        "linux",
	}
	for k, v := range labels {
		all[k] = v
	}
	return apiv1.Node{
		ObjectMeta: metav1.ObjectMeta{Name: name, Labels: all},
		Spec:       apiv1.NodeSpec{Taints: taints},
		Status: apiv1.NodeStatus{
			Conditions: []apiv1.NodeCondition{
				{Type: apiv1.NodeMemoryPressure, Status: apiv1.ConditionFalse},
				{Type: apiv1.NodeReady, Status: ready},
			},
			NodeInfo: apiv1.NodeSystemInfo{KubeletVersion: "v1.36.0"},
		},
	}
}

func TestLocalClusterNodeRowsOfAMultiNodeCluster(t *testing.T) {
	nodes := []apiv1.Node{
		statusNode("educates-control-plane", map[string]string{
			"ingress-ready":                                           "true",
			"node-role.kubernetes.io/control-plane":                   "",
			"node.kubernetes.io/exclude-from-external-load-balancers": "",
			"tier": "control",
		}, apiv1.ConditionTrue, apiv1.Taint{Key: "node-role.kubernetes.io/control-plane", Effect: apiv1.TaintEffectNoSchedule}),
		statusNode("educates-worker", map[string]string{"tier": "frontend", "disktype": "ssd"}, apiv1.ConditionFalse,
			apiv1.Taint{Key: "dedicated", Value: "gpu", Effect: apiv1.TaintEffectNoExecute}),
		statusNode("educates-worker2", nil, apiv1.ConditionUnknown),
	}

	rows := localClusterNodeRows(nodes, false)

	want := [][]string{
		{"educates-control-plane", "control-plane", "Ready", "v1.36.0", "ingress-ready=true,tier=control",
			"node-role.kubernetes.io/control-plane:NoSchedule"},
		{"educates-worker", "worker", "NotReady", "v1.36.0", "disktype=ssd,tier=frontend", "dedicated=gpu:NoExecute"},
		{"educates-worker2", "worker", "NotReady", "v1.36.0", "-", "-"},
	}
	if len(rows) != len(want) {
		t.Fatalf("got %d rows, want %d: %q", len(rows), len(want), rows)
	}
	for i := range want {
		if !slices.Equal(rows[i], want[i]) {
			t.Errorf("row %d = %q, want %q", i, rows[i], want[i])
		}
		if len(rows[i]) != len(localClusterNodeColumns) {
			t.Errorf("row %d has %d cells, want one for each of %q", i, len(rows[i]), localClusterNodeColumns)
		}
	}
}

func TestLocalClusterNodeRowsWithAllLabels(t *testing.T) {
	nodes := []apiv1.Node{
		statusNode("educates-control-plane", map[string]string{
			"ingress-ready":                                           "true",
			"node-role.kubernetes.io/control-plane":                   "",
			"node.kubernetes.io/exclude-from-external-load-balancers": "",
		}, apiv1.ConditionTrue),
	}

	rows := localClusterNodeRows(nodes, true)

	want := "beta.kubernetes.io/arch=arm64,beta.kubernetes.io/os=linux,ingress-ready=true," +
		"kubernetes.io/arch=arm64,kubernetes.io/hostname=educates-control-plane,kubernetes.io/os=linux," +
		"node-role.kubernetes.io/control-plane=,node.kubernetes.io/exclude-from-external-load-balancers="
	if len(rows) != 1 || rows[0][4] != want {
		t.Errorf("rows = %q, want one row whose labels are every label of the node, sorted: %q", rows, want)
	}
}

func TestLocalClusterNodeStatus(t *testing.T) {
	cordoned := statusNode("educates-worker", nil, apiv1.ConditionTrue)
	cordoned.Spec.Unschedulable = true
	noCondition := statusNode("educates-worker2", nil, apiv1.ConditionTrue)
	noCondition.Status.Conditions = nil

	for _, tc := range []struct {
		node apiv1.Node
		want string
	}{
		{cordoned, "Ready,SchedulingDisabled"},
		{noCondition, "Unknown"},
	} {
		if got := nodeStatus(tc.node); got != tc.want {
			t.Errorf("nodeStatus(%s) = %q, want %q", tc.node.Name, got, tc.want)
		}
	}
}
