package config

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/educates/educates-training-platform/client-programs/pkg/config/v1alpha1"
)

const mirrorsHeader = "# yaml-language-server: $schema=https://schemas.educates.dev/cli/v1alpha1/EducatesLocalMirrors.json\n" +
	"apiVersion: cli.educates.dev/v1alpha1\nkind: EducatesLocalMirrors\n"

func TestLocalMirrorsPathIsMirrorsYAMLInTheDataHome(t *testing.T) {
	if got, want := LocalMirrorsPath("/home/me/.local/share/educates"), "/home/me/.local/share/educates/mirrors.yaml"; got != want {
		t.Errorf("LocalMirrorsPath() = %q, want %q", got, want)
	}
}

func TestLoadLocalMirrorsReturnsTheMirrorsTheFileDeclares(t *testing.T) {
	dataHome := t.TempDir()
	must(t, os.WriteFile(filepath.Join(dataHome, "mirrors.yaml"), []byte(mirrorsHeader+
		"registryMirrors:\n"+
		"- mirror: docker.io\n  url: https://registry-1.docker.io\n  username: alice\n  password: secret\n"+
		"- mirror: ghcr.io\n  port: \"5002\"\n  bindIP: 127.0.0.1\n"), 0o644))

	got, err := LoadLocalMirrors(dataHome)
	if err != nil {
		t.Fatalf("LoadLocalMirrors: %v", err)
	}

	want := []v1alpha1.RegistryMirror{
		{Mirror: "docker.io", URL: "https://registry-1.docker.io", Username: "alice", Password: "secret"},
		{Mirror: "ghcr.io", Port: "5002", BindIP: "127.0.0.1"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("LoadLocalMirrors() = %+v, want %+v", got, want)
	}
}

func TestLoadLocalMirrorsOfADataHomeWithoutTheFileDeclaresNone(t *testing.T) {
	got, err := LoadLocalMirrors(t.TempDir())
	if err != nil {
		t.Fatalf("LoadLocalMirrors: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("LoadLocalMirrors() = %+v, want none", got)
	}
}

func TestLoadLocalMirrorsOfAFileWithoutMirrorsDeclaresNone(t *testing.T) {
	dataHome := t.TempDir()
	must(t, os.WriteFile(filepath.Join(dataHome, "mirrors.yaml"), []byte(mirrorsHeader), 0o644))

	got, err := LoadLocalMirrors(dataHome)
	if err != nil {
		t.Fatalf("LoadLocalMirrors: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("LoadLocalMirrors() = %+v, want none", got)
	}
}

func TestLoadLocalMirrorsOfAFileThatDoesNotLoadNamesItsPathAndWhy(t *testing.T) {
	cases := []struct {
		name string
		body string
		why  []string
	}{
		{
			name: "an unknown field",
			body: mirrorsHeader + "registryMirrors:\n- mirror: docker.io\n  proxy: true\n",
			why:  []string{"schema validation failed", "proxy"},
		},
		{
			name: "a mirror without its name",
			body: mirrorsHeader + "registryMirrors:\n- url: https://registry-1.docker.io\n",
			why:  []string{"schema validation failed", "mirror"},
		},
		{
			name: "another kind",
			body: "apiVersion: cli.educates.dev/v1alpha1\nkind: EducatesLocalConfig\n",
			why:  []string{`kind "EducatesLocalConfig"`, "EducatesLocalMirrors"},
		},
		{
			name: "no kind",
			body: "registryMirrors:\n- mirror: docker.io\n",
			why:  []string{"apiVersion", "kind"},
		},
		{
			name: "another apiVersion",
			body: "apiVersion: cli.educates.dev/v1beta1\nkind: EducatesLocalMirrors\n",
			why:  []string{`apiVersion "cli.educates.dev/v1beta1"`},
		},
		{
			name: "a mirror declared twice",
			body: mirrorsHeader + "registryMirrors:\n- mirror: docker.io\n- mirror: ghcr.io\n- mirror: docker.io\n  username: alice\n",
			why:  []string{"docker.io", "more than once"},
		},
		{
			name: "not YAML",
			body: "registryMirrors: [\n",
			why:  []string{"parse YAML"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dataHome := t.TempDir()
			path := filepath.Join(dataHome, "mirrors.yaml")
			must(t, os.WriteFile(path, []byte(tc.body), 0o644))

			_, err := LoadLocalMirrors(dataHome)

			if err == nil {
				t.Fatal("LoadLocalMirrors() = nil error, want one")
			}
			for _, want := range append([]string{path}, tc.why...) {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("LoadLocalMirrors() error %q does not mention %q", err, want)
				}
			}
		})
	}
}

// Recording a mirror in a data home without the registry mirrors file
// creates it, with the modeline, apiVersion and kind, readable only by
// the user since it can hold passwords.
func TestRecordLocalMirrorCreatesTheFileWhenMissing(t *testing.T) {
	dataHome := filepath.Join(t.TempDir(), "educates")

	err := RecordLocalMirror(dataHome, v1alpha1.RegistryMirror{Mirror: "docker.io", Username: "alice", Password: "secret"})
	if err != nil {
		t.Fatalf("RecordLocalMirror: %v", err)
	}

	path := filepath.Join(dataHome, "mirrors.yaml")
	want := mirrorsHeader + "registryMirrors:\n  - mirror: docker.io\n    username: alice\n    password: secret\n"
	if got := string(readFile(t, path)); got != want {
		t.Errorf("mirrors.yaml =\n%s\nwant\n%s", got, want)
	}
	assertMode(t, path, 0o600)
}

// handEditedMirrors is a registry mirrors file as a user writes it, with
// comments.
const handEditedMirrors = mirrorsHeader +
	"# Mirrors for my laptop.\n" +
	"registryMirrors:\n" +
	"  # GitHub's registry.\n" +
	"  - mirror: ghcr.io\n" +
	"  - mirror: docker.io\n" +
	"    url: registry-1.docker.io # Docker Hub's own host\n" +
	"    username: alice\n" +
	"    password: old\n"

// Recording a mirror the file has no entry for adds one at the end, and
// recording one it has replaces that entry where it is; everything else
// in the file stays as it was, comments included.
func TestRecordLocalMirrorKeepsTheRestOfTheFile(t *testing.T) {
	dataHome := t.TempDir()
	path := filepath.Join(dataHome, "mirrors.yaml")
	must(t, os.WriteFile(path, []byte(handEditedMirrors), 0o644))

	must(t, RecordLocalMirror(dataHome, v1alpha1.RegistryMirror{Mirror: "quay.io"}))
	must(t, RecordLocalMirror(dataHome, v1alpha1.RegistryMirror{
		Mirror: "docker.io", URL: "registry-1.docker.io", Username: "alice", Password: "1234"}))

	want := mirrorsHeader +
		"# Mirrors for my laptop.\n" +
		"registryMirrors:\n" +
		"  # GitHub's registry.\n" +
		"  - mirror: ghcr.io\n" +
		"  - mirror: docker.io\n" +
		"    url: registry-1.docker.io # Docker Hub's own host\n" +
		"    username: alice\n" +
		"    password: \"1234\"\n" +
		"  - mirror: quay.io\n"
	if got := string(readFile(t, path)); got != want {
		t.Errorf("mirrors.yaml =\n%s\nwant\n%s", got, want)
	}
	assertMode(t, path, 0o600)
}

// Removing a mirror's entry leaves the rest of the file as it was, and
// reports whether the file recorded the mirror.
func TestRemoveLocalMirrorKeepsTheRestOfTheFile(t *testing.T) {
	dataHome := t.TempDir()
	path := filepath.Join(dataHome, "mirrors.yaml")
	must(t, os.WriteFile(path, []byte(handEditedMirrors), 0o644))

	removed, err := RemoveLocalMirror(dataHome, "docker.io")
	if err != nil || !removed {
		t.Fatalf("RemoveLocalMirror(docker.io) = %v, %v, want true, nil", removed, err)
	}
	removed, err = RemoveLocalMirror(dataHome, "quay.io")
	if err != nil || removed {
		t.Fatalf("RemoveLocalMirror(quay.io) = %v, %v, want false, nil", removed, err)
	}

	want := mirrorsHeader +
		"# Mirrors for my laptop.\n" +
		"registryMirrors:\n" +
		"  # GitHub's registry.\n" +
		"  - mirror: ghcr.io\n"
	if got := string(readFile(t, path)); got != want {
		t.Errorf("mirrors.yaml =\n%s\nwant\n%s", got, want)
	}
}

// Removing a mirror from a data home without the registry mirrors file
// records none and leaves the file missing.
func TestRemoveLocalMirrorWithoutTheFile(t *testing.T) {
	dataHome := t.TempDir()

	removed, err := RemoveLocalMirror(dataHome, "docker.io")

	if err != nil || removed {
		t.Fatalf("RemoveLocalMirror() = %v, %v, want false, nil", removed, err)
	}
	if _, err := os.Stat(filepath.Join(dataHome, "mirrors.yaml")); !os.IsNotExist(err) {
		t.Errorf("mirrors.yaml exists after removing a mirror from a data home without it: %v", err)
	}
}

// A registry mirrors file that doesn't load is left as it is, and the
// writer's error names it and why.
func TestLocalMirrorsWriterRefusesAFileThatDoesNotLoad(t *testing.T) {
	dataHome := t.TempDir()
	path := filepath.Join(dataHome, "mirrors.yaml")
	body := mirrorsHeader + "registryMirrors:\n- mirror: docker.io\n  proxy: true\n"
	must(t, os.WriteFile(path, []byte(body), 0o644))

	recordErr := RecordLocalMirror(dataHome, v1alpha1.RegistryMirror{Mirror: "quay.io"})
	_, removeErr := RemoveLocalMirror(dataHome, "docker.io")

	for name, err := range map[string]error{"RecordLocalMirror": recordErr, "RemoveLocalMirror": removeErr} {
		if err == nil || !strings.Contains(err.Error(), path) || !strings.Contains(err.Error(), "proxy") {
			t.Errorf("%s() error = %v, want one naming %s and the field proxy", name, err, path)
		}
	}
	if got := string(readFile(t, path)); got != body {
		t.Errorf("mirrors.yaml changed to\n%s", got)
	}
}

// assertMode fails t unless the file at path has the permissions want.
func assertMode(t *testing.T, path string, want os.FileMode) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != want {
		t.Errorf("%s mode = %o, want %o", filepath.Base(path), got, want)
	}
}
