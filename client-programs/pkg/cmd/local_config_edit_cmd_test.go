package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
)

// ----------------------------------------------------------------------------
// Pure-helper table tests
// ----------------------------------------------------------------------------

func TestStripDoubleHashLines(t *testing.T) {
	cases := []struct {
		name, in, want string
	}{
		{
			name: "drops double-hash, keeps single-hash and content",
			in:   "## tool line\n# user comment\nkey: val\n",
			want: "# user comment\nkey: val\n",
		},
		{
			name: "drops indented double-hash",
			in:   "  ## indented tool line\nkey: val\n",
			want: "key: val\n",
		},
		{
			name: "all double-hash strips to empty",
			in:   "## a\n## b\n",
			want: "",
		},
		{
			name: "single-hash YAML comment is preserved",
			in:   "# yaml-language-server: $schema=x\napiVersion: v\n",
			want: "# yaml-language-server: $schema=x\napiVersion: v\n",
		},
		{
			name: "empty input",
			in:   "",
			want: "",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := string(stripDoubleHashLines([]byte(c.in))); got != c.want {
				t.Errorf("stripDoubleHashLines(%q) = %q, want %q", c.in, got, c.want)
			}
		})
	}
}

func TestCommentEachLine(t *testing.T) {
	cases := []struct {
		name, prefix, in, want string
	}{
		{
			name:   "multi-line error gets every line prefixed",
			prefix: "## ",
			in:     "schema validation failed:\n  - operator.logLevel: must be one of [debug info warn error]",
			want:   "## schema validation failed:\n##   - operator.logLevel: must be one of [debug info warn error]",
		},
		{
			name:   "trailing newline is not turned into a dangling comment",
			prefix: "## ",
			in:     "only line\n",
			want:   "## only line",
		},
		{
			name:   "single line",
			prefix: "# ",
			in:     "x",
			want:   "# x",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := commentEachLine(c.prefix, c.in); got != c.want {
				t.Errorf("commentEachLine(%q, %q) = %q, want %q", c.prefix, c.in, got, c.want)
			}
		})
	}
}

// ----------------------------------------------------------------------------
// Edit-loop integration tests, driven by a scripted fake $EDITOR
// ----------------------------------------------------------------------------

func TestRunLocalConfigEdit_InvalidThenValid(t *testing.T) {
	stubHostIP(t, "192.168.50.151")
	dataHome := t.TempDir()
	t.Setenv("EDUCATES_CLI_DATA_HOME", dataHome)

	invalid := "apiVersion: cli.educates.dev/v1alpha1\n" +
		"kind: EducatesLocalConfig\n" +
		"operator:\n  logLevel: trace\n" // logLevel enum violation

	valid := minimalLocalConfigYAML + "cluster:\n  listenAddress: 10.10.10.1\n\n# edited-marker\n"

	stateDir := stageFakeEditor(t, invalid, valid)

	if err := runLocalConfigEdit(defaultLocalCluster()); err != nil {
		t.Fatalf("edit: %v", err)
	}

	if n := editorInvocations(t, stateDir); n != 2 {
		t.Errorf("editor invoked %d times, want 2 (one reopen on validation error)", n)
	}

	saved, err := os.ReadFile(filepath.Join(dataHome, "config.yaml"))
	if err != nil {
		t.Fatalf("config.yaml not written: %v", err)
	}
	s := string(saved)
	if !strings.Contains(s, "EducatesLocalConfig") {
		t.Errorf("saved config missing kind:\n%s", s)
	}
	if !strings.Contains(s, "# edited-marker") {
		t.Errorf("single-'#' comment was not preserved through the edit loop:\n%s", s)
	}
	if strings.Contains(s, "##") {
		t.Errorf("tool '##' lines leaked into the saved config:\n%s", s)
	}
	if strings.Contains(s, "logLevel: trace") {
		t.Errorf("invalid first-pass content leaked into the saved config:\n%s", s)
	}
	if leftovers := tempLeftovers(t, dataHome); len(leftovers) != 0 {
		t.Errorf("temp files left behind after success: %v", leftovers)
	}
}

func TestRunLocalConfigEdit_ValidFirstPass_Saves(t *testing.T) {
	stubHostIP(t, "192.168.50.151")
	dataHome := t.TempDir()
	t.Setenv("EDUCATES_CLI_DATA_HOME", dataHome)

	valid := minimalLocalConfigYAML + "cluster:\n  listenAddress: 10.10.10.1\n\n# first-pass-marker\n"
	stateDir := stageFakeEditor(t, valid)

	if err := runLocalConfigEdit(defaultLocalCluster()); err != nil {
		t.Fatalf("edit: %v", err)
	}
	if n := editorInvocations(t, stateDir); n != 1 {
		t.Errorf("editor invoked %d times, want 1", n)
	}
	saved, err := os.ReadFile(filepath.Join(dataHome, "config.yaml"))
	if err != nil {
		t.Fatalf("config.yaml not written: %v", err)
	}
	if !strings.Contains(string(saved), "# first-pass-marker") {
		t.Errorf("expected saved marker, got:\n%s", saved)
	}
	if leftovers := tempLeftovers(t, dataHome); len(leftovers) != 0 {
		t.Errorf("temp files left behind after success: %v", leftovers)
	}
}

