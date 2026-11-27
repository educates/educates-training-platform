package cluster

import (
	"strings"
	"testing"

	"sigs.k8s.io/kind/pkg/apis/config/v1alpha4"
	"sigs.k8s.io/yaml"
)

// kubeadmPatch is the part of a node's kubeadm config patch the kind
// config template writes.
type kubeadmPatch struct {
	Kind             string `json:"kind"`
	NodeRegistration struct {
		KubeletExtraArgs map[string]string `json:"kubeletExtraArgs"`
	} `json:"nodeRegistration"`
}

// renderAndParseKindConfig renders input and parses the result as kind
// reads it, refusing fields kind's v1alpha4 Cluster does not have.
func renderAndParseKindConfig(t *testing.T, input *KindBootstrapInput) v1alpha4.Cluster {
	t.Helper()
	data, err := RenderKindConfig(input)
	if err != nil {
		t.Fatalf("RenderKindConfig: %v", err)
	}
	var cfg v1alpha4.Cluster
	if err := yaml.UnmarshalStrict(data, &cfg); err != nil {
		t.Fatalf("parse rendered kind config: %v\n%s", err, data)
	}
	return cfg
}

// nodePatch returns the single kubeadm config patch of node i of cfg.
func nodePatch(t *testing.T, cfg v1alpha4.Cluster, i int) kubeadmPatch {
	t.Helper()
	patches := cfg.Nodes[i].KubeadmConfigPatches
	if len(patches) != 1 {
		t.Fatalf("node %d has %d kubeadm config patches, want 1", i, len(patches))
	}
	var patch kubeadmPatch
	if err := yaml.UnmarshalStrict([]byte(patches[0]), &patch); err != nil {
		t.Fatalf("parse kubeadm config patch of node %d: %v\n%s", i, err, patches[0])
	}
	return patch
}

// wantKubeadmPatch checks that a kubeadm config patch is of the given
// kind and passes the kubelet the node labels and taints given, with an
// absent argument wanted as the empty string.
func wantKubeadmPatch(t *testing.T, i int, patch kubeadmPatch, kind, labels, taints string) {
	t.Helper()
	if patch.Kind != kind {
		t.Errorf("node %d patch kind = %q, want %q", i, patch.Kind, kind)
	}
	args := patch.NodeRegistration.KubeletExtraArgs
	if got := args["node-labels"]; got != labels {
		t.Errorf("node %d node-labels = %q, want %q", i, got, labels)
	}
	if got := args["register-with-taints"]; got != taints {
		t.Errorf("node %d register-with-taints = %q, want %q", i, got, taints)
	}
}

// wantPublishesNoPorts checks that node publishes no host ports: the
// router serves every local cluster's ports 80 and 443 and reaches the
// node over Docker's kind network.
func wantPublishesNoPorts(t *testing.T, i int, node v1alpha4.Node) {
	t.Helper()
	if len(node.ExtraPortMappings) != 0 {
		t.Errorf("node %d port mappings = %+v, want none", i, node.ExtraPortMappings)
	}
}

// TestRenderKindConfigOfTheDefaultCluster checks that an input declaring
// no nodes renders the default single control-plane node, labelled
// ingress-ready, publishing no host ports and mounting the volumes.
func TestRenderKindConfigOfTheDefaultCluster(t *testing.T) {
	cfg := renderAndParseKindConfig(t, &KindBootstrapInput{
		VolumeMounts: []KindVolumeMount{{HostPath: "/host/content", ContainerPath: "/content", HasReadOnly: true, ReadOnly: true}},
	})

	if len(cfg.Nodes) != 1 {
		t.Fatalf("got %d nodes, want 1", len(cfg.Nodes))
	}
	node := cfg.Nodes[0]
	if node.Role != v1alpha4.ControlPlaneRole {
		t.Errorf("node role = %q, want %q", node.Role, v1alpha4.ControlPlaneRole)
	}
	wantKubeadmPatch(t, 0, nodePatch(t, cfg, 0), "InitConfiguration", "ingress-ready=true", "")
	wantPublishesNoPorts(t, 0, node)
	wantMounts := []v1alpha4.Mount{{HostPath: "/host/content", ContainerPath: "/content", Readonly: true}}
	if len(node.ExtraMounts) != 1 || node.ExtraMounts[0] != wantMounts[0] {
		t.Errorf("node mounts = %+v, want %+v", node.ExtraMounts, wantMounts)
	}
}

