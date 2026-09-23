package packages

import (
	"archive/tar"
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	regv1 "github.com/google/go-containerregistry/pkg/v1"
)

const validManifest = `apiVersion: packages.educates.dev/v1alpha1
kind: ExtensionPackage
name: argocd
version: v2.10.6
description: Argo CD command line tool
`

// writeSource builds a package source on disk. Paths are relative to the
// source root; a path ending in a slash makes a directory. A mode of 0 means
// an ordinary readable file.
func writeSource(t *testing.T, files map[string]os.FileMode) string {
	t.Helper()

	dir := t.TempDir()

	for name, mode := range files {
		full := filepath.Join(dir, filepath.FromSlash(name))

		if strings.HasSuffix(name, "/") {
			if err := os.MkdirAll(full, 0o755); err != nil {
				t.Fatal(err)
			}
			continue
		}

		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}

		if mode == 0 {
			mode = 0o644
		}

		if err := os.WriteFile(full, []byte("contents of "+name+"\n"), mode); err != nil {
			t.Fatal(err)
		}
	}

	return dir
}

func fixedTime() time.Time {
	return time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)
}

// layerEntries reads back the single layer of an image as tar headers.
func layerEntries(t *testing.T, image regv1.Image) map[string]*tar.Header {
	t.Helper()

	layers, err := image.Layers()

	if err != nil {
		t.Fatal(err)
	}

	if len(layers) != 1 {
		t.Fatalf("expected exactly one layer, got %d", len(layers))
	}

	reader, err := layers[0].Uncompressed()

	if err != nil {
		t.Fatal(err)
	}

	defer reader.Close()

	headers := map[string]*tar.Header{}
	tarReader := tar.NewReader(reader)

	for {
		header, err := tarReader.Next()

		if err == io.EOF {
			break
		}

		if err != nil {
			t.Fatal(err)
		}

		headers[strings.TrimSuffix(header.Name, "/")] = header
	}

	return headers
}

func TestBuildPackageImage_ChildrenAndIndex(t *testing.T) {
	source := writeSource(t, map[string]os.FileMode{
		"package.yaml":                0,
		"common/setup.d/01-setup.sh":  0o755,
		"common/profile.d/01-path.sh": 0o644,
		"linux-amd64/bin/argocd":      0o755,
		"linux-arm64/bin/argocd":      0o755,
	})

	if err := os.WriteFile(filepath.Join(source, "package.yaml"), []byte(validManifest), 0o644); err != nil {
		t.Fatal(err)
	}

	build, err := BuildPackageImage(source, SupportedPlatforms, fixedTime())

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(build.Children) != 2 {
		t.Fatalf("expected two children, got %d", len(build.Children))
	}

	indexManifest, err := build.Index.IndexManifest()

	if err != nil {
		t.Fatal(err)
	}

	if len(indexManifest.Manifests) != 2 {
		t.Fatalf("expected two manifests in the index, got %d", len(indexManifest.Manifests))
	}

	// Every child descriptor carries its platform, which the library does not
	// infer.
	seen := map[string]bool{}

	for _, descriptor := range indexManifest.Manifests {
		if descriptor.Platform == nil {
			t.Fatalf("a child descriptor has no platform")
		}

		seen[descriptor.Platform.OS+"/"+descriptor.Platform.Architecture] = true
	}

	for _, platform := range SupportedPlatforms {
		if !seen[platform.String()] {
			t.Errorf("the index has no child for %s", platform)
		}
	}

	// The index carries the marker and the three standard keys.
	for key, want := range Metadata(build.Manifest, fixedTime()) {
		if got := indexManifest.Annotations[key]; got != want {
			t.Errorf("index annotation %s = %q, want %q", key, got, want)
		}
	}

	for _, child := range build.Children {
		configFile, err := child.Image.ConfigFile()

		if err != nil {
			t.Fatal(err)
		}

		if configFile.OS != child.Platform.OS || configFile.Architecture != child.Platform.Architecture {
			t.Errorf("child %s reports os=%s architecture=%s", child.Platform, configFile.OS, configFile.Architecture)
		}

		for key, want := range Metadata(build.Manifest, fixedTime()) {
			if got := configFile.Config.Labels[key]; got != want {
				t.Errorf("child %s label %s = %q, want %q", child.Platform, key, got, want)
			}
		}
	}
}

