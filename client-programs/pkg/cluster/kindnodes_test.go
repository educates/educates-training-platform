package cluster

import (
	"archive/tar"
	"bytes"
	"strings"
	"testing"
)

// versionArchive returns a tar archive holding one entry named version of
// type typeflag with content, as Docker copies /kind/version out of a node
// container.
func versionArchive(t *testing.T, typeflag byte, content string) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	header := &tar.Header{Name: "version", Typeflag: typeflag, Mode: 0o644, Size: int64(len(content))}
	if typeflag == tar.TypeDir {
		header.Size = 0
	}
	if err := tw.WriteHeader(header); err != nil {
		t.Fatal(err)
	}
	if typeflag == tar.TypeReg {
		if _, err := tw.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	return &buf
}

func TestKindVersionFromArchive(t *testing.T) {
	for _, content := range []string{"v1.36.4", "v1.36.4\n", "  v1.36.4\r\n"} {
		got, err := kindVersionFromArchive(versionArchive(t, tar.TypeReg, content))
		if err != nil || got != "v1.36.4" {
			t.Errorf("kindVersionFromArchive(%q) = %q, %v, want v1.36.4", content, got, err)
		}
	}
}

func TestKindVersionFromArchiveRefusesWhatHoldsNoVersion(t *testing.T) {
	for name, archive := range map[string]*bytes.Buffer{
		"an empty archive":  {},
		"an empty file":     versionArchive(t, tar.TypeReg, " \n"),
		"a directory":       versionArchive(t, tar.TypeDir, ""),
		"a truncated entry": bytes.NewBufferString(strings.Repeat("x", 100)),
	} {
		if got, err := kindVersionFromArchive(archive); err == nil {
			t.Errorf("kindVersionFromArchive(%s) = %q, want an error", name, got)
		}
	}
}
