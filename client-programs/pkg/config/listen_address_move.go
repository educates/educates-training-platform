package config

import (
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// MoveClusterListenAddress removes the listen address that the config at
// path still declares in its cluster block, cluster.listenAddress of an
// EducatesLocalConfig or target.cluster.listenAddress of an
// EducatesConfig, and writes a one-line note saying so to w. A local
// cluster has no address of its own: every local cluster is served
// through the router, on the router address that the router file of the
// data home dataHome, LocalRouterPath, declares.
//
// Only a config in the data home moves: the default local cluster's,
// <data-home>/config.yaml, or a named local cluster's,
// <data-home>/clusters/<name>/config.yaml. Any other file, a missing one
// and one without the field are left as they are, with no note; the
// loader's error for another file that declares the field says where the
// address went.
//
// The default local cluster's listen address becomes the router address
// when the data home has no router file and the address is a specific IP
// address, neither unspecified, as 0.0.0.0 is, nor loopback: the move
// writes the router file with it first, as CreateLocalRouter writes it.
// Any other listen address, a named local cluster's among them, is
// dropped, and an existing router file is left as it is. Then the
// config's previous content is kept beside it, as <file>.bak, or as
// <file>.<timestamp>.bak when that exists, readable only by the user, and
// the field is removed from the config, keeping the rest of the file,
// comments included, and the cluster block with it once nothing else is
// left in it.
func MoveClusterListenAddress(dataHome, path string, w io.Writer) error {
	config, err := readDataHomeConfig(dataHome, path)
	if err != nil || config == nil {
		return err
	}
	value := yamlMappingValue(config.cluster, "listenAddress")
	if value == nil {
		return nil
	}
	address := ""
	if value.Kind == yaml.ScalarNode && value.Tag != "!!null" {
		address = strings.TrimSpace(value.Value)
	}

	move := listenAddressMove{
		path:       path,
		field:      config.field("listenAddress"),
		address:    address,
		routerPath: LocalRouterPath(dataHome),
		isDefault:  sameDir(filepath.Dir(path), dataHome),
	}
	if move.isDefault && isSpecificAddress(address) {
		if move.moved, err = CreateLocalRouter(dataHome, address, false); err != nil {
			return fmt.Errorf("%s: %s was removed, and its address can't be moved to %s: %w",
				path, move.field, move.routerPath, err)
		}
	}

	removeYAMLMappingKey(config.cluster, "listenAddress")
	config.removeEmptyClusterBlock()
	if move.backup, err = config.rewrite(); err != nil {
		return err
	}
	fmt.Fprintln(w, move.note())
	return nil
}

// listenAddressMove is what MoveClusterListenAddress did with the listen
// address of a config, for the note it writes.
type listenAddressMove struct {
	// path is the config's path.
	path string

	// field is the field removed from the config, which declared address.
	field   string
	address string

	// routerPath is the path of the router file.
	routerPath string

	// backup is the path the previous config is kept at.
	backup string

	// moved says the address became the router address, in the router
	// file.
	moved bool

	// isDefault says the config is the default local cluster's.
	isDefault bool
}

// removeEmptyClusterBlock removes the config's cluster block when it
// declares nothing.
func (c *dataHomeConfig) removeEmptyClusterBlock() {
	if c.cluster == nil || c.cluster.Kind != yaml.MappingNode || len(c.cluster.Content) > 0 {
		return
	}
	parent := c.doc.Content[0]
	for _, key := range c.clusterPath[:len(c.clusterPath)-1] {
		parent = yamlMappingValue(parent, key)
	}
	removeYAMLMappingKey(parent, c.clusterPath[len(c.clusterPath)-1])
}

// isSpecificAddress reports whether address is an IP address that can be
// the router address of some host, as CheckSpecificRouterAddress decides.
func isSpecificAddress(address string) bool {
	_, err := CheckSpecificRouterAddress(address)
	return err == nil
}

// note is the note MoveClusterListenAddress writes after the move m.
func (m listenAddressMove) note() string {
	kept := fmt.Sprintf("The previous config is kept as %s.", m.backup)
	if m.moved {
		return fmt.Sprintf("note: moved %s of %s to %s as the router address, %s, which every local cluster "+
			"is served on. %s", m.field, m.path, m.routerPath, m.address, kept)
	}
	removed := m.field
	if m.address != "" {
		removed += " " + m.address
	}
	note := fmt.Sprintf("note: removed %s from %s: a local cluster has no address of its own, since every local "+
		"cluster is served on the router address, which %s declares.", removed, m.path, m.routerPath)
	if m.isDefault && m.address != "" && !isSpecificAddress(m.address) {
		note += fmt.Sprintf(" %s is not carried over to it, since it is not a specific IP address of this host.", m.address)
	}
	return note + " " + kept
}
