// Package packages builds extension package images from a package source.
//
// A package source is a directory holding a package manifest beside the
// reserved directories common, linux-amd64 and linux-arm64. Each platform
// child image is common overlaid by its platform directory, with the manifest
// copied to the image root.
package packages

import (
	"os"
	"path/filepath"

	"github.com/pkg/errors"
	"sigs.k8s.io/yaml"
)

// ManifestFileName is the fixed name of the package manifest at the root of a
// package source. The contract fixes the name, so there is no flag for it.
const ManifestFileName = "package.yaml"

const (
	manifestAPIVersion = "packages.educates.dev/v1alpha1"
	manifestKind       = "ExtensionPackage"
)

// Manifest is the package manifest. It names the package and its version, and
// is copied verbatim into every child image so a session can prove the package
// was delivered.
type Manifest struct {
	APIVersion  string `json:"apiVersion"`
	Kind        string `json:"kind"`
	Name        string `json:"name"`
	Version     string `json:"version"`
	Description string `json:"description,omitempty"`
}

// LoadManifest reads and validates the package manifest at the root of a
// package source, returning it alongside the bytes to copy into the image.
func LoadManifest(sourceDir string) (*Manifest, []byte, error) {
	path := filepath.Join(sourceDir, ManifestFileName)

	data, err := os.ReadFile(path)

	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil, errors.Errorf("no %s found in %s, which is not an extension package source", ManifestFileName, sourceDir)
		}

		return nil, nil, errors.Wrapf(err, "unable to read %s", path)
	}

	manifest := &Manifest{}

	// Unknown fields are rejected so a misspelled property is reported rather
	// than silently ignored.

	if err := yaml.UnmarshalStrict(data, manifest); err != nil {
		return nil, nil, errors.Wrapf(err, "unable to parse %s", ManifestFileName)
	}

	if err := manifest.validate(); err != nil {
		return nil, nil, err
	}

	return manifest, data, nil
}

func (m *Manifest) validate() error {
	if m.APIVersion != manifestAPIVersion {
		return errors.Errorf("%s must set apiVersion to %s", ManifestFileName, manifestAPIVersion)
	}

	if m.Kind != manifestKind {
		return errors.Errorf("%s must set kind to %s", ManifestFileName, manifestKind)
	}

	if m.Name == "" {
		return errors.Errorf("%s must set name", ManifestFileName)
	}

	if m.Version == "" {
		return errors.Errorf("%s must set version", ManifestFileName)
	}

	return nil
}