func TestBuildPackageImage_LayerContents(t *testing.T) {
	source := writeSource(t, map[string]os.FileMode{
		"common/setup.d/01-setup.sh":  0o700,
		"common/profile.d/01-path.sh": 0o600,
		"linux-amd64/bin/argocd":      0o755,
		"linux-arm64/bin/argocd":      0o755,
	})

	if err := os.WriteFile(filepath.Join(source, "package.yaml"), []byte(validManifest), 0o644); err != nil {
		t.Fatal(err)
	}

	build, err := BuildPackageImage(source, []Platform{{OS: "linux", Architecture: "amd64"}}, fixedTime())

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	headers := layerEntries(t, build.Children[0].Image)

	// The manifest sits at the image root under both deliveries.
	if _, found := headers["package.yaml"]; !found {
		t.Fatalf("the layer has no package.yaml, entries: %v", keysOf(headers))
	}

	// The platform directory contributes its own files; the other one does not.
	if _, found := headers["bin/argocd"]; !found {
		t.Errorf("the amd64 child has no bin/argocd, entries: %v", keysOf(headers))
	}

	for name, header := range headers {
		if header.Uid != 0 || header.Gid != 0 || header.Uname != "root" || header.Gname != "root" {
			t.Errorf("%s is not owned by root", name)
		}

		if !header.ModTime.Equal(fixedTime()) {
			t.Errorf("%s has modification time %v, want the fixed timestamp", name, header.ModTime)
		}

		switch header.Typeflag {
		case tar.TypeDir:
			if header.Mode != 0o755 {
				t.Errorf("directory %s has mode %o, want 755", name, header.Mode)
			}
		case tar.TypeReg:
			if header.Mode != 0o644 && header.Mode != 0o755 {
				t.Errorf("file %s has mode %o, want 644 or 755", name, header.Mode)
			}
		default:
			t.Errorf("%s has an unexpected type %v", name, header.Typeflag)
		}
	}

	// An execute bit anywhere in the source makes the file executable for all.
	if got := headers["setup.d/01-setup.sh"].Mode; got != 0o755 {
		t.Errorf("an executable source file published as mode %o, want 755", got)
	}

	// A file with no execute bit is readable by all, not just its owner.
	if got := headers["profile.d/01-path.sh"].Mode; got != 0o644 {
		t.Errorf("a non-executable source file published as mode %o, want 644", got)
	}
}

func TestBuildPackageImage_Reproducible(t *testing.T) {
	files := map[string]os.FileMode{
		"common/setup.d/01-setup.sh": 0o755,
		"linux-amd64/bin/argocd":     0o755,
		"linux-arm64/bin/argocd":     0o755,
	}

	digests := make([]string, 2)

	for i := range digests {
		source := writeSource(t, files)

		if err := os.WriteFile(filepath.Join(source, "package.yaml"), []byte(validManifest), 0o644); err != nil {
			t.Fatal(err)
		}

		build, err := BuildPackageImage(source, SupportedPlatforms, fixedTime())

		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		digest, err := build.Index.Digest()

		if err != nil {
			t.Fatal(err)
		}

		digests[i] = digest.String()
	}

	if digests[0] != digests[1] {
		t.Errorf("two builds of the same source produced different digests:\n  %s\n  %s", digests[0], digests[1])
	}

	// A positive fixed timestamp is required: a negative one makes tar skip
	// every entry and yields an empty layer.
	if fixedTime().Unix() <= 0 {
		t.Fatalf("the fixed timestamp must be positive")
	}
}

func TestBuildPackageImage_RejectsForbiddenEntries(t *testing.T) {
	source := writeSource(t, map[string]os.FileMode{
		"common/setup.d/01-setup.sh": 0o755,
		"linux-amd64/bin/argocd":     0o755,
	})

	if err := os.WriteFile(filepath.Join(source, "package.yaml"), []byte(validManifest), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := os.Symlink("01-setup.sh", filepath.Join(source, "common", "setup.d", "02-link.sh")); err != nil {
		t.Skipf("cannot create a symlink on this platform: %v", err)
	}

	if err := os.Symlink("argocd", filepath.Join(source, "linux-amd64", "bin", "argo")); err != nil {
		t.Fatal(err)
	}

	_, err := BuildPackageImage(source, []Platform{{OS: "linux", Architecture: "amd64"}}, fixedTime())

	if err == nil {
		t.Fatalf("expected the publish to be refused")
	}

	// Every offender is named, not just the first.
	for _, want := range []string{"common/setup.d/02-link.sh", "linux-amd64/bin/argo", "symbolic link"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("expected the error to mention %q, got: %v", want, err)
		}
	}
}

