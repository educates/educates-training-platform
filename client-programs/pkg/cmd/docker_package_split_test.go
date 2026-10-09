package cmd

import (
	"strings"
	"testing"

	composetypes "github.com/compose-spec/compose-go/v2/types"
	"sigs.k8s.io/yaml"
)

const workshopWithTwoImagePackages = `
apiVersion: training.educates.dev/v1beta1
kind: Workshop
metadata:
  name: lab-testing
spec:
  workshop:
    packages:
    - name: argocd
      image: ghcr.io/educates/educates-extension-packages/argocd:v2.10.6
    - name: crane
      image: $(image_repository)/crane:$(workshop_version)
      imagePullPolicy: Always
`

// The session and the daemon reach the local registry by different addresses,
// as they do when deploying against the local registry, so a test can see
// which of the two a reference was expanded with.
var testImageRepositories = imageRepositories{
	Session: "registry.docker.local:5000",
	Daemon:  "localhost:5001",
}

// splitPackages is exercised through both deliveries, since which list a
// package lands in is the whole point.
func TestSplitImagePackages(t *testing.T) {
	workshop := workshopFromYAML(t, workshopWithTwoImagePackages)

	t.Run("mounted packages become volumes and no fetcher entries", func(t *testing.T) {
		packages, err := splitImagePackages(workshop, true, testImageRepositories, "lab-testing", "1.0")

		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if len(packages.Mounts) != 2 {
			t.Fatalf("expected 2 mounts, got %d", len(packages.Mounts))
		}

		if len(packages.Fetches) != 0 {
			t.Errorf("expected no fetcher entries when mounting, got %d", len(packages.Fetches))
		}

		mount := packages.Mounts[0]

		if mount.Type != "image" {
			t.Errorf("mount type = %q, want image", mount.Type)
		}

		if mount.Target != "/opt/packages/argocd" {
			t.Errorf("mount target = %q", mount.Target)
		}

		if !mount.ReadOnly {
			t.Error("a mounted package must be read only")
		}

		if mount.Source != "ghcr.io/educates/educates-extension-packages/argocd:v2.10.6" {
			t.Errorf("mount source = %q", mount.Source)
		}
	})

	// The daemon pulls the image behind a volume, so a mounted reference is
	// expanded with the address the daemon reaches the registry by.
	t.Run("the three tokens are expanded for the daemon in a mounted reference", func(t *testing.T) {
		packages, err := splitImagePackages(workshop, true, testImageRepositories, "lab-testing", "1.0")

		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		want := "localhost:5001/crane:1.0"

		if got := packages.Mounts[1].Source; got != want {
			t.Errorf("mount source = %q, want %q", got, want)
		}
	})

	t.Run("fetched packages become fetcher entries and no volumes", func(t *testing.T) {
		packages, err := splitImagePackages(workshop, false, testImageRepositories, "lab-testing", "1.0")

		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if len(packages.Mounts) != 0 {
			t.Errorf("expected no mounts when fetching, got %d", len(packages.Mounts))
		}

		if len(packages.Fetches) != 2 {
			t.Fatalf("expected 2 fetcher entries, got %d", len(packages.Fetches))
		}

		if packages.Fetches[0].Path != "/opt/packages/argocd" {
			t.Errorf("fetch path = %q", packages.Fetches[0].Path)
		}

		// The fetcher runs in the session, so a fetched reference is expanded
		// with the address the session reaches the registry by.
		if packages.Fetches[1].Image != "registry.docker.local:5000/crane:1.0" {
			t.Errorf("fetch image = %q", packages.Fetches[1].Image)
		}
	})

	t.Run("a workshop with no packages yields neither", func(t *testing.T) {
		empty := workshopFromYAML(t, `
apiVersion: training.educates.dev/v1beta1
kind: Workshop
metadata:
  name: lab-testing
spec:
  workshop: {}
`)

		packages, err := splitImagePackages(empty, true, testImageRepositories, "lab-testing", "1.0")

		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if len(packages.Mounts) != 0 || len(packages.Fetches) != 0 {
			t.Errorf("expected nothing, got %d mounts and %d fetches",
				len(packages.Mounts), len(packages.Fetches))
		}
	})

	t.Run("a files package is left to vendir under either delivery", func(t *testing.T) {
		for _, mounting := range []bool{true, false} {
			packages, err := splitImagePackages(
				workshopFromYAML(t, workshopWithFilesPackage), mounting,
				testImageRepositories, "lab-testing", "1.0",
			)

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if len(packages.Mounts) != 0 || len(packages.Fetches) != 0 {
				t.Errorf("mounting=%v: a files package must not be mounted or fetched", mounting)
			}
		}
	})

	t.Run("a package declaring both is left to the validator", func(t *testing.T) {
		both := workshopFromYAML(t, `
apiVersion: training.educates.dev/v1beta1
kind: Workshop
metadata:
  name: lab-testing
spec:
  workshop:
    packages:
    - name: argocd
      image: ghcr.io/educates/argocd:v1
      files:
      - image:
          url: ghcr.io/educates/argocd:v1
`)

		// validateWorkshopPackages runs first in the deploy and refuses this,
		// so the split does not repeat the rule. Checked here so that removing
		// the validation does not silently leave the case unhandled.
		if err := validateWorkshopPackages(both); err == nil {
			t.Error("expected the package validation to refuse image and files together")
		}
	})

	t.Run("a malformed image is refused rather than mounted", func(t *testing.T) {
		malformed := workshopFromYAML(t, `
apiVersion: training.educates.dev/v1beta1
kind: Workshop
metadata:
  name: lab-testing
spec:
  workshop:
    packages:
    - name: argocd
      image: [not, a, string]
`)

		if _, err := splitImagePackages(malformed, true, testImageRepositories, "lab-testing", "1.0"); err == nil {
			t.Fatal("expected a non string image to be refused")
		}
	})
}

