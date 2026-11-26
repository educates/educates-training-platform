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

// LoadLocalRouter returns the router file of the data home dataHome,
// LocalRouterPath, which declares the router address. The file is loaded
// strictly, as an EducatesLocalRouter its schema validates, with no field
// the kind lacks; a file that doesn't load is an error that names the
// file and why. A missing file is an error wrapping fs.ErrNotExist, since
// the CLI writes it when missing rather than taking an address for it.
//
// The address is loaded as the file declares it. The rules an address
// must follow to serve local clusters, which depend on the host's network
// interfaces, are the local-cluster inventory module's.
func LoadLocalRouter(dataHome string) (*v1alpha1.EducatesLocalRouter, error) {
	path := LocalRouterPath(dataHome)
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	return loadLocalRouterBytes(data, path)
}

// hostIPRouterAddressComment is the comment above a router address the
// CLI wrote from the host's detected IP address.
const hostIPRouterAddressComment = "# This host's IP address when this file was written. An alias such as\n" +
	"# 10.10.10.1, added to a network interface, survives network changes."

// CreateLocalRouter writes the router file of the data home dataHome,
// LocalRouterPath, declaring the router address address, when the data
// home has none, and reports whether it wrote it. An existing file is
// left as it is, whatever it holds, and so is never overwritten.
//
// The file is written with the yaml-language-server modeline, apiVersion
// and kind, and the data home with it. When addressIsHostIP is set,
// address is the host's IP address the CLI detected, and a comment above
// it says so, and that an alias such as 10.10.10.1 survives network
// changes. The file holds no secret, so it is readable by everyone, as
// the configs of the data home are.
func CreateLocalRouter(dataHome, address string, addressIsHostIP bool) (bool, error) {
	path := LocalRouterPath(dataHome)
	comment := ""
	if addressIsHostIP {
		comment = hostIPRouterAddressComment
	}
	doc, err := newLocalRouterDocument(address, comment)
	if err != nil {
		return false, err
	}
	data, err := encodeLocalRouterDocument(path, doc)
	if err != nil {
		return false, err
	}
	if err := os.MkdirAll(dataHome, 0o755); err != nil {
		return false, fmt.Errorf("write %s: %w", path, err)
	}
	// O_EXCL creates the file only when it is missing, even against
	// another command writing it at the same time.
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if errors.Is(err, fs.ErrExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("write %s: %w", path, err)
	}
	_, err = f.Write(data)
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		os.Remove(path)
		return false, fmt.Errorf("write %s: %w", path, err)
	}
	return true, nil
}

// SetLocalRouterAddress makes the router file of the data home dataHome,
// LocalRouterPath, declare the router address address. It replaces the
// address the file declares where it is, and keeps the rest of the file
// as it was: its comments, the comment on the address line among them,
// and its permissions. Only the comment the CLI writes above the host's
// IP address, which says the address is that IP, goes when another
// address replaces it. A file that already declares address is left as
// it is.
//
// A missing file is created as CreateLocalRouter creates it, with no
// comment. A file that doesn't load by the rules of LoadLocalRouter is an
// error naming it and why, and is left as it is. The file is written
// through a temporary file renamed over it.
func SetLocalRouterAddress(dataHome, address string) error {
	path := LocalRouterPath(dataHome)
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		created, createErr := CreateLocalRouter(dataHome, address, false)
		if createErr != nil || created {
			return createErr
		}
		// Another command created the file in the meantime: set the
		// address in what it wrote.
		data, err = os.ReadFile(path)
	}
	if err != nil {
		return fmt.Errorf("read %s: %w", path, err)
	}
	router, err := loadLocalRouterBytes(data, path)
	if err != nil {
		return err
	}
	if router.Address == address {
		return nil
	}
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("read %s: %w", path, err)
	}
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return fmt.Errorf("%s: parse YAML: %w", path, err)
	}
	root := doc.Content[0]
	for i := 0; i+1 < len(root.Content); i += 2 {
		key, value := root.Content[i], root.Content[i+1]
		if key.Value != "address" {
			continue
		}
		if key.HeadComment == hostIPRouterAddressComment {
			key.HeadComment = ""
		}
		value.Tag, value.Value = "!!str", address
	}
	out, err := encodeLocalRouterDocument(path, &doc)
	if err != nil {
		return err
	}
	return WriteFileAtomically(path, out, info.Mode().Perm())
}

// newLocalRouterDocument returns the node tree of a router file that
// declares the router address address, with comment, when set, above it:
// the yaml-language-server modeline, then the apiVersion and kind of an
// EducatesLocalRouter, then the address.
func newLocalRouterDocument(address, comment string) (*yaml.Node, error) {
	header := LocalRouterModeline + "\napiVersion: " + v1alpha1.APIVersion +
		"\nkind: " + v1alpha1.KindEducatesLocalRouter + "\n"
	var doc yaml.Node
	if err := yaml.Unmarshal([]byte(header), &doc); err != nil {
		return nil, fmt.Errorf("build the router file: %w", err)
	}
	key := yamlScalar("address")
	key.HeadComment = comment
	root := doc.Content[0]
	root.Content = append(root.Content, key, yamlScalar(address))
	return &doc, nil
}

// encodeLocalRouterDocument returns doc, the node tree of the router file
// at path, as the CLI writes it, once it loads by the rules of
// LoadLocalRouter.
func encodeLocalRouterDocument(path string, doc *yaml.Node) ([]byte, error) {
	data, err := encodeYAMLDocument(doc)
	if err != nil {
		return nil, fmt.Errorf("write %s: %w", path, err)
	}
	if _, err := loadLocalRouterBytes(data, path); err != nil {
		return nil, fmt.Errorf("refusing to write a router file that would not load: %w", err)
	}
	return data, nil
}

// loadLocalRouterBytes loads data, the router file source, by the rules
// of LoadLocalRouter.
func loadLocalRouterBytes(data []byte, source string) (*v1alpha1.EducatesLocalRouter, error) {
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
	case kind != v1alpha1.KindEducatesLocalRouter:
		return nil, fmt.Errorf("%s: kind %q is not the kind of the router file, %q",
			source, kind, v1alpha1.KindEducatesLocalRouter)
	}
	decoded, err := decode(jsonData, schemas.EducatesLocalRouter, source, &v1alpha1.EducatesLocalRouter{}, true)
	if err != nil {
		return nil, err
	}
	return decoded.(*v1alpha1.EducatesLocalRouter), nil
}
