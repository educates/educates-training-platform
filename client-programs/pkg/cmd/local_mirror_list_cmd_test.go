package cmd

import (
	"bytes"
	"strings"
	"testing"

	"github.com/educates/educates-training-platform/client-programs/pkg/config/v1alpha1"
	"github.com/educates/educates-training-platform/client-programs/pkg/registry"
)

// mirrorListRow returns the line of out, the output of writeMirrorList,
// that starts with the mirror mirror, with its columns split.
func mirrorListRow(t *testing.T, out, mirror string) []string {
	t.Helper()
	for _, line := range strings.Split(out, "\n") {
		if fields := strings.Fields(line); len(fields) > 0 && fields[0] == mirror {
			return fields
		}
	}
	t.Fatalf("no row for mirror %s in:\n%s", mirror, out)
	return nil
}

func TestMirrorListSaysWhichMirrorsTheMirrorsFileDeclares(t *testing.T) {
	containers := []registry.RegistryMirror{
		{Name: "docker.io", URL: "registry-1.docker.io", Username: "alice", Status: "running", ContainerName: "educates-mirror-docker.io"},
		{Name: "quay.io", URL: "quay.io", Status: "exited", ContainerName: "educates-mirror-quay.io"},
	}
	declared := []v1alpha1.RegistryMirror{
		{Mirror: "docker.io", URL: "registry-1.docker.io", Username: "alice", Password: "secret"},
		{Mirror: "gcr.io", URL: "mirror.gcr.io", Username: "dave", Password: "hunter2"},
		{Mirror: "ghcr.io"},
	}
	var out bytes.Buffer

	writeMirrorList(&out, containers, declared)

	got := out.String()
	if header := mirrorListRow(t, got, "NAME"); header[len(header)-1] != "DECLARED" {
		t.Errorf("header = %v, want a last column DECLARED", header)
	}
	for mirror, want := range map[string][]string{
		"docker.io": {"docker.io", "registry-1.docker.io", "alice", "running", "educates-mirror-docker.io", "yes"},
		"quay.io":   {"quay.io", "quay.io", "exited", "educates-mirror-quay.io", "no"},
		"gcr.io":    {"gcr.io", "mirror.gcr.io", "dave", "missing", "yes"},
		"ghcr.io":   {"ghcr.io", "ghcr.io", "missing", "yes"},
	} {
		if row := mirrorListRow(t, got, mirror); strings.Join(row, " ") != strings.Join(want, " ") {
			t.Errorf("row of %s = %v, want %v", mirror, row, want)
		}
	}
	for _, password := range []string{"secret", "hunter2"} {
		if strings.Contains(got, password) {
			t.Errorf("mirror list gives the password %q:\n%s", password, got)
		}
	}
}

func TestMirrorListOfNoMirror(t *testing.T) {
	var out bytes.Buffer

	writeMirrorList(&out, nil, nil)

	if got, want := out.String(), "No mirrors found.\n"; got != want {
		t.Errorf("writeMirrorList() wrote %q, want %q", got, want)
	}
}