func TestGenerateFetchPackagesConfig(t *testing.T) {
	t.Run("no entries yields no config", func(t *testing.T) {
		config, err := generateFetchPackagesConfig(nil)

		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		// An empty string is how the template decides to write nothing, which
		// matters because the base image treats the file's presence as the
		// signal that a fetch is needed.
		if config != "" {
			t.Errorf("expected no config, got %q", config)
		}
	})

	t.Run("entries produce a config the fetcher accepts", func(t *testing.T) {
		config, err := generateFetchPackagesConfig([]fetchPackageEntry{
			{Path: "/opt/packages/argocd", Image: "ghcr.io/educates/argocd:v1"},
		})

		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		parsed := map[string]interface{}{}

		if err := yaml.Unmarshal([]byte(config), &parsed); err != nil {
			t.Fatalf("the config is not valid YAML: %v", err)
		}

		if parsed["apiVersion"] != "packages.educates.dev/v1alpha1" {
			t.Errorf("apiVersion = %v", parsed["apiVersion"])
		}

		if parsed["kind"] != "PackageFetch" {
			t.Errorf("kind = %v", parsed["kind"])
		}

		if !strings.Contains(config, "/opt/packages/argocd") {
			t.Errorf("expected the path in the config, got %q", config)
		}
	})
}

// The compose volume shape is what Compose itself validates, so the test
// pins the field names rather than trusting the struct literal.
func TestMountedPackageVolumeShape(t *testing.T) {
	packages, err := splitImagePackages(
		workshopFromYAML(t, workshopWithImagePackage), true,
		testImageRepositories, "lab-testing", "1.0",
	)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(packages.Mounts) != 1 {
		t.Fatalf("expected one mount, got %d", len(packages.Mounts))
	}

	var volume composetypes.ServiceVolumeConfig = packages.Mounts[0]

	encoded, err := yaml.Marshal(volume)

	if err != nil {
		t.Fatalf("unable to encode the volume: %v", err)
	}

	for _, want := range []string{"type: image", "read_only: true", "target: /opt/packages/argocd"} {
		if !strings.Contains(string(encoded), want) {
			t.Errorf("expected the encoded volume to contain %q, got:\n%s", want, encoded)
		}
	}
}
