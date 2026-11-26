package localclusters

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/educates/educates-training-platform/client-programs/pkg/config/v1alpha1"
)

const localConfigHeader = "apiVersion: cli.educates.dev/v1alpha1\nkind: EducatesLocalConfig\n"

// twoControlPlanesConfig is a config that does not load: it breaks a rule
// of a local cluster's config, declaring two control-plane nodes.
const twoControlPlanesConfig = localConfigHeader +
	"cluster:\n  nodes:\n  - role: control-plane\n  - role: control-plane\n"

// twoControlPlanes is what the error for twoControlPlanesConfig says.
const twoControlPlanes = `2 nodes have role "control-plane"`

// writeConfig writes body as a config file in a temporary directory and
// returns its path.
func writeConfig(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// wantErrorMentioning fails t unless err is an error whose message holds
// every one of parts.
func wantErrorMentioning(t *testing.T, err error, parts ...string) {
	t.Helper()
	if err == nil {
		t.Fatalf("got no error, want one mentioning %q", parts)
	}
	for _, part := range parts {
		if !strings.Contains(err.Error(), part) {
			t.Errorf("error %q does not mention %q", err, part)
		}
	}
}

// loadLocal loads the config of a named local cluster at path, in a data
// home whose router address is 10.10.10.1, and returns it as the
// EducatesLocalConfig it must be.
func loadLocal(t *testing.T, path string) *v1alpha1.EducatesLocalConfig {
	t.Helper()
	dataHome := t.TempDir()
	routerFile(t, dataHome, routerHeader+"address: 10.10.10.1\n")
	cfg, err := LoadConfigFile(namedID(t, "test2", dataHome), path)
	if err != nil {
		t.Fatalf("LoadConfigFile: %v", err)
	}
	local, ok := cfg.(*v1alpha1.EducatesLocalConfig)
	if !ok {
		t.Fatalf("LoadConfigFile returned %T, want *v1alpha1.EducatesLocalConfig", cfg)
	}
	return local
}

func TestLoadConfigFileOfANamedClusterFallsBackToItsNameUnderTheNipDomainOfTheRouterAddress(t *testing.T) {
	path := writeConfig(t, localConfigHeader)

	c := loadLocal(t, path)

	if got, want := c.Ingress.Domain, "test2.10-10-10-1.nip.io"; got != want {
		t.Errorf("ingress.domain = %q, want %q", got, want)
	}
	if !c.Ingress.Insecure {
		t.Errorf("ingress.insecure = false, want true with the nip.io fallback")
	}
	if c.ClusterAdmin == nil || !*c.ClusterAdmin {
		t.Errorf("clusterAdmin = %v, want the static default true", c.ClusterAdmin)
	}
}

func TestLoadConfigFileOfANamedClusterNeedsADomainForAnIPv6RouterAddress(t *testing.T) {
	dataHome := t.TempDir()
	routerFile(t, dataHome, routerHeader+"address: 'fd00::1'\n")
	id := namedID(t, "test2", dataHome)
	path := writeConfig(t, localConfigHeader)

	_, err := LoadConfigFile(id, path)

	wantErrorMentioning(t, err, path, "router address fd00::1", "nip.io", "ingress.domain")

	path = writeConfig(t, localConfigHeader+"ingress:\n  domain: test2.educates.test\n")
	cfg, err := LoadConfigFile(id, path)
	if err != nil {
		t.Fatalf("LoadConfigFile: %v", err)
	}
	if got := cfg.(*v1alpha1.EducatesLocalConfig).Ingress.Domain; got != "test2.educates.test" {
		t.Errorf("ingress.domain = %q, want the declared test2.educates.test", got)
	}
}

// A config that declares no domain takes it from the router address, so
// with no router file it says how to set the router address. The config
// itself exists, so the error is not one for a missing file, which a
// caller taking a missing config as none would take it for.
func TestLoadConfigFileOfANamedClusterWithNoRouterFileSaysHowToSetTheRouterAddress(t *testing.T) {
	path := writeConfig(t, localConfigHeader)

	_, err := LoadConfigFile(namedID(t, "test2", t.TempDir()), path)

	wantErrorMentioning(t, err, path, "ingress.domain", "router.yaml", "educates local router update --address <ip>")
	if errors.Is(err, fs.ErrNotExist) {
		t.Errorf("errors.Is(%v, fs.ErrNotExist) = true, want false for a config that exists", err)
	}
}

const escapeConfigHeader = "apiVersion: cli.educates.dev/v1alpha1\nkind: EducatesConfig\n"

func TestLoadConfigFileOfANamedClusterTakesAnEducatesConfigForKindWithoutAFallback(t *testing.T) {
	path := writeConfig(t, escapeConfigHeader+
		"target:\n  provider: kind\n  cluster:\n    apiServer:\n      port: 6443\n"+
		"educatesClusterConfig:\n  mode: Managed\n")

	cfg, err := LoadConfigFile(namedID(t, "test2", t.TempDir()), path)
	if err != nil {
		t.Fatalf("LoadConfigFile: %v", err)
	}

	escape, ok := cfg.(*v1alpha1.EducatesConfig)
	if !ok {
		t.Fatalf("LoadConfigFile returned %T, want *v1alpha1.EducatesConfig", cfg)
	}
	if got := escape.IngressDomain(); got != "" {
		t.Errorf("ingress domain = %q, want none: an EducatesConfig gets no nip.io fallback", got)
	}
	if got, want := escape.Target.Cluster.ApiServer.Port, 6443; got != want {
		t.Errorf("target.cluster.apiServer.port = %d, want %d", got, want)
	}
}

// An EducatesConfig takes no nip.io name, so it needs no router file.
func TestLoadConfigFileOfANamedClusterTakesAnEducatesConfigWithoutTheRouterFile(t *testing.T) {
	path := writeConfig(t, escapeConfigHeader+"target:\n  provider: kind\n  cluster: {}\n")

	if _, err := LoadConfigFile(namedID(t, "test2", t.TempDir()), path); err != nil {
		t.Errorf("LoadConfigFile: %v", err)
	}
}

func TestLoadConfigFileOfANamedClusterRefusesOtherConfigs(t *testing.T) {
	cases := []struct {
		name string
		body string
	}{
		{name: "EducatesConfig without a target", body: escapeConfigHeader},
		{
			name: "EducatesConfig without a provider",
			body: escapeConfigHeader + "target:\n  cluster:\n    apiServer:\n      port: 6443\n",
		},
		{
			name: "a remote cluster kind",
			body: "apiVersion: cli.educates.dev/v1alpha1\nkind: EducatesGKEConfig\n" +
				"gcp:\n  project: p\ndomain: academy.example.com\nacme:\n  email: ops@example.com\n",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := writeConfig(t, tc.body)

			_, err := LoadConfigFile(namedID(t, "test2", t.TempDir()), path)

			wantErrorMentioning(t, err, path, "EducatesLocalConfig", "EducatesConfig with target.provider: kind")
		})
	}
}

