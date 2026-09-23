package packages

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func testManifest() *Manifest {
	return &Manifest{
		APIVersion: manifestAPIVersion,
		Kind:       manifestKind,
		Name:       "argocd",
		Version:    "v2.10.6",
	}
}

// The reference is derived from the manifest unless a flag overrides it, and
// none of the flags rewrites the manifest.
func TestPublishOptions_Reference(t *testing.T) {
	cases := []struct {
		name    string
		options PublishOptions
		want    string
	}{
		{
			name:    "derived from the manifest",
			options: PublishOptions{},
			want:    "localhost:5001/argocd:v2.10.6",
		},
		{
			name:    "a repository replaces the default",
			options: PublishOptions{ImageRepository: "ghcr.io/myorg"},
			want:    "ghcr.io/myorg/argocd:v2.10.6",
		},
		{
			name:    "a trailing slash on the repository is tolerated",
			options: PublishOptions{ImageRepository: "ghcr.io/myorg/"},
			want:    "ghcr.io/myorg/argocd:v2.10.6",
		},
		{
			name:    "a tag replaces the manifest version",
			options: PublishOptions{ImageTag: "sha-5f9081f"},
			want:    "localhost:5001/argocd:sha-5f9081f",
		},
		{
			name:    "a repository and a tag combine",
			options: PublishOptions{ImageRepository: "ghcr.io/myorg", ImageTag: "sha-5f9081f"},
			want:    "ghcr.io/myorg/argocd:sha-5f9081f",
		},
		{
			name:    "a full reference wins over the repository and the tag",
			options: PublishOptions{Image: "example.com/other/name:v1", ImageRepository: "ghcr.io/myorg", ImageTag: "ignored"},
			want:    "example.com/other/name:v1",
		},
	}

	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			manifest := testManifest()

			reference, err := test.options.reference(manifest)

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			// Name() reports the fully qualified form, which differs from what
			// an author writes, so compare the readable form.
			got := reference.Context().RepositoryStr()
			if registry := reference.Context().RegistryStr(); registry != "" {
				got = registry + "/" + got
			}
			got = got + ":" + reference.TagStr()

			if got != test.want {
				t.Errorf("reference = %q, want %q", got, test.want)
			}

			// The manifest is never rewritten by a flag.
			if manifest.Name != "argocd" || manifest.Version != "v2.10.6" {
				t.Errorf("the manifest was modified: %+v", manifest)
			}
		})
	}
}

func TestPublishOptions_ReferenceRejectsGarbage(t *testing.T) {
	options := PublishOptions{Image: "NOT A REFERENCE"}

	if _, err := options.reference(testManifest()); err == nil {
		t.Fatalf("expected an unparseable reference to be refused")
	}
}

func TestPublishOptions_Platforms(t *testing.T) {
	cases := []struct {
		name           string
		platforms      []string
		want           []string
		singlePlatform bool
		wantErr        bool
	}{
		{
			name: "no flag builds every supported platform",
			want: []string{"linux/amd64", "linux/arm64"},
		},
		{
			name:           "one platform is reported as single",
			platforms:      []string{"linux/arm64"},
			want:           []string{"linux/arm64"},
			singlePlatform: true,
		},
		{
			name:      "both platforms named explicitly",
			platforms: []string{"linux/amd64", "linux/arm64"},
			want:      []string{"linux/amd64", "linux/arm64"},
		},
		{
			name:           "a repeated platform is not built twice",
			platforms:      []string{"linux/arm64", "linux/arm64"},
			want:           []string{"linux/arm64"},
			singlePlatform: true,
		},
		{
			name:      "surrounding space is tolerated",
			platforms: []string{" linux/amd64 "},
			want:      []string{"linux/amd64"},

			singlePlatform: true,
		},
		{
			name:      "an unsupported platform is refused",
			platforms: []string{"linux/arm/v7"},
			wantErr:   true,
		},
	}

	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			options := PublishOptions{Platforms: test.platforms}

			platforms, singlePlatform, err := options.platforms()

			if test.wantErr {
				if err == nil {
					t.Fatalf("expected an error")
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			var got []string

			for _, platform := range platforms {
				got = append(got, platform.String())
			}

			if strings.Join(got, ",") != strings.Join(test.want, ",") {
				t.Errorf("platforms = %v, want %v", got, test.want)
			}

			if singlePlatform != test.singlePlatform {
				t.Errorf("singlePlatform = %v, want %v", singlePlatform, test.singlePlatform)
			}
		})
	}
}

