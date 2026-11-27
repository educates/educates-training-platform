package v1alpha1

const KindEducatesLocalRouter = "EducatesLocalRouter"

// EducatesLocalRouter is the router file of the CLI data home,
// <data-home>/router.yaml: the router address, the one host address every
// local cluster is served on, declared once for all of them. Local config
// init and local cluster create write a missing file with the host's IP
// address; the file can also be edited by hand.
type EducatesLocalRouter struct {
	TypeMeta `yaml:",inline"`

	// Address is the router address: a specific IP address of a network
	// interface of the host, neither the unspecified address nor a
	// loopback address.
	Address string `yaml:"address"`
}
