// Package localclusters holds the rules about the local kind clusters the
// CLI manages: which local cluster a command means, what that cluster is
// called in kind, in Docker, in the kubeconfig and in the data home, what
// a named local cluster's config must declare, and, from an observed
// state of the host, which Educates clusters exist, whether they clash
// with one another, and what the local DNS resolver serves for them.
//
// It takes everything as plain values, the data home and the observed
// state included. Besides the cluster configs and folders of the data
// home, it reads neither files, the environment nor Docker, so callers
// decide where the inputs come from.
package localclusters

import (
	"fmt"
	"path/filepath"
	"regexp"

	"github.com/educates/educates-training-platform/client-programs/pkg/config"
)

const (
	// defaultKindName is the kind cluster name of the default local
	// cluster, and the prefix of every named local cluster's.
	defaultKindName = "educates"

	// kindConfigFileName is the name of the kind config file a local
	// cluster is created from, in the folder that holds the cluster's
	// config.
	kindConfigFileName = "educates-cluster-config.yaml"

	// Node roles as kind names them.
	controlPlaneRole         = "control-plane"
	externalLoadBalancerRole = "external-load-balancer"
)

// MaxNameLength is the longest local cluster name. It keeps every node
// container name kind derives from educates-<name> within a 63-character
// hostname, the longest being the external load balancer's.
const MaxNameLength = 63 - len(defaultKindName+"-") - len("-"+externalLoadBalancerRole)

// namePattern matches a lowercase DNS label.
var namePattern = regexp.MustCompile(`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`)

// ValidateName checks that name is a valid local cluster name: a lowercase
// DNS label of at most MaxNameLength characters. The error states the
// rules.
func ValidateName(name string) error {
	if len(name) <= MaxNameLength && namePattern.MatchString(name) {
		return nil
	}
	return fmt.Errorf("invalid local cluster name %q: a local cluster name is a lowercase DNS label "+
		"of 1 to %d characters, made of lowercase letters, digits and '-', "+
		"starting and ending with a letter or digit", name, MaxNameLength)
}

// Identity is what a local cluster is called everywhere.
type Identity struct {
	// Name is the local cluster name, empty for the default local
	// cluster.
	Name string

	// KindName is the name of the kind cluster.
	KindName string

	// Context is the kube context kind writes for the cluster.
	Context string

	// ConfigPath is the path of the cluster's CLI config.
	ConfigPath string

	// KindConfigPath is the path of the kind config file the cluster is
	// created from.
	KindConfigPath string
}

// Default returns the identity of the default local cluster of the data
// home dataHome: kind cluster educates, with its config and kind config
// file at the top of the data home.
func Default(dataHome string) Identity {
	return Identity{
		KindName:       defaultKindName,
		Context:        "kind-" + defaultKindName,
		ConfigPath:     config.LocalConfigPath(dataHome),
		KindConfigPath: filepath.Join(dataHome, kindConfigFileName),
	}
}

// Named returns the identity of the named local cluster name of the data
// home dataHome: kind cluster educates-<name>, with its config and kind
// config file in clusters/<name>/ of the data home. It returns an error
// naming the rules when name is not a valid local cluster name.
func Named(name, dataHome string) (Identity, error) {
	if err := ValidateName(name); err != nil {
		return Identity{}, err
	}
	kindName := defaultKindName + "-" + name
	dir := filepath.Join(dataHome, "clusters", name)
	return Identity{
		Name:           name,
		KindName:       kindName,
		Context:        "kind-" + kindName,
		ConfigPath:     filepath.Join(dir, "config.yaml"),
		KindConfigPath: filepath.Join(dir, kindConfigFileName),
	}, nil
}

// IsDefault reports whether the identity is the default local cluster's.
func (id Identity) IsDefault() bool {
	return id.Name == ""
}

// Description names the local cluster in a message: the default local
// cluster, or local cluster "<name>".
func (id Identity) Description() string {
	if id.IsDefault() {
		return "the default local cluster"
	}
	return fmt.Sprintf("local cluster %q", id.Name)
}

// ControlPlaneNode is the name of the node container kind creates for the
// cluster's first control-plane node.
func (id Identity) ControlPlaneNode() string {
	return id.KindName + "-" + controlPlaneRole
}

// NodeNames returns the names of the node containers kind creates for the
// cluster from the node roles its config declares, in kind's order. No
// roles means kind's default single control-plane node.
//
// kind names a node <kind-name>-<role>, adding a counter from 2 for each
// further node of the same role, and adds an external load balancer
// container, last, to a cluster with several control-plane nodes. The
// load balancer runs no kubelet.
func (id Identity) NodeNames(roles []string) []string {
	if len(roles) == 0 {
		roles = []string{controlPlaneRole}
	}
	counts := map[string]int{}
	name := func(role string) string {
		counts[role]++
		if n := counts[role]; n > 1 {
			return fmt.Sprintf("%s-%s%d", id.KindName, role, n)
		}
		return id.KindName + "-" + role
	}

	names := make([]string, 0, len(roles)+1)
	for _, role := range roles {
		names = append(names, name(role))
	}
	if counts[controlPlaneRole] > 1 {
		names = append(names, name(externalLoadBalancerRole))
	}
	return names
}