func TestLoadConfigFileOfANamedClusterKeepsADeclaredDomain(t *testing.T) {
	path := writeConfig(t, localConfigHeader+
		"ingress:\n  domain: test2.educates.test\n")

	c := loadLocal(t, path)

	if got, want := c.Ingress.Domain, "test2.educates.test"; got != want {
		t.Errorf("ingress.domain = %q, want %q", got, want)
	}
	if c.Ingress.Insecure {
		t.Errorf("ingress.insecure = true, want false as declared")
	}
}

// TestValidateClusterNodes checks that a declared node list has exactly one
// control-plane node, the node the router sends the cluster's names to,
// and uses only the roles kind understands.
func TestValidateClusterNodes(t *testing.T) {
	controlPlane := v1alpha1.ClusterNode{Role: "control-plane"}
	worker := v1alpha1.ClusterNode{Role: "worker"}

	cases := []struct {
		name    string
		nodes   []v1alpha1.ClusterNode
		wantErr string
	}{
		{"default cluster", nil, ""},
		{"single control-plane", []v1alpha1.ClusterNode{controlPlane}, ""},
		{"control-plane and workers", []v1alpha1.ClusterNode{controlPlane, worker, worker}, ""},
		{"workers only", []v1alpha1.ClusterNode{worker, worker}, `one node must have role "control-plane"`},
		{"two control-planes", []v1alpha1.ClusterNode{controlPlane, worker, controlPlane}, `2 nodes have role "control-plane"`},
		{"unknown role", []v1alpha1.ClusterNode{controlPlane, {Role: "infra"}}, `invalid node role "infra"`},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := validateClusterNodes(tc.nodes)

			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}

			wantErrorMentioning(t, err, tc.wantErr)
		})
	}
}

// TestCheckConfigRefusesMoreThanOneControlPlaneNode checks that the rules
// local config edit and set apply refuse a second control-plane node, in
// the field each config kind declares the nodes in.
func TestCheckConfigRefusesMoreThanOneControlPlaneNode(t *testing.T) {
	cases := []struct {
		name  string
		id    Identity
		body  string
		field string
	}{
		{
			name: "EducatesLocalConfig of the default cluster",
			id:   Default(t.TempDir()),
			body: localConfigHeader +
				"cluster:\n" +
				"  nodes:\n  - role: control-plane\n  - role: worker\n  - role: control-plane\n",
			field: "cluster.nodes",
		},
		{
			name: "EducatesConfig for kind of a named cluster",
			id:   namedID(t, "test2", t.TempDir()),
			body: escapeConfigHeader +
				"target:\n  provider: kind\n  cluster:\n" +
				"    nodes:\n    - role: control-plane\n    - role: worker\n    - role: control-plane\n" +
				"educatesClusterConfig:\n  mode: Managed\n",
			field: "target.cluster.nodes",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := CheckConfig(tc.id, []byte(tc.body), "config.yaml")

			wantErrorMentioning(t, err, "config.yaml", tc.field, `2 nodes have role "control-plane"`)
		})
	}
}

// TestCheckConfigTakesOneControlPlaneNodeWithWorkers checks that a node
// list with one control-plane node and workers passes the rules.
func TestCheckConfigTakesOneControlPlaneNodeWithWorkers(t *testing.T) {
	body := localConfigHeader +
		"cluster:\n  nodes:\n  - role: control-plane\n  - role: worker\n  - role: worker\n" +
		"ingress:\n  domain: educates.test\n"

	if err := CheckConfig(Default(t.TempDir()), []byte(body), "config.yaml"); err != nil {
		t.Fatalf("CheckConfig: %v", err)
	}
}
