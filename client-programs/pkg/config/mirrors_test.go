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