// TestRenderKindConfigOfDeclaredNodes checks that a declared node list
// renders a node for each entry: the control-plane node with its labels
// and ingress-ready, no published ports and the mounts, and each worker
// with its labels and taints registered when it joins.
func TestRenderKindConfigOfDeclaredNodes(t *testing.T) {
	cfg := renderAndParseKindConfig(t, &KindBootstrapInput{
		VolumeMounts: []KindVolumeMount{{HostPath: "/host/content", ContainerPath: "/content"}},
		Nodes: []KindNode{
			{Role: "control-plane", Labels: map[string]string{"pool": "system"}},
			{
				Role:   "worker",
				Labels: map[string]string{"tier": "b", "pool": "workshops"},
				Taints: []KindNodeTaint{
					{Key: "workshops", Effect: "NoSchedule"},
					{Key: "dedicated", Value: "educates", Effect: "NoExecute"},
				},
			},
			{Role: "worker", Taints: []KindNodeTaint{{Key: "gpu", Value: "true", Effect: "PreferNoSchedule"}}},
			{Role: "worker"},
		},
	})

	wantRoles := []v1alpha4.NodeRole{v1alpha4.ControlPlaneRole, v1alpha4.WorkerRole, v1alpha4.WorkerRole, v1alpha4.WorkerRole}
	if len(cfg.Nodes) != len(wantRoles) {
		t.Fatalf("got %d nodes, want %d", len(cfg.Nodes), len(wantRoles))
	}
	for i, role := range wantRoles {
		if cfg.Nodes[i].Role != role {
			t.Errorf("node %d role = %q, want %q", i, cfg.Nodes[i].Role, role)
		}
	}

	wantKubeadmPatch(t, 0, nodePatch(t, cfg, 0), "InitConfiguration", "pool=system,ingress-ready=true", "")
	wantPublishesNoPorts(t, 0, cfg.Nodes[0])
	wantMount := v1alpha4.Mount{HostPath: "/host/content", ContainerPath: "/content"}
	if len(cfg.Nodes[0].ExtraMounts) != 1 || cfg.Nodes[0].ExtraMounts[0] != wantMount {
		t.Errorf("control-plane mounts = %+v, want [%+v]", cfg.Nodes[0].ExtraMounts, wantMount)
	}

	wantKubeadmPatch(t, 1, nodePatch(t, cfg, 1), "JoinConfiguration",
		"pool=workshops,tier=b", "workshops:NoSchedule,dedicated=educates:NoExecute")
	wantKubeadmPatch(t, 2, nodePatch(t, cfg, 2), "JoinConfiguration", "", "gpu=true:PreferNoSchedule")
	if patches := cfg.Nodes[3].KubeadmConfigPatches; len(patches) != 0 {
		t.Errorf("worker without labels or taints has kubeadm config patches %q, want none", patches)
	}

	for i := 1; i < len(cfg.Nodes); i++ {
		if len(cfg.Nodes[i].ExtraPortMappings) != 0 || len(cfg.Nodes[i].ExtraMounts) != 0 {
			t.Errorf("worker %d publishes ports %+v and mounts %+v, want neither",
				i, cfg.Nodes[i].ExtraPortMappings, cfg.Nodes[i].ExtraMounts)
		}
	}
}

// TestRenderKindConfigRefusesTaintsOnTheControlPlaneNode checks that taints
// on the control-plane node, which the kind config has no place for, fail
// the render instead of being left out of it.
func TestRenderKindConfigRefusesTaintsOnTheControlPlaneNode(t *testing.T) {
	_, err := RenderKindConfig(&KindBootstrapInput{
		Nodes: []KindNode{
			{Role: "control-plane", Taints: []KindNodeTaint{{Key: "workshops", Effect: "NoSchedule"}}},
			{Role: "worker"},
		},
	})
	if err == nil {
		t.Fatal("RenderKindConfig: got no error, want one refusing the control-plane node's taints")
	}
	for _, part := range []string{`role "control-plane"`, "taints"} {
		if !strings.Contains(err.Error(), part) {
			t.Errorf("error %q does not mention %q", err, part)
		}
	}
}
