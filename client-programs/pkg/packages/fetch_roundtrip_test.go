package packages

import (
	"fmt"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	regname "github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/registry"
	"github.com/google/go-containerregistry/pkg/v1/remote"
)

// startRegistry runs an in-memory registry, so the round trip exercises a
// real registry protocol without needing one running.
func startRegistry(t *testing.T) string {
	t.Helper()

	server := httptest.NewServer(registry.New())

	t.Cleanup(server.Close)

	parsed, err := url.Parse(server.URL)

	if err != nil {
		t.Fatal(err)
	}

	return parsed.Host
}

// publishForTest builds a package image from a source and pushes it, which is
// what the publish command does, and returns the reference.
func publishForTest(t *testing.T, host string, source string, tag string) string {
	t.Helper()

	build, err := BuildPackageImage(source, SupportedPlatforms, fixedTime())

	if err != nil {
		t.Fatalf("unable to build the package image: %v", err)
	}

	reference, err := regname.NewTag(fmt.Sprintf("%s/argocd:%s", host, tag))

	if err != nil {
		t.Fatal(err)
	}

	for _, child := range build.Children {
		digest, err := child.Image.Digest()

		if err != nil {
			t.Fatal(err)
		}

		if err := remote.Write(reference.Context().Digest(digest.String()), child.Image); err != nil {
			t.Fatalf("unable to push a child image: %v", err)
		}
	}

	if err := remote.WriteIndex(reference, build.Index); err != nil {
		t.Fatalf("unable to push the index: %v", err)
	}

	return reference.Name()
}

// A package image published by this CLI is fetched back with its modes and
// contents intact, which is what a session depends on.
func TestFetch_RoundTrip(t *testing.T) {
	source := writeSource(t, map[string]os.FileMode{
		"common/setup.d/01-setup.sh":  0o755,
		"common/profile.d/01-path.sh": 0o644,
		"linux-amd64/bin/argocd":      0o755,
		"linux-arm64/bin/argocd":      0o755,
	})

	if err := os.WriteFile(filepath.Join(source, "package.yaml"), []byte(validManifest), 0o644); err != nil {
		t.Fatal(err)
	}

	host := startRegistry(t)
	reference := publishForTest(t, host, source, "v2.10.6")

	keychain, err := LoadKeychain(filepath.Join(t.TempDir(), "absent"))

	if err != nil {
		t.Fatal(err)
	}

	target := filepath.Join(t.TempDir(), "packages", "argocd")

	report := &strings.Builder{}

	entry := FetchEntry{Path: target, Image: reference}

	options := FetchOptions{
		Platform: Platform{OS: "linux", Architecture: "amd64"},
		Keychain: keychain,
		Insecure: true,
	}

	if err := Fetch(entry, options, report); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// The manifest proves the package arrived, and is what the session tests
	// for.
	if _, err := os.Stat(filepath.Join(target, "package.yaml")); err != nil {
		t.Errorf("the package manifest is missing: %v", err)
	}

	// An executable file keeps its execute bits, which vendir used to drop.
	info, err := os.Stat(filepath.Join(target, "setup.d", "01-setup.sh"))

	if err != nil {
		t.Fatalf("the setup script is missing: %v", err)
	}

	if info.Mode().Perm() != 0o755 {
		t.Errorf("the setup script has mode %o, want 755", info.Mode().Perm())
	}

	// A file with no execute bit stays readable by everyone.
	info, err = os.Stat(filepath.Join(target, "profile.d", "01-path.sh"))

	if err != nil {
		t.Fatalf("the profile script is missing: %v", err)
	}

	if info.Mode().Perm() != 0o644 {
		t.Errorf("the profile script has mode %o, want 644", info.Mode().Perm())
	}

	if !strings.Contains(report.String(), "linux/amd64") {
		t.Errorf("expected the report to name the platform, got: %s", report.String())
	}
}

// The fetcher resolves the index to the platform it is asked for, which is
// why a package image needs no per-architecture reference.
func TestFetch_ResolvesRequestedPlatform(t *testing.T) {
	source := writeSource(t, map[string]os.FileMode{
		"common/setup.d/01-setup.sh": 0o755,
		"linux-amd64/bin/tool":       0o755,
		"linux-arm64/bin/tool":       0o755,
	})

	if err := os.WriteFile(filepath.Join(source, "package.yaml"), []byte(validManifest), 0o644); err != nil {
		t.Fatal(err)
	}

	host := startRegistry(t)
	reference := publishForTest(t, host, source, "v2.10.6")

	keychain, err := LoadKeychain(filepath.Join(t.TempDir(), "absent"))

	if err != nil {
		t.Fatal(err)
	}

	for _, platform := range SupportedPlatforms {
		target := filepath.Join(t.TempDir(), "packages", "argocd")

		entry := FetchEntry{Path: target, Image: reference}

		options := FetchOptions{Platform: platform, Keychain: keychain, Insecure: true}

		if err := Fetch(entry, options, nil); err != nil {
			t.Fatalf("unexpected error for %s: %v", platform, err)
		}

		contents, err := os.ReadFile(filepath.Join(target, "bin", "tool"))

		if err != nil {
			t.Fatalf("the platform binary is missing for %s: %v", platform, err)
		}

		// Each platform directory holds a file naming itself, so the contents
		// prove which child was resolved.
		if !strings.Contains(string(contents), platform.DirName()) {
			t.Errorf("fetching %s produced %q, which is the wrong child", platform, contents)
		}
	}
}

// Fetching replaces whatever was at the path, so a restarted session does not
// accumulate files from a previous package.
func TestFetch_ReplacesExistingContents(t *testing.T) {
	source := writeSource(t, map[string]os.FileMode{
		"common/setup.d/01-setup.sh": 0o755,
	})

	if err := os.WriteFile(filepath.Join(source, "package.yaml"), []byte(validManifest), 0o644); err != nil {
		t.Fatal(err)
	}

	host := startRegistry(t)
	reference := publishForTest(t, host, source, "v2.10.6")

	keychain, err := LoadKeychain(filepath.Join(t.TempDir(), "absent"))

	if err != nil {
		t.Fatal(err)
	}

	target := filepath.Join(t.TempDir(), "packages", "argocd")

	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatal(err)
	}

	stale := filepath.Join(target, "stale-file")

	if err := os.WriteFile(stale, []byte("left over\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	entry := FetchEntry{Path: target, Image: reference}

	options := FetchOptions{
		Platform: Platform{OS: "linux", Architecture: runtime.GOARCH},
		Keychain: keychain,
		Insecure: true,
	}

	if err := Fetch(entry, options, nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Errorf("a file from a previous fetch survived")
	}
}

// An image which is not a package image fails rather than leaving a session
// subtly wrong.
func TestFetch_MissingImageIsReported(t *testing.T) {
	host := startRegistry(t)

	keychain, err := LoadKeychain(filepath.Join(t.TempDir(), "absent"))

	if err != nil {
		t.Fatal(err)
	}

	entry := FetchEntry{
		Path:  filepath.Join(t.TempDir(), "packages", "absent"),
		Image: host + "/packages/absent:v1",
	}

	options := FetchOptions{
		Platform: Platform{OS: "linux", Architecture: "amd64"},
		Keychain: keychain,
		Insecure: true,
	}

	err = Fetch(entry, options, nil)

	if err == nil {
		t.Fatalf("expected fetching a missing image to fail")
	}

	if !strings.Contains(err.Error(), "unable to fetch") {
		t.Errorf("unexpected error: %v", err)
	}
}
