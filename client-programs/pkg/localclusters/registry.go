package localclusters

// RegistryTarget is an Educates cluster that the local registry and the
// registry mirrors, which every local cluster shares, are linked into.
type RegistryTarget struct {
	// Identity is the cluster's identity, which names its kube context.
	Identity Identity

	// State is whether the cluster's node containers run.
	State State

	// Nodes names the cluster's node containers that get the containerd
	// host config of the local registry and the registry mirrors, as
	// Identity.RegistryNodes returns them.
	Nodes []string
}

// RegistryTargets returns every Educates cluster, in the order of
// Clusters, with its state and the node containers the local registry and
// the registry mirrors are linked into. The commands that deploy the
// local registry or a registry mirror act on every one of them.
func (inv Inventory) RegistryTargets() []RegistryTarget {
	targets := make([]RegistryTarget, 0, len(inv.clusters))
	for _, id := range inv.clusters {
		targets = append(targets, RegistryTarget{
			Identity: id,
			State:    id.State(inv.observed.Nodes),
			Nodes:    id.RegistryNodes(inv.observed.Nodes),
		})
	}
	return targets
}

// RegistryNodes returns the names of the node containers of every
// Educates cluster that get the containerd host config of the local
// registry and the registry mirrors: the Nodes of each of
// RegistryTargets, in order. They are the nodes the commands that deploy
// or delete a registry mirror link it into or unlink it from.
func (inv Inventory) RegistryNodes() []string {
	var names []string
	for _, t := range inv.RegistryTargets() {
		names = append(names, t.Nodes...)
	}
	return names
}
