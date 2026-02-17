package registry

// ContainerManager defines the interface for managing registry and mirror containers.
// Both Registry and Mirror types implement this interface.
type ContainerManager interface {
	// DeployAndLinkToCluster creates the container and configures the cluster to use it
	DeployAndLinkToCluster() error

	// DeleteAndUnlinkFromCluster removes the container and cleans up cluster configuration
	DeleteAndUnlinkFromCluster() error

	// Delete removes the container only without cluster configuration cleanup
	Delete() error
}
