package packages

import (
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	regname "github.com/google/go-containerregistry/pkg/name"
)

func writeTestFile(t *testing.T, path string, contents string) {
	t.Helper()

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestLoadFetchConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "packages.yaml")

	writeTestFile(t, path, `apiVersion: packages.educates.dev/v1alpha1
kind: PackageFetch
packages:
- path: /opt/packages/argocd
  image: ghcr.io/educates/packages/argocd:v2.10.6
- path: /opt/packages/private
  image: registry.example.com/packages/private:v1
  secretRef: registry-credentials
`)

	config, err := LoadFetchConfig(path)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(config.Packages) != 2 {
		t.Fatalf("expected two packages, got %d", len(config.Packages))
	}

	if config.Packages[0].Path != "/opt/packages/argocd" {
		t.Errorf("unexpected path %q", config.Packages[0].Path)
	}

	if config.Packages[1].SecretRef != "registry-credentials" {
		t.Errorf("unexpected secret reference %q", config.Packages[1].SecretRef)
	}
}

func TestLoadFetchConfig_Rejections(t *testing.T) {
	cases := []struct {
		name    string
		config  string
		wantErr string
	}{
		{
			name:    "wrong api version",
			config:  "apiVersion: other/v1\nkind: PackageFetch\npackages: []\n",
			wantErr: "apiVersion",
		},
		{
			name:    "wrong kind",
			config:  "apiVersion: packages.educates.dev/v1alpha1\nkind: Other\npackages: []\n",
			wantErr: "kind",
		},
		{
			name:    "unknown field",
			config:  "apiVersion: packages.educates.dev/v1alpha1\nkind: PackageFetch\nnonsense: true\npackages: []\n",
			wantErr: "unable to parse",
		},
		{
			name:    "package with no image",
			config:  "apiVersion: packages.educates.dev/v1alpha1\nkind: PackageFetch\npackages:\n- path: /opt/packages/x\n",
			wantErr: "needs a path and an image",
		},
	}

	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "packages.yaml")
			writeTestFile(t, path, test.config)

			_, err := LoadFetchConfig(path)

			if err == nil {
				t.Fatalf("expected an error")
			}

			if !strings.Contains(err.Error(), test.wantErr) {
				t.Errorf("expected the error to mention %q, got: %v", test.wantErr, err)
			}
		})
	}
}

// secretYAML builds the Kubernetes Secret document the session manager mounts.
func secretYAML(name string, secretType string, data map[string]string) string {
	builder := &strings.Builder{}

	fmt.Fprintf(builder, "apiVersion: v1\nkind: Secret\nmetadata:\n  name: %s\ntype: %s\ndata:\n", name, secretType)

	for key, value := range data {
		fmt.Fprintf(builder, "  %s: %s\n", key, base64.StdEncoding.EncodeToString([]byte(value)))
	}

	return builder.String()
}

func TestLoadKeychain_DockerConfigSecret(t *testing.T) {
	dir := t.TempDir()

	payload := `{"auths":{"registry.example.com":{"username":"alice","password":"opensesame"}}}`

	writeTestFile(t, filepath.Join(dir, "registry-credentials.yaml"),
		secretYAML("registry-credentials", "kubernetes.io/dockerconfigjson",
			map[string]string{".dockerconfigjson": payload}))

	keychain, err := LoadKeychain(dir)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	reference, err := regname.ParseReference("registry.example.com/packages/private:v1")

	if err != nil {
		t.Fatal(err)
	}

	authenticator, err := keychain.Resolve(reference, "registry-credentials")

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	config, err := authenticator.Authorization()

	if err != nil {
		t.Fatal(err)
	}

	if config.Username != "alice" || config.Password != "opensesame" {
		t.Errorf("unexpected credentials %+v", config)
	}
}

// The docker configuration may carry the credentials as a single encoded
// field instead of a username and password pair.
func TestLoadKeychain_DockerConfigAuthField(t *testing.T) {
	dir := t.TempDir()

	encoded := base64.StdEncoding.EncodeToString([]byte("bob:hunter2"))
	payload := fmt.Sprintf(`{"auths":{"registry.example.com":{"auth":%q}}}`, encoded)

	writeTestFile(t, filepath.Join(dir, "creds.yaml"),
		secretYAML("creds", "kubernetes.io/dockerconfigjson",
			map[string]string{".dockerconfigjson": payload}))

	keychain, err := LoadKeychain(dir)

	if err != nil {
		t.Fatal(err)
	}

	reference, err := regname.ParseReference("registry.example.com/packages/argocd:v1")

	if err != nil {
		t.Fatal(err)
	}

	authenticator, err := keychain.Resolve(reference, "creds")

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	config, _ := authenticator.Authorization()

	if config.Username != "bob" || config.Password != "hunter2" {
		t.Errorf("unexpected credentials %+v", config)
	}
}

