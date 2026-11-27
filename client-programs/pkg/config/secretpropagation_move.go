package config

import (
	"fmt"
	"io"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/educates/educates-training-platform/client-programs/pkg/config/v1alpha1"
)

// MoveSecretPropagation moves the image pull secret names that the
// EducatesLocalConfig at path still declares in
// secretPropagation.imagePullSecretNames to imageRegistry.pullSecrets,
// which replaced it, and writes a one-line note saying so to w. Names
// imageRegistry.pullSecrets already holds are not repeated.
//
// Only a config in the data home moves, as for
// MoveClusterRegistryMirrors: the default local cluster's or a named local
// cluster's config.yaml. Any other file, a missing one and one without the
// field are left as they are, with no note; the loader's error for another
// file that declares the field says what replaced it. The config's previous
// content is kept beside it, as <file>.bak, or as <file>.<timestamp>.bak
// when that exists, and the rest of the file, comments included, is kept.
func MoveSecretPropagation(dataHome, path string, w io.Writer) error {
	config, err := readDataHomeConfig(dataHome, path)
	if err != nil || config == nil {
		return err
	}
	root := config.doc.Content[0]
	if kind := yamlMappingValue(root, "kind"); kind == nil || kind.Value != v1alpha1.KindEducatesLocalConfig {
		return nil
	}
	propagation := yamlMappingValue(root, "secretPropagation")
	if propagation == nil {
		return nil
	}

	var names []string
	if list := yamlMappingValue(propagation, "imagePullSecretNames"); list != nil && list.Kind == yaml.SequenceNode {
		for _, item := range list.Content {
			if item.Kind == yaml.ScalarNode && item.Value != "" {
				names = append(names, item.Value)
			}
		}
	}

	if registry := yamlMappingValue(root, "imageRegistry"); registry != nil {
		appendPullSecrets(registry, names)
		removeYAMLMappingKey(root, "secretPropagation")
	} else {
		// imageRegistry takes the place of secretPropagation in the file.
		for i := 0; i+1 < len(root.Content); i += 2 {
			if root.Content[i].Value == "secretPropagation" {
				registry := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
				appendPullSecrets(registry, names)
				root.Content[i] = yamlScalar("imageRegistry")
				root.Content[i+1] = registry
				break
			}
		}
		if len(names) == 0 {
			removeYAMLMappingKey(root, "imageRegistry")
		}
	}
	backup, err := config.rewrite()
	if err != nil {
		return err
	}

	moved := "it named no image pull secrets"
	if len(names) > 0 {
		moved = "its names, " + strings.Join(names, ", ") + ", are in imageRegistry.pullSecrets"
	}
	fmt.Fprintf(w, "%s: secretPropagation.imagePullSecretNames was replaced by imageRegistry.pullSecrets, so it "+
		"was removed from the config and %s; the previous config is kept as %s.\n", path, moved, backup)
	return nil
}

// appendPullSecrets appends each of names that the pullSecrets list of
// the imageRegistry mapping node registry lacks, creating the list when
// needed.
func appendPullSecrets(registry *yaml.Node, names []string) {
	if len(names) == 0 {
		return
	}
	list := yamlMappingValue(registry, "pullSecrets")
	if list == nil || list.Kind != yaml.SequenceNode {
		removeYAMLMappingKey(registry, "pullSecrets")
		list = &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
		registry.Content = append(registry.Content, yamlScalar("pullSecrets"), list)
	}
	held := map[string]bool{}
	for _, item := range list.Content {
		held[item.Value] = true
	}
	for _, name := range names {
		if !held[name] {
			held[name] = true
			list.Content = append(list.Content, yamlScalar(name))
		}
	}
}

// checkNoSecretPropagation returns an error saying what replaced
// secretPropagation when raw, the top level of a config of a narrow kind,
// still sets it. The field was removed, so the error comes before the
// schema's generic one. A config in the data home never gets here, since
// reading it moves the field (MoveSecretPropagation).
func checkNoSecretPropagation(raw map[string]interface{}) error {
	propagation, set := raw["secretPropagation"]
	if !set {
		return nil
	}
	replaced := "secretPropagation was replaced by imageRegistry.pullSecrets, which names the image pull secrets " +
		"the operator and everything it installs pull with"
	var names []string
	list, _ := asMap(propagation)["imagePullSecretNames"].([]interface{})
	for _, item := range list {
		if name, ok := item.(string); ok && name != "" {
			names = append(names, name)
		}
	}
	if len(names) == 0 {
		return fmt.Errorf("%s. Remove secretPropagation from the config", replaced)
	}
	return fmt.Errorf("%s. Move its names there:\n\nimageRegistry:\n  pullSecrets:\n    - %s\n\nand remove "+
		"secretPropagation from the config", replaced, strings.Join(names, "\n    - "))
}
