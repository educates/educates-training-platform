package cmd

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/educates/educates-training-platform/client-programs/pkg/config"
	"github.com/educates/educates-training-platform/client-programs/pkg/localclusters"
)

func TestLocalConfigCommandsSelectTheLocalCluster(t *testing.T) {
	cases := []struct {
		name string
		args []string
		env  string
		want string
	}{
		{name: "the flag", args: []string{"--local-cluster", "test2"}, want: "test2"},
		{name: "the environment variable", env: "test2", want: "test2"},
		{name: "the flag over the environment variable", args: []string{"--local-cluster=test3"}, env: "test2", want: "test3"},
		{name: "an empty flag over the environment variable", args: []string{"--local-cluster="}, env: "test2", want: ""},
		{name: "neither", want: ""},
	}
	for _, command := range []string{"init", "get", "set", "view", "edit", "delete"} {
		for _, tc := range cases {
			t.Run(command+" with "+tc.name, func(t *testing.T) {
				dataHome := t.TempDir()
				t.Setenv("EDUCATES_CLI_DATA_HOME", dataHome)
				t.Setenv("EDUCATES_LOCAL_CLUSTER", tc.env)
				c, _, err := (&ProjectInfo{}).NewLocalConfigCmdGroup().Find([]string{command})
				if err != nil || c.Name() != command {
					t.Fatalf("local config has no %s command (found %q, err %v)", command, c.Name(), err)
				}
				if err := c.ParseFlags(tc.args); err != nil {
					t.Fatalf("ParseFlags(%q): %v", tc.args, err)
				}

				got, err := selectedLocalCluster(c)
				if err != nil {
					t.Fatalf("selectedLocalCluster: %v", err)
				}

				want := localclusters.Default(dataHome)
				if tc.want != "" {
					want, _ = localclusters.Named(tc.want, dataHome)
				}
				if got != want {
					t.Errorf("selected %+v, want %+v", got, want)
				}
			})
		}
	}
}

// A named cluster's config needs nothing but its name.
func TestLocalConfigInit_NamedCluster_WritesItsConfigWithNothingButItsName(t *testing.T) {
	dataHome, id := namedClusterForTest(t)

	var out bytes.Buffer
	if err := (&ProjectInfo{}).runLocalConfigInit(&LocalConfigInitOptions{}, id, &out); err != nil {
		t.Fatalf("init: %v", err)
	}

	if body, _ := os.ReadFile(id.ConfigPath); string(body) != minimalLocalConfigYAML {
		t.Errorf("init wrote:\n%s\nwant:\n%s", body, minimalLocalConfigYAML)
	}
	if !bytes.Contains(out.Bytes(), []byte(id.ConfigPath)) {
		t.Errorf("output %q does not name %s", out.String(), id.ConfigPath)
	}
	if _, err := os.Stat(config.LocalConfigPath(dataHome)); !os.IsNotExist(err) {
		t.Errorf("the default cluster's config was touched: stat err = %v", err)
	}
}

func TestLocalConfigInit_NamedCluster_WritesTheDomainGiven(t *testing.T) {
	_, id := namedClusterForTest(t)

	if err := (&ProjectInfo{}).runLocalConfigInit(&LocalConfigInitOptions{Domain: "test2.educates.test"}, id, io.Discard); err != nil {
		t.Fatalf("init: %v", err)
	}

	if got, want := declaredConfig(t, id.ConfigPath).Ingress.Domain, "test2.educates.test"; got != want {
		t.Errorf("written ingress.domain = %q, want %q", got, want)
	}
}

// namedConfigForTest is a named local cluster's config that declares
// nothing, so it takes its name under the nip.io name of the router
// address.
const namedConfigForTest = "apiVersion: cli.educates.dev/v1alpha1\nkind: EducatesLocalConfig\n"

