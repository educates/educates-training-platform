package config

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const localConfigHeader = "# yaml-language-server: $schema=https://schemas.educates.dev/cli/v1alpha1/EducatesLocalConfig.json\n" +
	"apiVersion: cli.educates.dev/v1alpha1\nkind: EducatesLocalConfig\n"

// configWithMirrors is a default config, as a user wrote it before
// registry mirrors moved to mirrors.yaml.
const configWithMirrors = localConfigHeader +
	"cluster:\n" +
	"  # My laptop's alias.\n" +
	"  listenAddress: 10.10.10.1\n" +
	"  registryMirrors:\n" +
	"    - mirror: ghcr.io\n" +
	"    - mirror: docker.io\n" +
	"      username: alice\n" +
	"      password: secret\n" +
	"ingress:\n" +
	"  domain: educates.test # resolved by the local resolver\n"

// configWithoutMirrors is configWithMirrors once its registry mirrors
// are moved.
const configWithoutMirrors = localConfigHeader +
	"cluster:\n" +
	"  # My laptop's alias.\n" +
	"  listenAddress: 10.10.10.1\n" +
	"ingress:\n" +
	"  domain: educates.test # resolved by the local resolver\n"

// writeDataHomeFile writes body to the file rel of the data home
// dataHome, creating its folder, and returns its path.
func writeDataHomeFile(t *testing.T, dataHome, rel, body string) string {
	t.Helper()
	path := filepath.Join(dataHome, rel)
	must(t, os.MkdirAll(filepath.Dir(path), 0o755))
	must(t, os.WriteFile(path, []byte(body), 0o644))
	return path
}

func TestMoveClusterRegistryMirrorsOfTheDefaultConfig(t *testing.T) {
	dataHome := t.TempDir()
	path := writeDataHomeFile(t, dataHome, "config.yaml", configWithMirrors)
	var note bytes.Buffer

	if err := MoveClusterRegistryMirrors(dataHome, path, &note); err != nil {
		t.Fatalf("MoveClusterRegistryMirrors: %v", err)
	}

	mirrorsPath := filepath.Join(dataHome, "mirrors.yaml")
	wantMirrors := mirrorsHeader + "registryMirrors:\n" +
		"  - mirror: ghcr.io\n" +
		"  - mirror: docker.io\n    username: alice\n    password: secret\n"
	if got := string(readFile(t, mirrorsPath)); got != wantMirrors {
		t.Errorf("mirrors.yaml =\n%s\nwant\n%s", got, wantMirrors)
	}
	if got := string(readFile(t, path)); got != configWithoutMirrors {
		t.Errorf("config.yaml =\n%s\nwant\n%s", got, configWithoutMirrors)
	}
	backup := path + ".bak"
	if got := string(readFile(t, backup)); got != configWithMirrors {
		t.Errorf("config.yaml.bak =\n%s\nwant the previous config", got)
	}
	assertMode(t, backup, 0o600)
	assertMode(t, path, 0o644)
	for _, want := range []string{"moved cluster.registryMirrors of " + path, "to " + mirrorsPath,
		"ghcr.io, docker.io", backup} {
		if !strings.Contains(note.String(), want) {
			t.Errorf("note does not mention %q:\n%s", want, note.String())
		}
	}
	if strings.Contains(note.String(), "secret") {
		t.Errorf("note gives a mirror's password:\n%s", note.String())
	}
	if strings.Count(note.String(), "\n") != 1 {
		t.Errorf("note is not one line:\n%s", note.String())
	}
}

// Loading a config of the data home moves its registry mirrors first, so
// it loads.
func TestLoadMovesTheRegistryMirrorsOfAConfigInTheDataHome(t *testing.T) {
	dataHome := t.TempDir()
	t.Setenv("EDUCATES_CLI_DATA_HOME", dataHome)
	path := writeDataHomeFile(t, dataHome, "config.yaml", configWithMirrors)

	if _, err := Load(path); err != nil {
		t.Fatalf("Load: %v", err)
	}

	if got := string(readFile(t, path)); got != configWithoutMirrors {
		t.Errorf("config.yaml =\n%s\nwant\n%s", got, configWithoutMirrors)
	}
	if mirrors, err := LoadLocalMirrors(dataHome); err != nil || len(mirrors) != 2 {
		t.Errorf("mirrors.yaml records %+v, %v, want ghcr.io and docker.io", mirrors, err)
	}
}

