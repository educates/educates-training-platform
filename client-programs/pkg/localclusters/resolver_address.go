package localclusters

// AddressSource is where the address the local DNS resolver resolves a
// local cluster's names to comes from.
type AddressSource int

const (
	// AddressFromListenAddress is the cluster's listen address, a
	// specific IP address that is not a loopback address.
	AddressFromListenAddress AddressSource = iota

	// AddressFromHostIP is the host's IP address, for a cluster whose
	// listen address is not a specific IP address, or is a loopback one.
	AddressFromHostIP

	// AddressFromLoopback is the loopback address loopbackAddress, for a
	// cluster with neither a specific listen address nor a known host IP
	// address.
	AddressFromLoopback
)

// loopbackAddress is the address a local cluster's names resolve to when
// neither its listen address nor the host's IP address gives one.
const loopbackAddress = "127.0.0.1"

// resolverAddress returns the address the local DNS resolver resolves
// the names of a local cluster to, the default one or a named one, and
// where it comes from. listenAddress is the cluster's listen address, and
// hostIP the host's IP address, empty when it is not known.
//
// It is the listen address when that is a specific IP address and not a
// loopback address, as ValidateListenAddress checks it, since the cluster
// publishes its ports 80 and 443 there. Otherwise it is the host's IP
// address, and with that unknown, the loopback address.
func resolverAddress(listenAddress, hostIP string) (string, AddressSource) {
	switch {
	case ValidateListenAddress(listenAddress) == nil:
		return listenAddress, AddressFromListenAddress
	case hostIP != "":
		return hostIP, AddressFromHostIP
	default:
		return loopbackAddress, AddressFromLoopback
	}
}