func TestRunLocalConfigEdit_QuitNoChanges_DoesNotCreateFile(t *testing.T) {
	stubHostIP(t, "192.168.50.151")
	dataHome := t.TempDir()
	t.Setenv("EDUCATES_CLI_DATA_HOME", dataHome)

	// No staged steps: the fake editor leaves the buffer untouched, simulating
	// a quit-without-saving on a pristine data home.
	stateDir := stageFakeEditor(t)

	if err := runLocalConfigEdit(defaultLocalCluster()); err != nil {
		t.Fatalf("expected nil on no-change quit, got %v", err)
	}
	if n := editorInvocations(t, stateDir); n != 1 {
		t.Errorf("editor invoked %d times, want 1", n)
	}
	if _, err := os.Stat(filepath.Join(dataHome, "config.yaml")); !os.IsNotExist(err) {
		t.Errorf("config.yaml should not be created on a no-change quit (stat err = %v)", err)
	}
	if leftovers := tempLeftovers(t, dataHome); len(leftovers) != 0 {
		t.Errorf("temp files left behind: %v", leftovers)
	}
}

func TestRunLocalConfigEdit_AbortAfterError_PreservesTemp(t *testing.T) {
	stubHostIP(t, "192.168.50.151")
	dataHome := t.TempDir()
	t.Setenv("EDUCATES_CLI_DATA_HOME", dataHome)

	invalid := "apiVersion: cli.educates.dev/v1alpha1\n" +
		"kind: EducatesLocalConfig\n" +
		"operator:\n  logLevel: trace\n"

	// step1 invalid; no step2 → the user "quits" the reopened editor without
	// saving, which should abort and preserve their work.
	stateDir := stageFakeEditor(t, invalid)

	err := runLocalConfigEdit(defaultLocalCluster())
	if err == nil {
		t.Fatal("expected an abort error after invalid edit followed by quit")
	}
	if !strings.Contains(err.Error(), "cancelled") {
		t.Errorf("abort error %q should mention 'cancelled'", err)
	}
	if n := editorInvocations(t, stateDir); n != 2 {
		t.Errorf("editor invoked %d times, want 2", n)
	}
	if _, statErr := os.Stat(filepath.Join(dataHome, "config.yaml")); !os.IsNotExist(statErr) {
		t.Errorf("config.yaml should not exist after an aborted edit")
	}

	leftovers := tempLeftovers(t, dataHome)
	if len(leftovers) != 1 {
		t.Fatalf("expected exactly one preserved recovery temp file, got %v", leftovers)
	}
	recovered, err := os.ReadFile(leftovers[0])
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(recovered), "logLevel: trace") {
		t.Errorf("recovery temp file missing the user's content:\n%s", recovered)
	}
}

// ----------------------------------------------------------------------------
// Test helpers
// ----------------------------------------------------------------------------

// stageFakeEditor writes a small POSIX-sh script that acts as $EDITOR and
// points $EDITOR/$VISUAL at it. On its Nth invocation the script overwrites the
// file it is given with the contents of the Nth `steps` entry; when there is no
// corresponding step it leaves the file untouched (simulating quit-without-
// saving). An invocation counter is kept on disk so tests can assert how many
// times the editor was reopened. Returns the state directory.
func stageFakeEditor(t *testing.T, steps ...string) (stateDir string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("fake-editor tests rely on a POSIX-sh script; skipped on windows")
	}
	stateDir = t.TempDir()
	for i, content := range steps {
		p := filepath.Join(stateDir, fmt.Sprintf("step%d", i+1))
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	script := filepath.Join(stateDir, "editor.sh")
	body := "#!/bin/sh\n" +
		"set -eu\n" +
		"target=\"$1\"\n" +
		"n=$(cat \"$FAKE_EDIT_DIR/n\" 2>/dev/null || echo 0)\n" +
		"n=$((n + 1))\n" +
		"printf '%s' \"$n\" > \"$FAKE_EDIT_DIR/n\"\n" +
		"step=\"$FAKE_EDIT_DIR/step$n\"\n" +
		"if [ -f \"$step\" ]; then cat \"$step\" > \"$target\"; fi\n"
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	// WriteFile's mode is masked by umask; make the exec bit explicit.
	if err := os.Chmod(script, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("FAKE_EDIT_DIR", stateDir)
	t.Setenv("VISUAL", "") // ensure resolveEditor falls through to EDITOR
	t.Setenv("EDITOR", script)
	return stateDir
}

// editorInvocations reports how many times the fake editor ran.
func editorInvocations(t *testing.T, stateDir string) int {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(stateDir, "n"))
	if err != nil {
		return 0
	}
	n, _ := strconv.Atoi(strings.TrimSpace(string(b)))
	return n
}

// tempLeftovers returns any config.yaml.<pid> recovery temp files in dataHome.
func tempLeftovers(t *testing.T, dataHome string) []string {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join(dataHome, "config.yaml.*"))
	if err != nil {
		t.Fatal(err)
	}
	return matches
}