func TestPublishOptions_WriteDigestFile(t *testing.T) {
	source := writeSource(t, map[string]os.FileMode{
		"common/setup.d/01-setup.sh": 0o755,
	})

	if err := os.WriteFile(filepath.Join(source, "package.yaml"), []byte(validManifest), 0o644); err != nil {
		t.Fatal(err)
	}

	build, err := BuildPackageImage(source, SupportedPlatforms, fixedTime())

	if err != nil {
		t.Fatal(err)
	}

	path := filepath.Join(t.TempDir(), "digest.txt")

	options := PublishOptions{DigestFile: path}

	if err := options.writeDigestFile(build); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	contents, err := os.ReadFile(path)

	if err != nil {
		t.Fatal(err)
	}

	digest, err := build.Index.Digest()

	if err != nil {
		t.Fatal(err)
	}

	if strings.TrimSpace(string(contents)) != digest.String() {
		t.Errorf("digest file holds %q, want %q", strings.TrimSpace(string(contents)), digest.String())
	}

	// With no path set nothing is written and nothing fails.
	if err := (&PublishOptions{}).writeDigestFile(build); err != nil {
		t.Errorf("unexpected error with no digest file: %v", err)
	}
}

// A dry run reports the same digests it would publish, and contacts nothing.
func TestPublishOptions_DryRunReportsDigests(t *testing.T) {
	source := writeSource(t, map[string]os.FileMode{
		"common/setup.d/01-setup.sh": 0o755,
		"linux-amd64/bin/argocd":     0o755,
		"linux-arm64/bin/argocd":     0o755,
		"README.md":                  0,
	})

	if err := os.WriteFile(filepath.Join(source, "package.yaml"), []byte(validManifest), 0o644); err != nil {
		t.Fatal(err)
	}

	options := PublishOptions{
		Path:   source,
		DryRun: true,
		// A registry which does not exist: a dry run must not reach for it.
		ImageRepository: "registry.invalid:5000",
	}

	output := &strings.Builder{}

	if err := options.Publish(output); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	report := output.String()

	build, err := BuildPackageImage(source, SupportedPlatforms, fixedTime())

	if err != nil {
		t.Fatal(err)
	}

	digest, err := build.Index.Digest()

	if err != nil {
		t.Fatal(err)
	}

	for _, want := range []string{
		"Dry run, nothing was published.",
		"registry.invalid:5000/argocd:v2.10.6",
		digest.String(),
		"linux/amd64",
		"linux/arm64",
		"Ignored in the package source: README.md",
	} {
		if !strings.Contains(report, want) {
			t.Errorf("expected the report to mention %q, got:\n%s", want, report)
		}
	}

	if strings.Contains(report, "Warning:") {
		t.Errorf("a two platform publish should not warn, got:\n%s", report)
	}
}

func TestPublishOptions_SinglePlatformWarns(t *testing.T) {
	source := writeSource(t, map[string]os.FileMode{
		"common/setup.d/01-setup.sh": 0o755,
	})

	if err := os.WriteFile(filepath.Join(source, "package.yaml"), []byte(validManifest), 0o644); err != nil {
		t.Fatal(err)
	}

	options := PublishOptions{Path: source, DryRun: true, Platforms: []string{"linux/arm64"}}

	output := &strings.Builder{}

	if err := options.Publish(output); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(output.String(), "Warning: only one platform") {
		t.Errorf("expected a warning about the single platform, got:\n%s", output.String())
	}
}

func TestPublishOptions_SourceDir(t *testing.T) {
	if _, err := (&PublishOptions{Path: filepath.Join(t.TempDir(), "absent")}).sourceDir(); err == nil {
		t.Errorf("expected a missing directory to be refused")
	}

	file := filepath.Join(t.TempDir(), "afile")

	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := (&PublishOptions{Path: file}).sourceDir(); err == nil {
		t.Errorf("expected a path which is not a directory to be refused")
	}
}