func TestMoveClusterRegistryMirrorsOfANamedClusterConfigAndAnEducatesConfig(t *testing.T) {
	cases := []struct {
		name, body, field string
	}{
		{
			name:  "an EducatesLocalConfig",
			body:  configWithMirrors,
			field: "cluster.registryMirrors",
		},
		{
			name: "an EducatesConfig",
			body: "apiVersion: cli.educates.dev/v1alpha1\nkind: EducatesConfig\n" +
				"target:\n  provider: kind\n  cluster:\n    listenAddress: 10.10.10.2\n" +
				"    registryMirrors:\n      - mirror: ghcr.io\n",
			field: "target.cluster.registryMirrors",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dataHome := t.TempDir()
			path := writeDataHomeFile(t, dataHome, filepath.Join("clusters", "test2", "config.yaml"), tc.body)
			var note bytes.Buffer

			if err := MoveClusterRegistryMirrors(dataHome, path, &note); err != nil {
				t.Fatalf("MoveClusterRegistryMirrors: %v", err)
			}

			if got := string(readFile(t, path)); strings.Contains(got, "registryMirrors") {
				t.Errorf("config still declares registry mirrors:\n%s", got)
			}
			if mirrors, err := LoadLocalMirrors(dataHome); err != nil || len(mirrors) == 0 || mirrors[0].Mirror != "ghcr.io" {
				t.Errorf("mirrors.yaml records %+v, %v, want ghcr.io first", mirrors, err)
			}
			if !strings.Contains(note.String(), "moved "+tc.field+" of "+path) {
				t.Errorf("note does not name %s of %s:\n%s", tc.field, path, note.String())
			}
		})
	}
}

// A config anywhere but where the default and named local clusters keep
// theirs in the data home is never rewritten.
func TestMoveClusterRegistryMirrorsLeavesAnyOtherConfig(t *testing.T) {
	dataHome := t.TempDir()
	for _, rel := range []string{"elsewhere/config.yaml", "clusters/test2/other.yaml", "my-config.yaml"} {
		t.Run(rel, func(t *testing.T) {
			path := writeDataHomeFile(t, dataHome, rel, configWithMirrors)
			outside := writeDataHomeFile(t, t.TempDir(), "config.yaml", configWithMirrors)
			var note bytes.Buffer

			for _, p := range []string{path, outside} {
				if err := MoveClusterRegistryMirrors(dataHome, p, &note); err != nil {
					t.Fatalf("MoveClusterRegistryMirrors(%s): %v", p, err)
				}
				if got := string(readFile(t, p)); got != configWithMirrors {
					t.Errorf("%s was rewritten:\n%s", p, got)
				}
				if _, err := os.Stat(p + ".bak"); !os.IsNotExist(err) {
					t.Errorf("%s was backed up: %v", p, err)
				}
			}
			if note.Len() != 0 {
				t.Errorf("note written for a config that did not move:\n%s", note.String())
			}
			if _, err := os.Stat(filepath.Join(dataHome, "mirrors.yaml")); !os.IsNotExist(err) {
				t.Errorf("mirrors.yaml was written: %v", err)
			}
		})
	}
}