func TestLocalConfigInit_NamedCluster_ExistingConfig_ErrorsWithoutForce(t *testing.T) {
	_, id := namedClusterForTest(t)
	writeNamedConfig(t, id, namedConfigForTest)

	err := (&ProjectInfo{}).runLocalConfigInit(&LocalConfigInitOptions{Domain: "test2.educates.test"}, id, io.Discard)

	wantError(t, err, id.ConfigPath, "already exists", "--force")
	if body, _ := os.ReadFile(id.ConfigPath); string(body) != namedConfigForTest {
		t.Errorf("the config was changed to:\n%s", body)
	}
}

func TestLocalConfigInit_NamedCluster_Force_Overwrites(t *testing.T) {
	_, id := namedClusterForTest(t)
	writeNamedConfig(t, id, namedConfigForTest+"cluster:\n  listenAddress: 10.10.10.2\n")

	o := &LocalConfigInitOptions{Force: true, Domain: "test2.educates.test"}
	if err := (&ProjectInfo{}).runLocalConfigInit(o, id, io.Discard); err != nil {
		t.Fatalf("init --force: %v", err)
	}

	written := declaredConfig(t, id.ConfigPath)
	if written.Cluster.ListenAddress != "" || written.Ingress.Domain != "test2.educates.test" {
		t.Errorf("written cluster.listenAddress, ingress.domain = %q, %q, want none, test2.educates.test",
			written.Cluster.ListenAddress, written.Ingress.Domain)
	}
}

func TestLocalConfigInit_DefaultCluster_WritesTheDomainGiven(t *testing.T) {
	dataHome, _ := namedClusterForTest(t)

	err := (&ProjectInfo{}).runLocalConfigInit(&LocalConfigInitOptions{Domain: "educates.test"},
		localclusters.Default(dataHome), io.Discard)
	if err != nil {
		t.Fatalf("init: %v", err)
	}

	want := minimalLocalConfigYAML + "ingress:\n  domain: educates.test\n"
	if body, _ := os.ReadFile(config.LocalConfigPath(dataHome)); string(body) != want {
		t.Errorf("init wrote:\n%s\nwant:\n%s", body, want)
	}
}

func TestLocalConfigInit_NamedCluster_RefusesDefaults(t *testing.T) {
	_, id := namedClusterForTest(t)

	err := (&ProjectInfo{}).runLocalConfigInit(&LocalConfigInitOptions{Defaults: true}, id, io.Discard)

	wantError(t, err, "--defaults", "named local cluster")
	if _, err := os.Stat(id.ConfigPath); !os.IsNotExist(err) {
		t.Errorf("a config was written: stat err = %v", err)
	}
}

func TestLocalConfigSet_NamedCluster_ChangesOnlyThatClustersConfig(t *testing.T) {
	dataHome := stageInitConfig(t)
	defaultConfig, _ := os.ReadFile(config.LocalConfigPath(dataHome))
	id, _ := localclusters.Named("test2", dataHome)
	writeNamedConfig(t, id, namedConfigForTest)

	if err := runLocalConfigSet(io.Discard, id, "ingress.domain", "test2.educates.test"); err != nil {
		t.Fatalf("set: %v", err)
	}

	if got := declaredConfig(t, id.ConfigPath).Ingress.Domain; got != "test2.educates.test" {
		t.Errorf("named config ingress.domain = %q, want test2.educates.test", got)
	}
	if body, _ := os.ReadFile(config.LocalConfigPath(dataHome)); string(body) != string(defaultConfig) {
		t.Errorf("the default cluster's config was changed to:\n%s", body)
	}
	var out bytes.Buffer
	if err := runLocalConfigGet(&out, id, "ingress.domain"); err != nil {
		t.Fatalf("get: %v", err)
	}
	if got := strings.TrimSpace(out.String()); got != "test2.educates.test" {
		t.Errorf("get ingress.domain = %q, want test2.educates.test", got)
	}
}

func TestLocalConfigGetAndSet_NamedClusterWithoutConfig_NameTheInitCommand(t *testing.T) {
	_, id := namedClusterForTest(t)

	errs := map[string]error{
		"get": runLocalConfigGet(io.Discard, id, "ingress.domain"),
		"set": runLocalConfigSet(io.Discard, id, "ingress.domain", "test2.educates.test"),
	}

	for command, err := range errs {
		t.Run(command, func(t *testing.T) {
			wantError(t, err, id.ConfigPath, "educates local config init --local-cluster test2\n")
		})
	}
	if _, err := os.Stat(id.ConfigPath); !os.IsNotExist(err) {
		t.Errorf("a config was written: stat err = %v", err)
	}
}

