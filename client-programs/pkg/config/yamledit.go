package config

import (
	"bytes"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// The CLI edits the files of the data home that users also edit by hand
// as yaml.v3 node trees, so that a change keeps the file's comments, the
// yaml-language-server modeline among them, and the order of its keys and
// entries. Marshalling a decoded value instead would drop every comment.

// yamlMappingValue returns the value node paired with key in the mapping
// node m, or nil when m is not a mapping or has no such key.
func yamlMappingValue(m *yaml.Node, key string) *yaml.Node {
	if m == nil || m.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			return m.Content[i+1]
		}
	}
	return nil
}

// removeYAMLMappingKey removes key, and its value, from the mapping node
// m, and reports whether m had it.
func removeYAMLMappingKey(m *yaml.Node, key string) bool {
	if m == nil || m.Kind != yaml.MappingNode {
		return false
	}
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			m.Content = append(m.Content[:i], m.Content[i+2:]...)
			return true
		}
	}
	return false
}

// yamlScalar returns a string scalar node holding value. The encoder
// quotes it when it would otherwise read as another type, such as a
// number.
func yamlScalar(value string) *yaml.Node {
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: value}
}

// encodeYAMLDocument returns doc, a node tree, as YAML indented by two
// spaces, as the CLI writes the files of the data home.
func encodeYAMLDocument(doc *yaml.Node) ([]byte, error) {
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(doc); err != nil {
		return nil, err
	}
	if err := enc.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// writeFileAtomically writes data to the file at path with the
// permissions perm, through a temporary file in the same folder renamed
// over it, so that a reader sees either the old content or the new one,
// and a failed write leaves the old content. A path that is a symbolic
// link has the file it links to written.
func writeFileAtomically(path string, data []byte, perm fs.FileMode) error {
	if target, err := filepath.EvalSymlinks(path); err == nil {
		path = target
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".*")
	if err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	defer os.Remove(tmp.Name())
	if err := tmp.Chmod(perm); err != nil {
		tmp.Close()
		return fmt.Errorf("write %s: %w", path, err)
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("write %s: %w", path, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}
