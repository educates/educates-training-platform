package cmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/educates/educates-training-platform/client-programs/pkg/packages"
)

// The docker renderer and the session manager both write the fetcher's
// configuration, and the fetcher reads one shape. This proves the renderer's
// output is that shape by loading it with the fetcher's own loader, rather
// than by comparing it to a string which could drift.
func TestFetchPackagesConfigIsReadableByTheFetcher(t *testing.T) {
	config, err := generateFetchPackagesConfig([]fetchPackageEntry{
		{Path: "/opt/packages/argocd", Image: "ghcr.io/educates/argocd:v2.10.6"},
		{Path: "/opt/packages/crane", Image: "registry.local:5000/crane:v1"},
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	path := filepath.Join(t.TempDir(), "packages.yaml")

	if err := os.WriteFile(path, []byte(config), 0o644); err != nil {
		t.Fatalf("unable to write the config: %v", err)
	}

	loaded, err := packages.LoadFetchConfig(path)

	if err != nil {
		t.Fatalf("the fetcher could not read the renderer's config: %v", err)
	}

	if len(loaded.Packages) != 2 {
		t.Fatalf("expected 2 packages, got %d", len(loaded.Packages))
	}

	if loaded.Packages[0].Path != "/opt/packages/argocd" {
		t.Errorf("path = %q", loaded.Packages[0].Path)
	}

	if loaded.Packages[0].Image != "ghcr.io/educates/argocd:v2.10.6" {
		t.Errorf("image = %q", loaded.Packages[0].Image)
	}
}