// twoControlPlanesConfig is a local cluster's config that breaks a rule of
// a local cluster's config, declaring two control-plane nodes.
const twoControlPlanesConfig = namedConfigForTest +
	"cluster:\n  nodes:\n  - role: control-plane\n  - role: control-plane\n"

func TestLocalConfigSet_NamedCluster_RefusesAChangeToAConfigTheRulesReject(t *testing.T) {
	_, id := namedClusterForTest(t)
	writeNamedConfig(t, id, twoControlPlanesConfig)

	err := runLocalConfigSet(io.Discard, id, "ingress.domain", "test2.educates.test")

	wantError(t, err, "cluster.nodes", `2 nodes have role "control-plane"`)
	if body, _ := os.ReadFile(id.ConfigPath); string(body) != twoControlPlanesConfig {
		t.Errorf("the config was changed to:\n%s", body)
	}
}

func TestLocalConfigView_NamedCluster_PrintsItsEffectiveConfig(t *testing.T) {
	dataHome, id := namedClusterForTest(t)
	writeRouterFile(t, dataHome, handWrittenRouterFile)
	writeNamedConfig(t, id, namedConfigForTest)

	p := ProjectInfo{Version: "test", ImageRepository: "ghcr.io/educates"}
	var out bytes.Buffer
	if err := p.runLocalConfigView(&LocalConfigViewOptions{}, id, &out); err != nil {
		t.Fatalf("view: %v", err)
	}

	s := out.String()
	for _, want := range []string{
		"domain: test2.10-10-10-1.nip.io",
		"insecure: true",
		"lookupService: true",
		"(file: " + id.ConfigPath,
		"educates local config view --local-cluster test2 --raw",
		"with a named local cluster's name under it",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("view output does not contain %q:\n%s", want, s)
		}
	}
	if strings.Contains(s, "<host-IP>") {
		t.Errorf("view output explains the default cluster's host IP fallback:\n%s", s)
	}
}

func TestLocalConfigView_NamedCluster_Raw(t *testing.T) {
	_, id := namedClusterForTest(t)
	writeNamedConfig(t, id, namedConfigForTest)

	var out bytes.Buffer
	if err := (&ProjectInfo{}).runLocalConfigView(&LocalConfigViewOptions{Raw: true}, id, &out); err != nil {
		t.Fatalf("view --raw: %v", err)
	}

	if got := out.String(); got != namedConfigForTest {
		t.Errorf("--raw printed %q, want the file %q", got, namedConfigForTest)
	}
}

func TestLocalConfigView_NamedCluster_ShowsAConfigTheRulesRejectWithWhy(t *testing.T) {
	cases := map[string]struct {
		config, routerFile string
		want               []string
	}{
		"two control-plane nodes": {config: twoControlPlanesConfig, routerFile: handWrittenRouterFile,
			want: []string{"cluster.nodes", `2 nodes have role "control-plane"`}},
		"no domain and no router file": {config: namedConfigForTest,
			want: []string{"router.yaml", "educates local router update --address <ip>"}},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			dataHome, id := namedClusterForTest(t)
			if tc.routerFile != "" {
				writeRouterFile(t, dataHome, tc.routerFile)
			}
			writeNamedConfig(t, id, tc.config)

			var out bytes.Buffer
			if err := (&ProjectInfo{}).runLocalConfigView(&LocalConfigViewOptions{}, id, &out); err != nil {
				t.Fatalf("view: %v", err)
			}

			for _, want := range append([]string{"# The local cluster can't use this config until it is fixed:"}, tc.want...) {
				if !strings.Contains(out.String(), want) {
					t.Errorf("view output does not contain %q:\n%s", want, out.String())
				}
			}
		})
	}
}

