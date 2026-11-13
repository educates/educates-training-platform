package cmd

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/educates/educates-training-platform/client-programs/pkg/config/hostinfo"
)

// TestLocalConfigView_Effective asserts the default view prints only the
// effective configuration with the CLI defaults materialised, carries the
// yaml-language-server modeline, and points at --raw for the file as
// written. An empty config falls back to a nip.io domain, which defaults
// to an insecure plain-HTTP install, so those defaults must show up.
func TestLocalConfigView_Effective(t *testing.T) {
	dataHome := t.TempDir()
	if err := os.WriteFile(filepath.Join(dataHome, "config.yaml"), []byte(emptyLocal), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("EDUCATES_CLI_DATA_HOME", dataHome)

	p := ProjectInfo{Version: "test", ImageRepository: "ghcr.io/educates"}
	var buf bytes.Buffer
	if err := p.runLocalConfigView(&LocalConfigViewOptions{}, defaultLocalCluster(), &buf); err != nil {
		t.Fatalf("runLocalConfigView: %v", err)
	}
	s := buf.String()

	if !strings.Contains(s, "# yaml-language-server: $schema=") {
		t.Errorf("effective view should carry the yaml-language-server modeline:\n%s", s)
	}
	if !strings.Contains(s, "educates local config view --raw") {
		t.Errorf("effective view should point at --raw for the file as written:\n%s", s)
	}
	if !strings.Contains(s, "(file: "+filepath.Join(dataHome, "config.yaml")) {
		t.Errorf("effective view should show the configuration file location:\n%s", s)
	}
	// Static defaults are environment-independent and must always appear.
	for _, want := range []string{"clusterAdmin: true", "lookupService: true", "logLevel: info"} {
		if !strings.Contains(s, want) {
			t.Errorf("effective configuration missing default %q:\n%s", want, s)
		}
	}
	// The nip.io + insecure defaulting only resolves when a host IP is
	// detectable; assert it when it is.
	if _, err := hostinfo.DetectHostIP(); err == nil {
		if !strings.Contains(s, ".nip.io") || !strings.Contains(s, "insecure: true") {
			t.Errorf("effective view should show the nip.io + insecure default:\n%s", s)
		}
	}
}

// TestLocalConfigView_Raw asserts --raw prints the file exactly as
// written and nothing else (no effective section, no explanatory header).
func TestLocalConfigView_Raw(t *testing.T) {
	dataHome := t.TempDir()
	raw := "# yaml-language-server: $schema=x\n" + emptyLocal
	if err := os.WriteFile(filepath.Join(dataHome, "config.yaml"), []byte(raw), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("EDUCATES_CLI_DATA_HOME", dataHome)

	p := ProjectInfo{Version: "test", ImageRepository: "ghcr.io/educates"}
	var buf bytes.Buffer
	if err := p.runLocalConfigView(&LocalConfigViewOptions{Raw: true}, defaultLocalCluster(), &buf); err != nil {
		t.Fatalf("runLocalConfigView --raw: %v", err)
	}
	if got := buf.String(); got != raw {
		t.Errorf("--raw should print the file verbatim.\n got: %q\nwant: %q", got, raw)
	}
}

// TestLocalConfigView_FullyDefaulted_SkipsComment asserts that when the
// file already carries every default (as init --defaults writes), view
// skips the explanatory comment and its output equals view --raw.
func TestLocalConfigView_FullyDefaulted_SkipsComment(t *testing.T) {
	dataHome := t.TempDir()
	t.Setenv("EDUCATES_CLI_DATA_HOME", dataHome)
	p := ProjectInfo{Version: "test", ImageRepository: "ghcr.io/educates"}

	if err := p.runLocalConfigInit(&LocalConfigInitOptions{Defaults: true}, defaultLocalCluster(), io.Discard); err != nil {
		t.Fatalf("init --defaults: %v", err)
	}

	var def, raw bytes.Buffer
	if err := p.runLocalConfigView(&LocalConfigViewOptions{}, defaultLocalCluster(), &def); err != nil {
		t.Fatalf("view: %v", err)
	}
	if err := p.runLocalConfigView(&LocalConfigViewOptions{Raw: true}, defaultLocalCluster(), &raw); err != nil {
		t.Fatalf("view --raw: %v", err)
	}

	if strings.Contains(def.String(), "Effective EducatesLocalConfig") {
		t.Errorf("view of a fully-defaulted file should skip the defaults comment:\n%s", def.String())
	}
	if def.String() != raw.String() {
		t.Errorf("view of a fully-defaulted file should equal view --raw.\n--- view ---\n%s\n--- raw ---\n%s", def.String(), raw.String())
	}
}

// TestLocalConfigInit_Defaults_WritesEffective asserts init --defaults
// writes a fully-defaulted, schema-valid file carrying the modeline and
// the static defaults.
func TestLocalConfigInit_Defaults_WritesEffective(t *testing.T) {
	dataHome := t.TempDir()
	t.Setenv("EDUCATES_CLI_DATA_HOME", dataHome)

	p := ProjectInfo{Version: "test", ImageRepository: "ghcr.io/educates"}
	var buf bytes.Buffer
	if err := p.runLocalConfigInit(&LocalConfigInitOptions{Defaults: true}, defaultLocalCluster(), &buf); err != nil {
		t.Fatalf("init --defaults: %v", err)
	}
	body, err := os.ReadFile(filepath.Join(dataHome, "config.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	s := string(body)
	for _, want := range []string{
		"# yaml-language-server: $schema=",
		"kind: EducatesLocalConfig",
		"clusterAdmin: true",
		"logLevel: info",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("init --defaults output missing %q:\n%s", want, s)
		}
	}
}
