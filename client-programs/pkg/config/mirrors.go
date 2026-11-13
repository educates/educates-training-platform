package config

import (
	"errors"
	"fmt"
	"io/fs"
	"os"

	"gopkg.in/yaml.v3"

	"github.com/educates/educates-training-platform/client-programs/pkg/config/v1alpha1"
	"github.com/educates/educates-training-platform/client-programs/pkg/config/v1alpha1/schemas"
)

// LoadLocalMirrors returns the registry mirrors the registry mirrors file
// of the data home dataHome, LocalMirrorsPath, declares. A missing file
// declares none. The file is loaded strictly, as an EducatesLocalMirrors
// its schema validates, with no field the kind lacks and each mirror
// declared once; a file that doesn't load is an error that names the file
// and why.
func LoadLocalMirrors(dataHome string) ([]v1alpha1.RegistryMirror, error) {
	path := LocalMirrorsPath(dataHome)
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	mirrors, err := loadLocalMirrorsBytes(data, path)
	if err != nil {
		return nil, err
	}
	return mirrors.RegistryMirrors, nil
}

// RecordLocalMirror records the registry mirror mirror in the registry
// mirrors file of the data home dataHome, LocalMirrorsPath: it adds the
// mirror's entry at the end, or replaces the entry of the mirror of the
// same name, field by field, so that the comments on the fields it keeps
// stay. The rest of the file is left as it was: the other entries, their
// order, and comments.
//
// A missing file is created, with the yaml-language-server modeline,
// apiVersion and kind, and the data home with it. A file that doesn't load
// by the rules of LoadLocalMirrors is an error naming it and why, and is
// left as it is. The file can hold passwords, so it is written readable
// only by the user, through a temporary file renamed over it.
func RecordLocalMirror(dataHome string, mirror v1alpha1.RegistryMirror) error {
	path := LocalMirrorsPath(dataHome)
	doc, err := readLocalMirrorsDocument(path)
	if errors.Is(err, fs.ErrNotExist) {
		doc, err = newLocalMirrorsDocument()
		if err == nil {
			err = os.MkdirAll(dataHome, 0o755)
		}
	}
	if err != nil {
		return err
	}
	list := registryMirrorsNode(doc, true)
	entry := mirrorEntryNode(list, mirror.Mirror)
	if entry == nil {
		entry = &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
		if len(list.Content) == 0 {
			list.Style = 0
		}
		list.Content = append(list.Content, entry)
	}
	setMirrorFields(entry, mirror)
	return writeLocalMirrorsDocument(path, doc)
}

// RemoveLocalMirror removes the entry of the registry mirror name from
// the registry mirrors file of the data home dataHome, and reports
// whether the file recorded it. The rest of the file is left as it was,
// as RecordLocalMirror leaves it. A missing file records no mirror and is
// not created, and a file that doesn't load is an error naming it and
// why.
func RemoveLocalMirror(dataHome, name string) (bool, error) {
	path := LocalMirrorsPath(dataHome)
	doc, err := readLocalMirrorsDocument(path)
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	list := registryMirrorsNode(doc, false)
	if list == nil {
		return false, nil
	}
	for i, entry := range list.Content {
		if mirrorName(entry) == name {
			list.Content = append(list.Content[:i], list.Content[i+1:]...)
			return true, writeLocalMirrorsDocument(path, doc)
		}
	}
	return false, nil
}

// readLocalMirrorsDocument reads the registry mirrors file at path as a
// YAML node tree, after checking that it loads by the rules of
// LoadLocalMirrors. A missing file is an error wrapping fs.ErrNotExist.
func readLocalMirrorsDocument(path string) (*yaml.Node, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	if _, err := loadLocalMirrorsBytes(data, path); err != nil {
		return nil, err
	}
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("%s: parse YAML: %w", path, err)
	}
	return &doc, nil
}

// newLocalMirrorsDocument returns the node tree of a registry mirrors
// file that declares no mirror: the yaml-language-server modeline, then
// the apiVersion and kind of an EducatesLocalMirrors.
func newLocalMirrorsDocument() (*yaml.Node, error) {
	header := LocalMirrorsModeline + "\napiVersion: " + v1alpha1.APIVersion +
		"\nkind: " + v1alpha1.KindEducatesLocalMirrors + "\n"
	var doc yaml.Node
	if err := yaml.Unmarshal([]byte(header), &doc); err != nil {
		return nil, fmt.Errorf("build the registry mirrors file: %w", err)
	}
	return &doc, nil
}

