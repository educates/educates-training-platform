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

	field := config.field("listenAddress")
	routerPath := LocalRouterPath(dataHome)
	isDefault := sameDir(filepath.Dir(path), dataHome)
	moved := false
	if isDefault && isSpecificAddress(address) {
		if moved, err = CreateLocalRouter(dataHome, address, false); err != nil {
			return fmt.Errorf("%s: %s was removed, and its address can't be moved to %s: %w",
				path, field, routerPath, err)
		}
	}

	removeYAMLMappingKey(config.cluster, "listenAddress")
	config.removeEmptyClusterBlock()
	backup, err := config.rewrite()
	if err != nil {
		return err
	}
	fmt.Fprintln(w, listenAddressMoveNote(path, field, address, routerPath, backup, moved, isDefault))
	return nil
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

// listenAddressMoveNote is the note MoveClusterListenAddress writes after
// removing field, which declared address, from the config at path,
// keeping the previous config as backup. moved says the address became
// the router address, in the router file at routerPath, and isDefault
// that the config is the default local cluster's.
func listenAddressMoveNote(path, field, address, routerPath, backup string, moved, isDefault bool) string {
	kept := fmt.Sprintf("The previous config is kept as %s.", backup)
	if moved {
		return fmt.Sprintf("note: moved %s of %s to %s as the router address, %s, which every local cluster "+
			"is served on. %s", field, path, routerPath, address, kept)
	}
	removed := field
	if address != "" {
		removed += " " + address
	}
	note := fmt.Sprintf("note: removed %s from %s: a local cluster has no address of its own, since every local "+
		"cluster is served on the router address, which %s declares.", removed, path, routerPath)
	if isDefault && address != "" && !isSpecificAddress(address) {
		note += fmt.Sprintf(" %s is not carried over to it, since it is not a specific IP address of this host.", address)
	}
	return note + " " + kept
}
