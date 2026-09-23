package cmd

import (
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/yaml"
)

// workshopFromYAML builds the unstructured Workshop the generator functions
// read. The docker renderer has no schema for the Workshop definition, so a
// malformed or simply unexpected package item reaches the generator as
// whatever the YAML decoded to.
func workshopFromYAML(t *testing.T, content string) *unstructured.Unstructured {
	t.Helper()

	object := map[string]interface{}{}

	if err := yaml.Unmarshal([]byte(content), &object); err != nil {
		t.Fatalf("could not parse workshop fixture: %v", err)
	}

	return &unstructured.Unstructured{Object: object}
}

const workshopWithFilesPackage = `
apiVersion: training.educates.dev/v1beta1
kind: Workshop
metadata:
  name: lab-testing
spec:
  workshop:
    packages:
    - name: argocd
      files:
      - image:
          url: ghcr.io/educates/educates-extension-packages/argocd:v2.10.6
`

// A package whose content comes from an image rather than a vendir download
// carries no files entry, so the renderer skips it.
const workshopWithImagePackage = `
apiVersion: training.educates.dev/v1beta1
kind: Workshop
metadata:
  name: lab-testing
spec:
  workshop:
    packages:
    - name: argocd
      image: ghcr.io/educates/educates-extension-packages/argocd:v2.10.6
`

const workshopWithMixedPackages = `
apiVersion: training.educates.dev/v1beta1
kind: Workshop
metadata:
  name: lab-testing
spec:
  workshop:
    packages:
    - name: argocd
      image: ghcr.io/educates/educates-extension-packages/argocd:v2.10.6
    - name: vcluster
      files:
      - image:
          url: ghcr.io/educates/educates-extension-packages/vcluster:v0.19.0
`

// TestGenerateVendirPackagesConfig_FilesPackage pins the behaviour of a
// package declaring files: it produces a vendir directory with the default
// contents path applied.
func TestGenerateVendirPackagesConfig_FilesPackage(t *testing.T) {
	workshop := workshopFromYAML(t, workshopWithFilesPackage)

	config, err := generateVendirPackagesConfig(workshop, "lab-testing", "localhost:5001", "latest")

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(config, "/opt/packages/argocd") {
		t.Errorf("expected the package path in the config, got:\n%s", config)
	}

	if !strings.Contains(config, "path: .") {
		t.Errorf("expected the default contents path to be applied, got:\n%s", config)
	}
}

// TestGenerateVendirPackagesConfig_ImagePackageDoesNotPanic covers a package
// with no files key, which the unchecked type assertion used to panic on.
func TestGenerateVendirPackagesConfig_ImagePackageDoesNotPanic(t *testing.T) {
	workshop := workshopFromYAML(t, workshopWithImagePackage)

	config, err := generateVendirPackagesConfig(workshop, "lab-testing", "localhost:5001", "latest")

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if strings.Contains(config, "argocd") {
		t.Errorf("a package without files must not reach the vendir config, got:\n%s", config)
	}

	// No config at all rather than one with an empty directory list: the base
	// image runs vendir when the file is present, so an empty config would
	// have it download nothing, slowly.
	if config != "" {
		t.Errorf("expected no vendir config when every package is an image, got:\n%s", config)
	}
}

// TestGenerateVendirPackagesConfig_MixedPackages proves the skip is per
// package: a files package beside an image package is still rendered.
func TestGenerateVendirPackagesConfig_MixedPackages(t *testing.T) {
	workshop := workshopFromYAML(t, workshopWithMixedPackages)

	config, err := generateVendirPackagesConfig(workshop, "lab-testing", "localhost:5001", "latest")

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if strings.Contains(config, "/opt/packages/argocd") {
		t.Errorf("the image package must be skipped, got:\n%s", config)
	}

	if !strings.Contains(config, "/opt/packages/vcluster") {
		t.Errorf("the files package must still be rendered, got:\n%s", config)
	}
}

// TestGenerateVendirPackagesConfig_MalformedItems covers the remaining
// unchecked assertions in the same loop: a package item that is not a map, a
// name that is not a string, a files value that is not a list, and a files
// entry that is not a map. Each reports an error naming the problem rather
// than crashing.
func TestGenerateVendirPackagesConfig_MalformedItems(t *testing.T) {
	cases := []struct {
		name     string
		workshop string
		wantErr  string
	}{
		{
			name: "package item is not a map",
			workshop: `
spec:
  workshop:
    packages:
    - just-a-string
`,
			wantErr: "entry is not an object",
		},
		{
			name: "name is not a string",
			workshop: `
spec:
  workshop:
    packages:
    - name: 42
      files:
      - path: .
`,
			wantErr: "name 42 is not a string",
		},
		{
			name: "files is not a list",
			workshop: `
spec:
  workshop:
    packages:
    - name: argocd
      files: nonsense
`,
			wantErr: `extension package "argocd", files is not a list`,
		},
		{
			name: "files entry is not a map",
			workshop: `
spec:
  workshop:
    packages:
    - name: argocd
      files:
      - just-a-string
`,
			wantErr: `extension package "argocd", files entry is not an object`,
		},
	}

	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			workshop := workshopFromYAML(t, test.workshop)

			config, err := generateVendirPackagesConfig(workshop, "lab-testing", "localhost:5001", "latest")

			if err == nil {
				t.Fatalf("expected an error naming the problem, got config:\n%s", config)
			}

			if !strings.Contains(err.Error(), test.wantErr) {
				t.Errorf("expected the error to mention %q, got: %v", test.wantErr, err)
			}
		})
	}
}
