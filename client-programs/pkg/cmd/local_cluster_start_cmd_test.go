package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/educates/educates-training-platform/client-programs/pkg/config/v1alpha1"
)

// writeStartConfig writes body as the config of the local cluster whose
// config path is path.
func writeStartConfig(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestDeclaredRegistryMirrorsAreThoseTheClusterConfigDeclares(t *testing.T) {
	_, id := namedClusterForTest(t)
	writeStartConfig(t, id.ConfigPath, "apiVersion: cli.educates.dev/v1alpha1\nkind: EducatesLocalConfig\n"+
		"cluster:\n  listenAddress: 10.10.10.2\n  registryMirrors:\n"+
		"  - mirror: docker.io\n    username: alice\n    password: secret\n")
	var out bytes.Buffer

	got := declaredRegistryMirrors(&out, id)

	want := []v1alpha1.RegistryMirror{{Mirror: "docker.io", Username: "alice", Password: "secret"}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("declaredRegistryMirrors() = %+v, want %+v", got, want)
	}
	if out.Len() != 0 {
		t.Errorf("declaredRegistryMirrors() wrote %q, want nothing", out.String())
	}
}

func TestDeclaredRegistryMirrorsOfAClusterWithoutAConfigAreNone(t *testing.T) {
	_, id := namedClusterForTest(t)
	var out bytes.Buffer

	if got := declaredRegistryMirrors(&out, id); len(got) != 0 {
		t.Errorf("declaredRegistryMirrors() = %+v, want none", got)
	}
	if out.Len() != 0 {
		t.Errorf("declaredRegistryMirrors() wrote %q, want nothing", out.String())
	}
}

func TestDeclaredRegistryMirrorsOfAConfigThatDoesNotLoadWarn(t *testing.T) {
	_, id := namedClusterForTest(t)
	writeStartConfig(t, id.ConfigPath, "apiVersion: cli.educates.dev/v1alpha1\nkind: EducatesLocalConfig\n"+
		"cluster:\n  listenAddress: 127.0.0.1\n  registryMirrors:\n  - mirror: docker.io\n")
	var out bytes.Buffer

	got := declaredRegistryMirrors(&out, id)

	if len(got) != 0 {
		t.Errorf("declaredRegistryMirrors() = %+v, want none", got)
	}
	for _, want := range []string{"Warning:", `local cluster "test2"`, "registry mirrors", "loopback"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("declaredRegistryMirrors() wrote %q, want it to mention %q", out.String(), want)
		}
	}
}
