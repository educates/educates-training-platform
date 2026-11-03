package cmd

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLocalConfigInit_WritesDefault(t *testing.T) {
	t.Setenv("EDUCATES_CLI_DATA_HOME", t.TempDir())

	o := &LocalConfigInitOptions{}
	if err := (&ProjectInfo{}).runLocalConfigInit(o, io.Discard); err != nil {
		t.Fatalf("init: %v", err)
	}

	body, err := os.ReadFile(filepath.Join(os.Getenv("EDUCATES_CLI_DATA_HOME"), "config.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	s := string(body)
	for _, want := range []string{"apiVersion: cli.educates.dev/v1alpha1", "kind: EducatesLocalConfig"} {
		if !strings.Contains(s, want) {
			t.Errorf("init output missing %q:\n%s", want, s)
		}
	}
}

func TestLocalConfigInit_ExistingFile_ErrorsWithoutForce(t *testing.T) {
	dataHome := t.TempDir()
	t.Setenv("EDUCATES_CLI_DATA_HOME", dataHome)
	if err := os.WriteFile(filepath.Join(dataHome, "config.yaml"), []byte("existing"), 0o644); err != nil {
		t.Fatal(err)
	}

	o := &LocalConfigInitOptions{}
	err := (&ProjectInfo{}).runLocalConfigInit(o, io.Discard)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "already exists") {
		t.Errorf("error %q does not mention 'already exists'", err)
	}
}

func TestLocalConfigInit_Force_Overwrites(t *testing.T) {
	dataHome := t.TempDir()
	t.Setenv("EDUCATES_CLI_DATA_HOME", dataHome)
	if err := os.WriteFile(filepath.Join(dataHome, "config.yaml"), []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	o := &LocalConfigInitOptions{Force: true}
	if err := (&ProjectInfo{}).runLocalConfigInit(o, io.Discard); err != nil {
		t.Fatal(err)
	}
	body, _ := os.ReadFile(filepath.Join(dataHome, "config.yaml"))
	if !strings.Contains(string(body), "EducatesLocalConfig") {
		t.Errorf("--force did not overwrite: %q", string(body))
	}
}

func TestLocalConfigSet_ScalarRoundTrip(t *testing.T) {
	dataHome := stageInitConfig(t)
	var buf bytes.Buffer
	if err := runLocalConfigSet(&buf, "ingress.domain", "workshop.test"); err != nil {
		t.Fatalf("set: %v", err)
	}
	body, _ := os.ReadFile(filepath.Join(dataHome, "config.yaml"))
	if !strings.Contains(string(body), "domain: workshop.test") {
		t.Errorf("file missing the set value:\n%s", body)
	}

	// Round-trip through get.
	var out bytes.Buffer
	if err := runLocalConfigGet(&out, "ingress.domain"); err != nil {
		t.Fatalf("get: %v", err)
	}
	if got := strings.TrimSpace(out.String()); got != "workshop.test" {
		t.Errorf("get ingress.domain = %q, want %q", got, "workshop.test")
	}
}

// TestLocalConfigSet_PreservesModelineAndOrder is the regression guard
// for set rewriting the file through a map (which dropped comments and
// sorted keys alphabetically). The yaml-language-server modeline and the
// original, non-alphabetical key order must survive the edit.
func TestLocalConfigSet_PreservesModelineAndOrder(t *testing.T) {
	dataHome := t.TempDir()
	t.Setenv("EDUCATES_CLI_DATA_HOME", dataHome)
	original := "# yaml-language-server: $schema=https://schemas.educates.dev/cli/v1alpha1/EducatesLocalConfig.json\n" +
		"apiVersion: cli.educates.dev/v1alpha1\n" +
		"kind: EducatesLocalConfig\n" +
		"cluster:\n  listenAddress: 0.0.0.0\n" +
		"ingress:\n  domain: insecure.educates.test\n" +
		"operator:\n  logLevel: debug\n"
	if err := os.WriteFile(filepath.Join(dataHome, "config.yaml"), []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	if err := runLocalConfigSet(&buf, "ingress.insecure", "true"); err != nil {
		t.Fatalf("set: %v", err)
	}
	s := func() string { b, _ := os.ReadFile(filepath.Join(dataHome, "config.yaml")); return string(b) }()

	if !strings.Contains(s, "# yaml-language-server: $schema=") {
		t.Errorf("set dropped the yaml-language-server modeline:\n%s", s)
	}
	if !strings.Contains(s, "insecure: true") {
		t.Errorf("set did not write the value:\n%s", s)
	}
	// Original order has kind before cluster; an alphabetical map sort
	// would have put cluster, ingress before kind. Asserting this order
	// holds proves comments+order were preserved rather than re-sorted.
	last := -1
	for _, key := range []string{"apiVersion:", "kind:", "cluster:", "ingress:", "operator:"} {
		idx := strings.Index(s, key)
		if idx < 0 {
			t.Fatalf("missing key %q in:\n%s", key, s)
		}
		if idx < last {
			t.Errorf("key %q out of original order (alphabetical scramble?):\n%s", key, s)
		}
		last = idx
	}
}

func TestLocalConfigSet_CoercesBoolAndInt(t *testing.T) {
	stageInitConfig(t)
	if err := runLocalConfigSet(io.Discard, "lookupService", "false"); err != nil {
		t.Fatalf("set bool: %v", err)
	}
	if err := runLocalConfigSet(io.Discard, "cluster.apiServer.port", "6443"); err != nil {
		t.Fatalf("set int: %v", err)
	}

	// get should return scalar form.
	var b1, b2 bytes.Buffer
	_ = runLocalConfigGet(&b1, "lookupService")
	_ = runLocalConfigGet(&b2, "cluster.apiServer.port")
	if got := strings.TrimSpace(b1.String()); got != "false" {
		t.Errorf("lookupService get = %q, want %q", got, "false")
	}
	if got := strings.TrimSpace(b2.String()); got != "6443" {
		t.Errorf("apiServer.port get = %q, want %q", got, "6443")
	}
}

func TestLocalConfigSet_SchemaViolation_Errors(t *testing.T) {
	stageInitConfig(t)
	// logLevel enum is [debug, info, warn, error]; "trace" rejected.
	err := runLocalConfigSet(io.Discard, "operator.logLevel", "trace")
	if err == nil {
		t.Fatal("expected schema rejection, got nil")
	}
	if !strings.Contains(err.Error(), "logLevel") {
		t.Errorf("error %q does not mention the field path", err)
	}
}

func TestLocalConfigSet_UnknownField_Errors(t *testing.T) {
	stageInitConfig(t)
	err := runLocalConfigSet(io.Discard, "bogus", "x")
	if err == nil {
		t.Fatal("expected schema rejection")
	}
	if !strings.Contains(err.Error(), "bogus") {
		t.Errorf("error %q does not mention the unknown field", err)
	}
}

func TestLocalConfigSet_ImagePrePullerEnabled_RoundTrips(t *testing.T) {
	stageInitConfig(t)
	if err := runLocalConfigSet(io.Discard, "imagePrePuller.enabled", "false"); err != nil {
		t.Fatalf("set: %v", err)
	}
	var b bytes.Buffer
	if err := runLocalConfigGet(&b, "imagePrePuller.enabled"); err != nil {
		t.Fatalf("get: %v", err)
	}
	if got := strings.TrimSpace(b.String()); got != "false" {
		t.Errorf("imagePrePuller.enabled get = %q, want %q", got, "false")
	}
}

// set reports the boolean form of imagePrePuller, whether the file
// already holds it or the set would write it, by naming the object form.
func TestLocalConfigSet_ImagePrePullerBooleanForm_NamesObjectForm(t *testing.T) {
	cases := []struct {
		name     string
		fileHas  string
		path     string
		value    string
		wantForm string
	}{
		{"file holds the boolean", "imagePrePuller: true\n", "imagePrePuller.enabled", "false", "imagePrePuller: {enabled: true}"},
		{"set writes the boolean", "", "imagePrePuller", "false", "imagePrePuller: {enabled: false}"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dataHome := stageInitConfig(t)
			if tc.fileHas != "" {
				appendToConfig(t, dataHome, tc.fileHas)
			}
			err := runLocalConfigSet(io.Discard, tc.path, tc.value)
			if err == nil {
				t.Fatal("expected an error, got nil")
			}
			for _, want := range []string{"imagePrePuller.enabled", tc.wantForm} {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("error %q does not contain %q", err, want)
				}
			}
			for _, noise := range []string{"is not a map", "Invalid type"} {
				if strings.Contains(err.Error(), noise) {
					t.Errorf("error %q reports only %q", err, noise)
				}
			}
		})
	}
}

// set validates the whole file with the readable message of the rule the
// change breaks.
func TestLocalConfigSet_ImagePrePullerNothingToPull_Rejected(t *testing.T) {
	stageInitConfig(t)
	err := runLocalConfigSet(io.Discard, "imagePrePuller.includeDefaultImages", "false")
	if err == nil {
		t.Fatal("expected schema rejection, got nil")
	}
	want := "imagePrePuller: with enabled true, its default, and includeDefaultImages false, images must list at least one entry"
	if !strings.Contains(err.Error(), want) {
		t.Errorf("error %q does not contain %q", err, want)
	}
}

// appendToConfig appends text to the staged config.yaml.
func appendToConfig(t *testing.T, dataHome, text string) {
	t.Helper()
	f, err := os.OpenFile(filepath.Join(dataHome, "config.yaml"), os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if _, err := f.WriteString(text); err != nil {
		t.Fatal(err)
	}
}

func TestLocalConfigGet_MissingPath_Errors(t *testing.T) {
	stageInitConfig(t)
	err := runLocalConfigGet(io.Discard, "ingress.domain")
	if err == nil {
		t.Fatal("expected 'path not found' error on empty config")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("error %q does not say 'not found'", err)
	}
}

func TestLocalConfigGet_FullFile_NoArg(t *testing.T) {
	stageInitConfig(t)
	var buf bytes.Buffer
	if err := runLocalConfigGet(&buf, ""); err != nil {
		t.Fatalf("get (no arg): %v", err)
	}
	if !strings.Contains(buf.String(), "EducatesLocalConfig") {
		t.Errorf("expected full file output, got: %s", buf.String())
	}
}

// stageInitConfig sets up $EDUCATES_CLI_DATA_HOME with a freshly init'd
// EducatesLocalConfig at config.yaml. Returns the data home path.
func stageInitConfig(t *testing.T) string {
	t.Helper()
	dataHome := t.TempDir()
	t.Setenv("EDUCATES_CLI_DATA_HOME", dataHome)
	if err := (&ProjectInfo{}).runLocalConfigInit(&LocalConfigInitOptions{}, io.Discard); err != nil {
		t.Fatal(err)
	}
	return dataHome
}
