package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/educates/educates-training-platform/client-programs/pkg/config/v1alpha1"
	"github.com/educates/educates-training-platform/client-programs/pkg/localclusters"
)

// stageLocalClusterConfigs writes, into a fresh data home it returns, the
// default local cluster's config with domain default.test and the named
// local cluster test2's config with domain test2.test. Named cluster test3
// has no config.
func stageLocalClusterConfigs(t *testing.T) string {
	t.Helper()
	dataHome := t.TempDir()
	t.Setenv("EDUCATES_CLI_DATA_HOME", dataHome)
	if err := os.WriteFile(filepath.Join(dataHome, "config.yaml"), []byte(
		"apiVersion: cli.educates.dev/v1alpha1\nkind: EducatesLocalConfig\ningress:\n  domain: default.test\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	test2, err := localclusters.Named("test2", dataHome)
	if err != nil {
		t.Fatal(err)
	}
	writeNamedConfig(t, test2, "apiVersion: cli.educates.dev/v1alpha1\nkind: EducatesLocalConfig\n"+
		"cluster:\n  listenAddress: 10.10.10.2\ningress:\n  domain: test2.test\n")
	return dataHome
}

func ingressDomainOf(t *testing.T, cfg v1alpha1.Config) string {
	t.Helper()
	local, ok := cfg.(*v1alpha1.EducatesLocalConfig)
	if !ok {
		t.Fatalf("loaded a %s, want an EducatesLocalConfig", cfg.GetKind())
	}
	return local.Ingress.Domain
}

func TestPlatformConfig_ReadsTheSelectedLocalClustersConfig(t *testing.T) {
	file := writeFixture(t, "apiVersion: cli.educates.dev/v1alpha1\nkind: EducatesLocalConfig\ningress:\n  domain: file.test\n")
	cases := []struct {
		name        string
		configFile  string
		localConfig bool
		sel         localclusters.Selection
		wantPath    string
		wantDomain  string
	}{
		{
			name:        "--local-config without a selection reads the default cluster's config",
			localConfig: true,
			wantPath:    "config.yaml",
			wantDomain:  "default.test",
		},
		{
			name:        "--local-config with the environment variable reads the named cluster's config",
			localConfig: true,
			sel:         localclusters.Selection{Env: "test2"},
			wantPath:    "clusters/test2/config.yaml",
			wantDomain:  "test2.test",
		},
		{
			name:        "--local-config with --local-cluster reads the named cluster's config",
			localConfig: true,
			sel:         localclusters.Selection{FlagSet: true, Flag: "test2"},
			wantPath:    "clusters/test2/config.yaml",
			wantDomain:  "test2.test",
		},
		{
			name:        "--local-config with an empty --local-cluster reads the default cluster's config",
			localConfig: true,
			sel:         localclusters.Selection{FlagSet: true, Env: "test2"},
			wantPath:    "config.yaml",
			wantDomain:  "default.test",
		},
		{
			name:       "--config reads its file whatever the selection",
			configFile: file,
			sel:        localclusters.Selection{Env: "test2"},
			wantDomain: "file.test",
		},
		{
			name:       "--config ignores an invalid environment variable",
			configFile: file,
			sel:        localclusters.Selection{Env: "Bad_Name"},
			wantDomain: "file.test",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dataHome := stageLocalClusterConfigs(t)

			cfg, path, err := loadPlatformConfig(tc.configFile, tc.localConfig, tc.sel)
			if err != nil {
				t.Fatalf("loadPlatformConfig: %v", err)
			}

			wantPath := tc.configFile
			if tc.wantPath != "" {
				wantPath = filepath.Join(dataHome, tc.wantPath)
			}
			if path != wantPath {
				t.Errorf("read %s, want %s", path, wantPath)
			}
			if got := ingressDomainOf(t, cfg); got != tc.wantDomain {
				t.Errorf("loaded ingress.domain %q, want %q", got, tc.wantDomain)
			}
		})
	}
}

func TestPlatformConfig_NamedClusterWithoutConfig_SaysHowToWriteOne(t *testing.T) {
	stageLocalClusterConfigs(t)

	_, _, err := loadPlatformConfig("", true, localclusters.Selection{Env: "test3"})

	wantError(t, err, `"test3"`, "educates local config init --local-cluster test3 --listen-address <ip>")
}

func TestPlatformConfig_InvalidSelection_Errors(t *testing.T) {
	stageLocalClusterConfigs(t)

	_, _, err := loadPlatformConfig("", true, localclusters.Selection{Env: "Bad_Name"})

	wantError(t, err, "EDUCATES_LOCAL_CLUSTER", `"Bad_Name"`, "lowercase DNS label")
}

func TestRender_LocalConfig_NamedCluster_TakesTheNipDomainOfItsListenAddress(t *testing.T) {
	dataHome := t.TempDir()
	t.Setenv("EDUCATES_CLI_DATA_HOME", dataHome)
	test2, err := localclusters.Named("test2", dataHome)
	if err != nil {
		t.Fatal(err)
	}
	writeNamedConfig(t, test2, "apiVersion: cli.educates.dev/v1alpha1\nkind: EducatesLocalConfig\n"+
		"cluster:\n  listenAddress: 10.10.10.2\n")

	p := ProjectInfo{Version: "test", ImageRepository: "ghcr.io/educates"}
	o := &PlatformRenderOptions{LocalConfig: true, LocalCluster: localclusters.Selection{FlagSet: true, Flag: "test2"}}

	var buf bytes.Buffer
	if err := p.runRender(&buf, o); err != nil {
		t.Fatalf("runRender: %v", err)
	}
	s := buf.String()
	for _, want := range []string{"domain: 10-10-10-2.nip.io", "provider: None", "protocol: http"} {
		if !strings.Contains(s, want) {
			t.Errorf("render output missing %q:\n%s", want, s)
		}
	}
	if strings.Contains(s, "auto-derived from host IP") {
		t.Errorf("render of a named cluster emitted the host-IP note:\n%s", s)
	}
}

func TestRender_RefusesLocalClusterWithConfig(t *testing.T) {
	t.Setenv("EDUCATES_CLI_DATA_HOME", t.TempDir())
	c := (&ProjectInfo{}).NewAdminPlatformRenderCmd()
	c.SetArgs([]string{"--config", writeFixture(t, localWithDomain), "--local-cluster", "test2"})
	c.SetOut(&bytes.Buffer{})
	c.SetErr(&bytes.Buffer{})

	err := c.Execute()

	wantError(t, err, "config", "local-cluster")
}

func TestRenderCommandReadsTheSelectedLocalClustersConfig(t *testing.T) {
	stageLocalClusterConfigs(t)
	stageRootCA(t)
	t.Setenv("EDUCATES_LOCAL_CLUSTER", "test3")
	p := &ProjectInfo{Version: "test", ImageRepository: "ghcr.io/educates"}

	render := p.NewAdminPlatformRenderCmd()
	render.SetArgs([]string{"--local-config", "--local-cluster", "test2"})
	var renderOut bytes.Buffer
	render.SetOut(&renderOut)
	render.SetErr(&bytes.Buffer{})
	if err := render.Execute(); err != nil {
		t.Fatalf("render: %v", err)
	}
	if !strings.Contains(renderOut.String(), "domain: test2.test") {
		t.Errorf("render --local-cluster test2 did not render test2's config:\n%s", renderOut.String())
	}
}