// An existing backup is never overwritten: the previous config is kept
// in a backup named with the time instead.
func TestMoveClusterRegistryMirrorsKeepsAnExistingBackup(t *testing.T) {
	dataHome := t.TempDir()
	path := writeDataHomeFile(t, dataHome, "config.yaml", configWithMirrors)
	writeDataHomeFile(t, dataHome, "config.yaml.bak", "an older backup\n")
	var note bytes.Buffer

	if err := MoveClusterRegistryMirrors(dataHome, path, &note); err != nil {
		t.Fatalf("MoveClusterRegistryMirrors: %v", err)
	}

	if got := string(readFile(t, path+".bak")); got != "an older backup\n" {
		t.Errorf("config.yaml.bak was overwritten:\n%s", got)
	}
	backups, err := filepath.Glob(path + ".*.bak")
	if err != nil || len(backups) != 1 {
		t.Fatalf("backups named with the time = %v, %v, want one", backups, err)
	}
	if got := string(readFile(t, backups[0])); got != configWithMirrors {
		t.Errorf("%s =\n%s\nwant the previous config", backups[0], got)
	}
	if !strings.Contains(note.String(), backups[0]) {
		t.Errorf("note does not name the backup %s:\n%s", backups[0], note.String())
	}
}

// A mirror mirrors.yaml already records with the same URL and username is
// skipped, so the file's entry, its password included, stays as it was.
func TestMoveClusterRegistryMirrorsSkipsAMirrorRecordedWithTheSameValues(t *testing.T) {
	dataHome := t.TempDir()
	path := writeDataHomeFile(t, dataHome, "config.yaml", configWithMirrors)
	recorded := mirrorsHeader + "registryMirrors:\n  # Docker Hub, with my token.\n" +
		"  - mirror: docker.io\n    username: alice\n    password: token\n"
	writeDataHomeFile(t, dataHome, "mirrors.yaml", recorded)
	var note bytes.Buffer

	if err := MoveClusterRegistryMirrors(dataHome, path, &note); err != nil {
		t.Fatalf("MoveClusterRegistryMirrors: %v", err)
	}

	want := recorded + "  - mirror: ghcr.io\n"
	if got := string(readFile(t, filepath.Join(dataHome, "mirrors.yaml"))); got != want {
		t.Errorf("mirrors.yaml =\n%s\nwant\n%s", got, want)
	}
	if got := string(readFile(t, path)); got != configWithoutMirrors {
		t.Errorf("config.yaml =\n%s\nwant\n%s", got, configWithoutMirrors)
	}
	if !strings.Contains(note.String(), "already declared docker.io") {
		t.Errorf("note does not say docker.io was already declared:\n%s", note.String())
	}
}

// A mirror mirrors.yaml records with another URL or username stops the
// move before anything is written, with an error naming the conflict.
func TestMoveClusterRegistryMirrorsStopsOnAMirrorRecordedWithOtherValues(t *testing.T) {
	dataHome := t.TempDir()
	path := writeDataHomeFile(t, dataHome, "config.yaml", configWithMirrors)
	recorded := mirrorsHeader + "registryMirrors:\n  - mirror: docker.io\n    username: bob\n    password: token\n"
	writeDataHomeFile(t, dataHome, "mirrors.yaml", recorded)
	var note bytes.Buffer

	err := MoveClusterRegistryMirrors(dataHome, path, &note)

	if err == nil {
		t.Fatal("MoveClusterRegistryMirrors() = nil, want an error")
	}
	mirrorsPath := filepath.Join(dataHome, "mirrors.yaml")
	for _, want := range []string{path + ": cluster.registryMirrors was removed", "every local cluster",
		mirrorsPath + " records registry mirror docker.io with username bob", "declares it with username alice",
		"educates local mirror delete", "left as it is"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not mention %q", err, want)
		}
	}
	for _, password := range []string{"secret", "token"} {
		if strings.Contains(err.Error(), password) {
			t.Errorf("error %q gives a password", err)
		}
	}
	if got := string(readFile(t, path)); got != configWithMirrors {
		t.Errorf("config.yaml was rewritten:\n%s", got)
	}
	if got := string(readFile(t, mirrorsPath)); got != recorded {
		t.Errorf("mirrors.yaml was rewritten:\n%s", got)
	}
	if _, err := os.Stat(path + ".bak"); !os.IsNotExist(err) {
		t.Errorf("config.yaml was backed up: %v", err)
	}
	if note.Len() != 0 {
		t.Errorf("note written for a move that stopped:\n%s", note.String())
	}
}