// A hard link is an ordinary file by its mode, so it is caught by its link
// count. It would be published as an independent copy under an image mount
// but dropped by a vendir fetch, which is why the contract refuses it.
func TestBuildPackageImage_RejectsHardLinks(t *testing.T) {
	source := writeSource(t, map[string]os.FileMode{
		"common/bin/real": 0o755,
	})

	if err := os.WriteFile(filepath.Join(source, "package.yaml"), []byte(validManifest), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := os.Link(filepath.Join(source, "common", "bin", "real"), filepath.Join(source, "common", "bin", "linked")); err != nil {
		t.Skipf("cannot create a hard link on this platform: %v", err)
	}

	_, err := BuildPackageImage(source, []Platform{{OS: "linux", Architecture: "amd64"}}, fixedTime())

	if err == nil {
		t.Fatalf("expected a package source containing a hard link to be refused")
	}

	if !strings.Contains(err.Error(), "hard link") {
		t.Errorf("expected the error to name the hard link, got: %v", err)
	}

	// Both names of the linked file are reported, since either could be the
	// one the author did not intend.
	for _, want := range []string{"common/bin/real", "common/bin/linked"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("expected the error to mention %q, got: %v", want, err)
		}
	}
}

// An ordinary file with a single link is published normally.
func TestBuildPackageImage_AcceptsOrdinaryFiles(t *testing.T) {
	source := writeSource(t, map[string]os.FileMode{
		"common/bin/real": 0o755,
	})

	if err := os.WriteFile(filepath.Join(source, "package.yaml"), []byte(validManifest), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := BuildPackageImage(source, []Platform{{OS: "linux", Architecture: "amd64"}}, fixedTime()); err != nil {
		t.Fatalf("an ordinary file should be published: %v", err)
	}
}

func TestBuildPackageImage_RejectsEmptyChild(t *testing.T) {
	source := t.TempDir()

	if err := os.WriteFile(filepath.Join(source, "package.yaml"), []byte(validManifest), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := BuildPackageImage(source, []Platform{{OS: "linux", Architecture: "amd64"}}, fixedTime())

	if err == nil {
		t.Fatalf("expected a source with no files to be refused")
	}

	if !strings.Contains(err.Error(), "only the manifest") {
		t.Errorf("expected the error to explain the package is empty, got: %v", err)
	}
}

func TestBuildPackageImage_IgnoresMacOSArtefactsAndKeepsDotfiles(t *testing.T) {
	source := writeSource(t, map[string]os.FileMode{
		"common/.DS_Store":       0,
		"common/._hidden":        0,
		"common/.keeprc":         0,
		"linux-amd64/bin/argocd": 0o755,
	})

	if err := os.WriteFile(filepath.Join(source, "package.yaml"), []byte(validManifest), 0o644); err != nil {
		t.Fatal(err)
	}

	build, err := BuildPackageImage(source, []Platform{{OS: "linux", Architecture: "amd64"}}, fixedTime())

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	headers := layerEntries(t, build.Children[0].Image)

	for _, name := range []string{".DS_Store", "._hidden"} {
		if _, found := headers[name]; found {
			t.Errorf("%s should have been skipped", name)
		}
	}

	if _, found := headers[".keeprc"]; !found {
		t.Errorf("an ordinary dotfile should be published, entries: %v", keysOf(headers))
	}
}

func TestBuildPackageImage_PlatformOverlaysCommon(t *testing.T) {
	source := writeSource(t, map[string]os.FileMode{
		"common/bin/tool":      0o644,
		"linux-amd64/bin/tool": 0o755,
	})

	if err := os.WriteFile(filepath.Join(source, "package.yaml"), []byte(validManifest), 0o644); err != nil {
		t.Fatal(err)
	}

	build, err := BuildPackageImage(source, []Platform{{OS: "linux", Architecture: "amd64"}}, fixedTime())

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	headers := layerEntries(t, build.Children[0].Image)

	// The platform file wins, so the entry is the executable one.
	if got := headers["bin/tool"].Mode; got != 0o755 {
		t.Errorf("the platform file should win over common, mode %o", got)
	}

	contents := readEntry(t, build.Children[0].Image, "bin/tool")

	if !strings.Contains(contents, "linux-amd64") {
		t.Errorf("expected the platform copy of the file, got %q", contents)
	}
}

func TestBuildPackageImage_ArchIndependentPackage(t *testing.T) {
	source := writeSource(t, map[string]os.FileMode{
		"common/setup.d/01-setup.sh": 0o755,
	})

	if err := os.WriteFile(filepath.Join(source, "package.yaml"), []byte(validManifest), 0o644); err != nil {
		t.Fatal(err)
	}

	build, err := BuildPackageImage(source, SupportedPlatforms, fixedTime())

	if err != nil {
		t.Fatalf("a package with no platform directories should build: %v", err)
	}

	if len(build.Children) != 2 {
		t.Fatalf("expected two children, got %d", len(build.Children))
	}
}

func TestIgnoredRootEntries(t *testing.T) {
	source := writeSource(t, map[string]os.FileMode{
		"common/setup.d/01-setup.sh": 0o755,
		"vendir.yml":                 0,
		"README.md":                  0,
		".DS_Store":                  0,
	})

	if err := os.WriteFile(filepath.Join(source, "package.yaml"), []byte(validManifest), 0o644); err != nil {
		t.Fatal(err)
	}

	ignored, err := IgnoredRootEntries(source)

	if err != nil {
		t.Fatal(err)
	}

	want := []string{"README.md", "vendir.yml"}

	if strings.Join(ignored, ",") != strings.Join(want, ",") {
		t.Errorf("ignored = %v, want %v", ignored, want)
	}
}

func TestParsePlatform(t *testing.T) {
	for _, value := range []string{"linux/amd64", "linux/arm64"} {
		if _, err := ParsePlatform(value); err != nil {
			t.Errorf("%s should be accepted: %v", value, err)
		}
	}

	for _, value := range []string{"linux/arm/v7", "darwin/arm64", "windows/amd64", "amd64", ""} {
		if _, err := ParsePlatform(value); err == nil {
			t.Errorf("%s should be rejected", value)
		}
	}
}

func TestTimestamp(t *testing.T) {
	fixed, err := Timestamp(func(string) string { return "" })

	if err != nil {
		t.Fatal(err)
	}

	if !fixed.Equal(defaultTimestamp) {
		t.Errorf("without SOURCE_DATE_EPOCH the timestamp should be the fixed constant, got %v", fixed)
	}

	if fixed.Unix() <= 0 {
		t.Errorf("the default timestamp must be positive, got %v", fixed.Unix())
	}

	honoured, err := Timestamp(func(string) string { return "1700000000" })

	if err != nil {
		t.Fatal(err)
	}

	if honoured.Unix() != 1700000000 {
		t.Errorf("SOURCE_DATE_EPOCH should be honoured, got %v", honoured.Unix())
	}

	for _, bad := range []string{"0", "-1", "nonsense"} {
		if _, err := Timestamp(func(string) string { return bad }); err == nil {
			t.Errorf("SOURCE_DATE_EPOCH of %q should be rejected", bad)
		}
	}
}

func keysOf(headers map[string]*tar.Header) []string {
	names := make([]string, 0, len(headers))

	for name := range headers {
		names = append(names, name)
	}

	return names
}

func readEntry(t *testing.T, image regv1.Image, name string) string {
	t.Helper()

	layers, err := image.Layers()

	if err != nil {
		t.Fatal(err)
	}

	reader, err := layers[0].Uncompressed()

	if err != nil {
		t.Fatal(err)
	}

	defer reader.Close()

	tarReader := tar.NewReader(reader)

	for {
		header, err := tarReader.Next()

		if err == io.EOF {
			break
		}

		if err != nil {
			t.Fatal(err)
		}

		if header.Name == name {
			buffer := &bytes.Buffer{}

			if _, err := io.Copy(buffer, tarReader); err != nil {
				t.Fatal(err)
			}

			return buffer.String()
		}
	}

	t.Fatalf("%s not found in the layer", name)

	return ""
}
