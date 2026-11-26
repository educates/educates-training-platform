package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/educates/educates-training-platform/client-programs/pkg/config/v1alpha1"
)

func TestMarshalLocalConfig(t *testing.T) {
	local := func(cluster v1alpha1.LocalClusterConfig, ingress v1alpha1.LocalIngressConfig) *v1alpha1.EducatesLocalConfig {
		return &v1alpha1.EducatesLocalConfig{
			TypeMeta: v1alpha1.TypeMeta{APIVersion: v1alpha1.APIVersion, Kind: v1alpha1.KindEducatesLocalConfig},
			Cluster:  cluster,
			Ingress:  ingress,
		}
	}
	const header = "# yaml-language-server: $schema=https://schemas.educates.dev/cli/v1alpha1/EducatesLocalConfig.json\n" +
		"apiVersion: cli.educates.dev/v1alpha1\nkind: EducatesLocalConfig\n"
	cases := []struct {
		name string
		cfg  *v1alpha1.EducatesLocalConfig
		want string
	}{
		{
			name: "a cluster setting and domain given",
			cfg: local(v1alpha1.LocalClusterConfig{ApiServer: v1alpha1.ApiServerConfig{Port: 6443}},
				v1alpha1.LocalIngressConfig{Domain: "educates.test"}),
			want: header + "cluster:\n  apiServer:\n    port: 6443\ningress:\n  domain: educates.test\n",
		},
		{
			name: "nothing given",
			cfg:  local(v1alpha1.LocalClusterConfig{}, v1alpha1.LocalIngressConfig{}),
			want: header,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := MarshalLocalConfig(tc.cfg)
			if err != nil {
				t.Fatalf("MarshalLocalConfig: %v", err)
			}
			if string(got) != tc.want {
				t.Errorf("MarshalLocalConfig =\n%s\nwant:\n%s", got, tc.want)
			}
		})
	}
}

func TestMissingLocalConfigError_V3ValuesPresent(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "values.yaml"), []byte("clusterInfrastructure:\n  provider: kind\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	err := MissingLocalConfigError(dir)
	if err == nil {
		t.Fatal("expected error")
	}
	s := err.Error()
	for _, want := range []string{"v3-style values.yaml", "migration shim", "EducatesLocalConfig"} {
		if !strings.Contains(s, want) {
			t.Errorf("error missing hint %q in:\n%s", want, s)
		}
	}
}

func TestMissingLocalConfigError_FirstTimeUser(t *testing.T) {
	// Point at a path that does NOT exist.
	dir := filepath.Join(t.TempDir(), "does-not-exist")

	err := MissingLocalConfigError(dir)
	if err == nil {
		t.Fatal("expected error")
	}
	s := err.Error()
	for _, want := range []string{"no Educates data home found", "First-time setup", "local config init"} {
		if !strings.Contains(s, want) {
			t.Errorf("error missing hint %q in:\n%s", want, s)
		}
	}
}

func TestMissingLocalConfigError_DirExistsConfigMissing(t *testing.T) {
	dir := t.TempDir()
	// Drop a sibling subdir to look like a partially-used data home.
	if err := os.MkdirAll(filepath.Join(dir, "secrets"), 0o755); err != nil {
		t.Fatal(err)
	}

	err := MissingLocalConfigError(dir)
	if err == nil {
		t.Fatal("expected error")
	}
	s := err.Error()
	for _, want := range []string{"data home directory exists but config.yaml is missing", "local config init"} {
		if !strings.Contains(s, want) {
			t.Errorf("error missing hint %q in:\n%s", want, s)
		}
	}
	if strings.Contains(s, "v3-style values.yaml") {
		t.Errorf("should not mention v3 migration when no values.yaml present:\n%s", s)
	}
}

func TestMissingLocalConfigError_ConfigExists_InternalError(t *testing.T) {
	// Caller misuse: config.yaml is present, so this function should not
	// have been called. Surface a detectable error.
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "config.yaml"), []byte(""), 0o644); err != nil {
		t.Fatal(err)
	}
	err := MissingLocalConfigError(dir)
	if err == nil || !strings.Contains(err.Error(), "internal") {
		t.Errorf("expected 'internal:' error for misuse, got %v", err)
	}
}