// writeLocalMirrorsDocument writes doc, the node tree of a registry
// mirrors file, to path, readable only by the user, once it loads by the
// rules of LoadLocalMirrors.
func writeLocalMirrorsDocument(path string, doc *yaml.Node) error {
	data, err := encodeYAMLDocument(doc)
	if err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	if _, err := loadLocalMirrorsBytes(data, path); err != nil {
		return fmt.Errorf("refusing to write a registry mirrors file that would not load: %w", err)
	}
	return writeFileAtomically(path, data, 0o600)
}

// registryMirrorsNode returns the sequence node of the registryMirrors of
// doc, the node tree of a registry mirrors file that loads. When the file
// has none, it adds an empty one if add is set, and otherwise returns nil.
func registryMirrorsNode(doc *yaml.Node, add bool) *yaml.Node {
	root := doc.Content[0]
	if list := yamlMappingValue(root, "registryMirrors"); list != nil {
		return list
	}
	if !add {
		return nil
	}
	list := &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
	root.Content = append(root.Content, yamlScalar("registryMirrors"), list)
	return list
}

// mirrorEntryNode returns the entry of the registry mirror name in list,
// the sequence node of a registryMirrors, or nil when it has none.
func mirrorEntryNode(list *yaml.Node, name string) *yaml.Node {
	for _, entry := range list.Content {
		if mirrorName(entry) == name {
			return entry
		}
	}
	return nil
}

// mirrorName returns the name of the registry mirror whose entry is the
// mapping node entry.
func mirrorName(entry *yaml.Node) string {
	if name := yamlMappingValue(entry, "mirror"); name != nil {
		return name.Value
	}
	return ""
}

// setMirrorFields makes entry, the mapping node of a registry mirror's
// entry, hold the fields of mirror: a field mirror sets replaces the
// value of the entry's field in place, or is added in the order the kind
// declares the fields, and a field mirror leaves empty is removed.
func setMirrorFields(entry *yaml.Node, mirror v1alpha1.RegistryMirror) {
	fields := []struct{ key, value string }{
		{"mirror", mirror.Mirror},
		{"url", mirror.URL},
		{"username", mirror.Username},
		{"password", mirror.Password},
		{"port", mirror.Port},
		{"bindIP", mirror.BindIP},
	}
	for _, f := range fields {
		existing := yamlMappingValue(entry, f.key)
		switch {
		case f.value == "":
			removeYAMLMappingKey(entry, f.key)
		case existing != nil:
			existing.Kind, existing.Tag, existing.Value, existing.Content = yaml.ScalarNode, "!!str", f.value, nil
		default:
			entry.Content = append(entry.Content, yamlScalar(f.key), yamlScalar(f.value))
		}
	}
}

// loadLocalMirrorsBytes loads data, the registry mirrors file source, by
// the rules of LoadLocalMirrors.
func loadLocalMirrorsBytes(data []byte, source string) (*v1alpha1.EducatesLocalMirrors, error) {
	jsonData, raw, err := yamlToJSON(data, source)
	if err != nil {
		return nil, err
	}
	apiVersion, _ := raw["apiVersion"].(string)
	kind, _ := raw["kind"].(string)
	switch {
	case apiVersion == "" || kind == "":
		return nil, fmt.Errorf("%s: missing required field 'apiVersion' or 'kind'", source)
	case apiVersion != v1alpha1.APIVersion:
		return nil, fmt.Errorf("%s: unsupported apiVersion %q (want %q)", source, apiVersion, v1alpha1.APIVersion)
	case kind != v1alpha1.KindEducatesLocalMirrors:
		return nil, fmt.Errorf("%s: kind %q is not the kind of the registry mirrors file, %q",
			source, kind, v1alpha1.KindEducatesLocalMirrors)
	}
	decoded, err := decode(jsonData, schemas.EducatesLocalMirrors, source, &v1alpha1.EducatesLocalMirrors{}, true)
	if err != nil {
		return nil, err
	}
	mirrors := decoded.(*v1alpha1.EducatesLocalMirrors)
	declared := map[string]bool{}
	for _, m := range mirrors.RegistryMirrors {
		if declared[m.Mirror] {
			return nil, fmt.Errorf("%s: registry mirror %s is declared more than once; declare each mirror once",
				source, m.Mirror)
		}
		declared[m.Mirror] = true
	}
	return mirrors, nil
}
