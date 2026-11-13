package localclusters

import (
	"cmp"
	"slices"
	"strconv"
	"strings"
)

const workerRole = "worker"

// NodeContainer is a node container of a kind cluster as Docker reports
// it.
type NodeContainer struct {
	// Name is the container name.
	Name string

	// KindCluster is the name of the kind cluster the container belongs
	// to.
	KindCluster string

	// Role is the node's kind role: control-plane, worker or
	// external-load-balancer.
	Role string

	// Running reports whether the container is running.
	Running bool
}

// State is whether a local cluster's node containers run, as local
// cluster status reports it.
type State string

const (
	// Running is a cluster whose node containers all run.
	Running State = "running"

	// Stopped is a cluster none of whose node containers runs.
	Stopped State = "stopped"

	// PartlyRunning is a cluster only some of whose node containers run.
	PartlyRunning State = "partly running"
)

// State returns the state of the cluster from its node containers among
// the observed ones, the external load balancer included. A cluster with
// no node containers is stopped.
func (id Identity) State(observed []NodeContainer) State {
	running, stopped := 0, 0
	for _, n := range id.nodes(observed) {
		if n.Running {
			running++
		} else {
			stopped++
		}
	}
	switch {
	case running == 0:
		return Stopped
	case stopped == 0:
		return Running
	default:
		return PartlyRunning
	}
}

// RegistryNodes returns the names of the cluster's node containers, among
// the observed ones, that get the containerd host config of the local
// registry and the registry mirrors, in kind's order, whether they run or
// not. These are every node that runs a kubelet, so every node but the
// external load balancer of a cluster with several control planes.
func (id Identity) RegistryNodes(observed []NodeContainer) []string {
	var names []string
	for _, n := range id.nodes(observed) {
		if n.Role != externalLoadBalancerRole {
			names = append(names, n.Name)
		}
	}
	return names
}

// NodesToStart returns the names of the cluster's node containers, among
// the observed ones, that are not running, in the order to start them:
// kind's order, control-plane nodes first. The external load balancer of a
// cluster with several control planes is among them, since the nodes reach
// the API server through it.
func (id Identity) NodesToStart(observed []NodeContainer) []string {
	var names []string
	for _, n := range id.nodes(observed) {
		if !n.Running {
			names = append(names, n.Name)
		}
	}
	return names
}

// NodesToStop returns the names of the cluster's node containers, among
// the observed ones, that are running, in the order to stop them: the
// reverse of the start order, so the control-plane nodes stop last.
func (id Identity) NodesToStop(observed []NodeContainer) []string {
	var names []string
	for _, n := range slices.Backward(id.nodes(observed)) {
		if n.Running {
			names = append(names, n.Name)
		}
	}
	return names
}

// nodes returns the cluster's node containers among the observed ones in
// kind's order: control-plane nodes, then workers, then the external load
// balancer, each role by the counter kind gives its nodes.
func (id Identity) nodes(observed []NodeContainer) []NodeContainer {
	var nodes []NodeContainer
	for _, n := range observed {
		if n.KindCluster == id.KindName {
			nodes = append(nodes, n)
		}
	}
	slices.SortStableFunc(nodes, func(a, b NodeContainer) int {
		return cmp.Or(
			cmp.Compare(roleRank(a.Role), roleRank(b.Role)),
			cmp.Compare(id.nodeCounter(a), id.nodeCounter(b)),
			cmp.Compare(a.Name, b.Name),
		)
	})
	return nodes
}

// roleRank orders node roles the way kind lists them.
func roleRank(role string) int {
	switch role {
	case controlPlaneRole:
		return 0
	case workerRole:
		return 1
	case externalLoadBalancerRole:
		return 2
	default:
		return 3
	}
}

// nodeCounter is the counter kind puts after <kind-name>-<role> in a node
// container's name: 1 for the first node of a role, which has none.
func (id Identity) nodeCounter(n NodeContainer) int {
	suffix, ok := strings.CutPrefix(n.Name, id.KindName+"-"+n.Role)
	if !ok {
		return 0
	}
	if suffix == "" {
		return 1
	}
	counter, err := strconv.Atoi(suffix)
	if err != nil {
		return 0
	}
	return counter
}