func TestLocalConfigView_NamedClusterWithoutConfig_NamesTheInitCommand(t *testing.T) {
	_, id := namedClusterForTest(t)

	err := (&ProjectInfo{}).runLocalConfigView(&LocalConfigViewOptions{}, id, io.Discard)

	wantError(t, err, id.ConfigPath, "educates local config init --local-cluster test2\n")
}

func TestRunLocalConfigEdit_NamedCluster_EditsItsConfig(t *testing.T) {
	dataHome := stageInitConfig(t)
	defaultConfig, _ := os.ReadFile(config.LocalConfigPath(dataHome))
	id, _ := localclusters.Named("test2", dataHome)
	writeNamedConfig(t, id, namedConfigForTest)
	edited := namedConfigForTest + "ingress:\n  domain: test2.educates.test\n"
	stateDir := stageFakeEditor(t, edited)

	if err := runLocalConfigEdit(id); err != nil {
		t.Fatalf("edit: %v", err)
	}

	if n := editorInvocations(t, stateDir); n != 1 {
		t.Errorf("editor invoked %d times, want 1", n)
	}
	if got := declaredConfig(t, id.ConfigPath).Ingress.Domain; got != "test2.educates.test" {
		t.Errorf("named config ingress.domain = %q, want test2.educates.test", got)
	}
	if body, _ := os.ReadFile(config.LocalConfigPath(dataHome)); string(body) != string(defaultConfig) {
		t.Errorf("the default cluster's config was changed to:\n%s", body)
	}
	if leftovers := tempLeftovers(t, filepath.Dir(id.ConfigPath)); len(leftovers) != 0 {
		t.Errorf("temp files left behind after success: %v", leftovers)
	}
}

func TestRunLocalConfigEdit_NamedCluster_ReopensOnAConfigTheNamedClusterRulesReject(t *testing.T) {
	dataHome, id := namedClusterForTest(t)
	writeRouterFile(t, dataHome, handWrittenRouterFile)
	writeNamedConfig(t, id, namedConfigForTest)
	valid := namedConfigForTest + "# edited-marker\n"
	stateDir := stageFakeEditor(t, twoControlPlanesConfig, valid)

	if err := runLocalConfigEdit(id); err != nil {
		t.Fatalf("edit: %v", err)
	}

	if n := editorInvocations(t, stateDir); n != 2 {
		t.Errorf("editor invoked %d times, want 2 (one reopen on the two control-plane nodes)", n)
	}
	if body, _ := os.ReadFile(id.ConfigPath); !strings.Contains(string(body), "# edited-marker") {
		t.Errorf("the second, valid edit was not saved:\n%s", body)
	}
}

func TestRunLocalConfigEdit_NamedClusterWithoutConfig_NamesTheInitCommand(t *testing.T) {
	_, id := namedClusterForTest(t)
	stateDir := stageFakeEditor(t, namedConfigForTest)

	err := runLocalConfigEdit(id)

	wantError(t, err, id.ConfigPath, "educates local config init --local-cluster test2\n")
	if n := editorInvocations(t, stateDir); n != 0 {
		t.Errorf("editor invoked %d times, want 0", n)
	}
	if _, err := os.Stat(filepath.Dir(id.ConfigPath)); !os.IsNotExist(err) {
		t.Errorf("the cluster's folder was created: stat err = %v", err)
	}
}

// A config init can't write, here one without a domain on a router
// address with no nip.io name, leaves the existing config as it is.
func TestLocalConfigInit_NamedCluster_Force_KeepsTheConfigWhenTheNewOneIsRefused(t *testing.T) {
	dataHome, id := namedClusterForTest(t)
	writeRouterFile(t, dataHome, "apiVersion: cli.educates.dev/v1alpha1\nkind: EducatesLocalRouter\naddress: 'fd00::1'\n")
	writeNamedConfig(t, id, namedConfigForTest)

	err := (&ProjectInfo{}).runLocalConfigInit(&LocalConfigInitOptions{Force: true}, id, io.Discard)

	wantError(t, err, "router address fd00::1", "nip.io")
	if body, _ := os.ReadFile(id.ConfigPath); string(body) != namedConfigForTest {
		t.Errorf("the config was changed to:\n%s", body)
	}
}
