package cluster

import (
	"bytes"
	_ "embed"
	"html/template"
	"sort"
	"strings"

	"github.com/pkg/errors"

	"github.com/educates/educates-training-platform/client-programs/pkg/localclusters"
)

//go:embed kindclusterconfig.yaml.tpl
var kindConfigTemplateData string

// RenderKindConfig renders the kind cluster config of input, the file
// kind creates the local cluster's kind cluster from. It refuses taints on
// the control-plane node, which the kind config registers only on worker
// nodes, rather than leave them out.
func RenderKindConfig(input *KindBootstrapInput) ([]byte, error) {
	for _, n := range input.Nodes {
		if n.Role == localclusters.ControlPlaneRole && len(n.Taints) > 0 {
			return nil, errors.Errorf("the kind config registers taints only on worker nodes, "+
				"so the node with role %q can't have taints", localclusters.ControlPlaneRole)
		}
	}

	kindConfigTemplate, err := template.New("kind-cluster-config").Parse(kindConfigTemplateData)
	if err != nil {
		return nil, errors.Wrap(err, "failed to parse cluster config template")
	}

	var kindConfig bytes.Buffer
	if err := kindConfigTemplate.Execute(&kindConfig, input); err != nil {
		return nil, errors.Wrap(err, "failed to generate cluster config")
	}
	return kindConfig.Bytes(), nil
}

// KindBootstrapInput is the focused payload the kind-cluster template
// reads from. Decouples cluster-bootstrap rendering from any specific
// CLI config kind: 'local cluster create' builds one from
// EducatesLocalConfig; future scenario kinds (GKE/EKS — though they
// would not use kind) or test fixtures build it directly.
//
// No node publishes a port on the host: the router reaches each cluster's
// ingress on its control-plane node over the kind network.
type KindBootstrapInput struct {
	ApiServer    KindApiServer
	Networking   KindNetworking
	VolumeMounts []KindVolumeMount
	// Nodes, when non-empty, fully describes the cluster's nodes (the
	// template renders a node per entry). Empty keeps the default single
	// control-plane node.
	Nodes []KindNode
}

// KindNode is one node in a multi-node kind cluster. Labels become
// Kubernetes node labels; Taints are registered on worker nodes at join
// time, and RenderKindConfig refuses them on the control-plane node. The
// control-plane node additionally carries the ingress-ready label and the
// volume mounts.
type KindNode struct {
	Role   string
	Labels map[string]string
	Taints []KindNodeTaint
}

type KindNodeTaint struct {
	Key    string
	Value  string
	Effect string
}

// LabelsCSV renders the node's labels as a comma-separated key=value list in
// sorted key order, or the empty string when there are none. Used by the
// kind config template's kubelet node-labels argument.
func (n KindNode) LabelsCSV() string {
	keys := make([]string, 0, len(n.Labels))
	for k := range n.Labels {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, k+"="+n.Labels[k])
	}
	return strings.Join(parts, ",")
}

// TaintsCSV renders the node's taints as a comma-separated
// key=value:effect (or key:effect when no value) list, or the empty string
// when there are none. Used by the kubelet register-with-taints argument.
func (n KindNode) TaintsCSV() string {
	parts := make([]string, 0, len(n.Taints))
	for _, t := range n.Taints {
		s := t.Key
		if t.Value != "" {
			s += "=" + t.Value
		}
		s += ":" + t.Effect
		parts = append(parts, s)
	}
	return strings.Join(parts, ",")
}

type KindApiServer struct {
	Address string
	Port    int
}

type KindNetworking struct {
	ServiceSubnet string
	PodSubnet     string
}

type KindVolumeMount struct {
	HostPath      string
	ContainerPath string
	// HasReadOnly + ReadOnly map to kind's extraMounts[].readOnly.
	// The pair models a nullable bool without requiring text/template
	// pointer dereference: HasReadOnly=false leaves the field unset
	// (kind defaults to read-write); HasReadOnly=true emits the
	// explicit ReadOnly value into the template.
	HasReadOnly bool
	ReadOnly    bool
}