// The plain form names a host alongside a username and password.
func TestLoadKeychain_PlainSecret(t *testing.T) {
	dir := t.TempDir()

	writeTestFile(t, filepath.Join(dir, "plain.yaml"),
		secretYAML("plain", "Opaque", map[string]string{
			"username": "carol",
			"password": "letmein",
			"hostname": "registry.example.com",
		}))

	keychain, err := LoadKeychain(dir)

	if err != nil {
		t.Fatal(err)
	}

	reference, err := regname.ParseReference("registry.example.com/packages/argocd:v1")

	if err != nil {
		t.Fatal(err)
	}

	authenticator, err := keychain.Resolve(reference, "plain")

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	config, _ := authenticator.Authorization()

	if config.Username != "carol" {
		t.Errorf("unexpected credentials %+v", config)
	}
}

// A secret naming no host applies to whatever it was referenced for.
func TestLoadKeychain_SecretWithoutHostname(t *testing.T) {
	dir := t.TempDir()

	writeTestFile(t, filepath.Join(dir, "nohost.yaml"),
		secretYAML("nohost", "Opaque", map[string]string{
			"username": "dave",
			"password": "secret",
		}))

	keychain, err := LoadKeychain(dir)

	if err != nil {
		t.Fatal(err)
	}

	reference, err := regname.ParseReference("anything.example.com/packages/argocd:v1")

	if err != nil {
		t.Fatal(err)
	}

	if _, err := keychain.Resolve(reference, "nohost"); err != nil {
		t.Errorf("a secret with no hostname should apply: %v", err)
	}
}

func TestKeychain_MissingSecretIsReported(t *testing.T) {
	keychain, err := LoadKeychain(t.TempDir())

	if err != nil {
		t.Fatal(err)
	}

	reference, err := regname.ParseReference("registry.example.com/packages/argocd:v1")

	if err != nil {
		t.Fatal(err)
	}

	_, err = keychain.Resolve(reference, "absent")

	if err == nil {
		t.Fatalf("expected a missing secret to be reported")
	}

	if !strings.Contains(err.Error(), "absent") {
		t.Errorf("expected the error to name the secret, got: %v", err)
	}
}

// A session with no private packages mounts no secrets at all.
func TestLoadKeychain_MissingDirectoryIsNotAnError(t *testing.T) {
	keychain, err := LoadKeychain(filepath.Join(t.TempDir(), "absent"))

	if err != nil {
		t.Fatalf("a missing secrets directory should be tolerated: %v", err)
	}

	reference, err := regname.ParseReference("ghcr.io/educates/argocd:v1")

	if err != nil {
		t.Fatal(err)
	}

	// With no secret named, the ambient configuration is consulted, which
	// succeeds even when it holds nothing.
	if _, err := keychain.Resolve(reference, ""); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

// Fetching replaces the package directory, so a configuration naming a path
// outside the packages root is refused before anything is deleted.
func TestLoadFetchConfig_RefusesPathsOutsideThePackagesRoot(t *testing.T) {
	for _, path := range []string{
		"/",
		"/opt",
		"/opt/packages",
		"/etc/passwd",
		"/opt/packages/argocd/nested",
		"/opt/packages/../../etc",
		"opt/packages/argocd",
		"relative",
	} {
		config := "apiVersion: packages.educates.dev/v1alpha1\nkind: PackageFetch\npackages:\n" +
			"- path: " + path + "\n  image: ghcr.io/educates/packages/argocd:v1\n"

		file := filepath.Join(t.TempDir(), "packages.yaml")
		writeTestFile(t, file, config)

		if _, err := LoadFetchConfig(file); err == nil {
			t.Errorf("the path %q should be refused", path)
		}
	}
}

func TestLoadFetchConfig_AcceptsAPackageDirectory(t *testing.T) {
	config := "apiVersion: packages.educates.dev/v1alpha1\nkind: PackageFetch\npackages:\n" +
		"- path: /opt/packages/argocd\n  image: ghcr.io/educates/packages/argocd:v1\n"

	file := filepath.Join(t.TempDir(), "packages.yaml")
	writeTestFile(t, file, config)

	if _, err := LoadFetchConfig(file); err != nil {
		t.Errorf("a package directory should be accepted: %v", err)
	}
}

func TestSecurePath(t *testing.T) {
	target := filepath.Join(t.TempDir(), "packages", "argocd")

	if _, err := securePath(target, "bin/argocd"); err != nil {
		t.Errorf("an ordinary path should be accepted: %v", err)
	}

	for _, name := range []string{"../escape", "../../etc/passwd", "bin/../../escape"} {
		if _, err := securePath(target, name); err == nil {
			t.Errorf("%q should be refused", name)
		}
	}
}
