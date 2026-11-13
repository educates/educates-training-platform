package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/educates/educates-training-platform/client-programs/pkg/config"
	"github.com/educates/educates-training-platform/client-programs/pkg/config/v1alpha1"
	"github.com/educates/educates-training-platform/client-programs/pkg/localclusters"
)

// namedClusterForTest sets a temporary data home and returns it with the
// identity of the named local cluster test2 in it.
func namedClusterForTest(t *testing.T) (string, localclusters.Identity) {
	t.Helper()
	dataHome := t.TempDir()
	t.Setenv("EDUCATES_CLI_DATA_HOME", dataHome)
	id, err := localclusters.Named("test2", dataHome)
	if err != nil {
		t.Fatal(err)
	}
	return dataHome, id
}

// declaredConfig loads the config file at path as written, without
// defaults, as an EducatesLocalConfig.
func declaredConfig(t *testing.T, path string) *v1alpha1.EducatesLocalConfig {
	t.Helper()
	cfg, err := config.LoadWithoutDefaults(path)
	if err != nil {
		t.Fatalf("load %s: %v", path, err)
	}
	local, ok := cfg.(*v1alpha1.EducatesLocalConfig)
	if !ok {
		t.Fatalf("%s holds %T, want *v1alpha1.EducatesLocalConfig", path, cfg)
	}
	return local
}

func TestLoadLocalConfig_NamedCluster_WritesTheMissingConfigFromTheListenAddress(t *testing.T) {
	dataHome, id := namedClusterForTest(t)

	cfg, path, created, err := loadLocalConfig(&LocalClusterCreateOptions{ListenAddress: "10.10.10.2"}, id)
	if err != nil {
		t.Fatalf("loadLocalConfig: %v", err)
	}

	if !created {
		t.Errorf("created = false, want true for a config written from --listen-address")
	}
	if path != id.ConfigPath {
		t.Errorf("path = %q, want the named cluster's config path %q", path, id.ConfigPath)
	}
	written := declaredConfig(t, id.ConfigPath)
	if got, want := written.Cluster.ListenAddress, "10.10.10.2"; got != want {
		t.Errorf("written cluster.listenAddress = %q, want %q", got, want)
	}
	if written.Ingress.Domain != "" {
		t.Errorf("written ingress.domain = %q, want none", written.Ingress.Domain)
	}
	local, ok := cfg.(*v1alpha1.EducatesLocalConfig)
	if !ok {
		t.Fatalf("config = %T, want *v1alpha1.EducatesLocalConfig", cfg)
	}
	if got, want := local.Ingress.Domain, "10-10-10-2.nip.io"; got != want {
		t.Errorf("ingress.domain = %q, want the nip.io fallback %q", got, want)
	}
	if _, err := os.Stat(config.LocalConfigPath(dataHome)); !os.IsNotExist(err) {
		t.Errorf("the default cluster's config was touched: stat err = %v", err)
	}
}

func TestLoadLocalConfig_NamedCluster_WritesTheDomain(t *testing.T) {
	_, id := namedClusterForTest(t)

	cfg, _, _, err := loadLocalConfig(&LocalClusterCreateOptions{ListenAddress: "10.10.10.2", Domain: "test2.educates.test"}, id)
	if err != nil {
		t.Fatalf("loadLocalConfig: %v", err)
	}

	if got, want := declaredConfig(t, id.ConfigPath).Ingress.Domain, "test2.educates.test"; got != want {
		t.Errorf("written ingress.domain = %q, want %q", got, want)
	}
	if ingress := cfg.(*v1alpha1.EducatesLocalConfig).Ingress; ingress.Domain != "test2.educates.test" || ingress.Insecure {
		t.Errorf("ingress = %+v, want domain test2.educates.test, not insecure", ingress)
	}
}

// wantError fails t unless err is an error whose message holds every one
// of parts.
func wantError(t *testing.T, err error, parts ...string) {
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

func TestLoadLocalConfig_NamedCluster_WithoutConfigOrListenAddress_NamesBothWaysForward(t *testing.T) {
	_, id := namedClusterForTest(t)

	for _, o := range []*LocalClusterCreateOptions{{}, {Domain: "test2.educates.test"}} {
		_, _, _, err := loadLocalConfig(o, id)

		wantError(t, err, id.ConfigPath,
			"educates local config init --local-cluster test2 --listen-address",
			"educates local cluster create --local-cluster test2 --listen-address")
		if _, err := os.Stat(id.ConfigPath); !os.IsNotExist(err) {
			t.Errorf("a config was written: stat err = %v", err)
		}
	}
}

func TestLoadLocalConfig_NamedCluster_RefusesAListenAddressFlagThatIsNotASpecificIP(t *testing.T) {
	_, id := namedClusterForTest(t)

	for _, address := range []string{"0.0.0.0", "127.0.0.1"} {
		_, _, _, err := loadLocalConfig(&LocalClusterCreateOptions{ListenAddress: address}, id)

		wantError(t, err, "--listen-address", address)
		if _, err := os.Stat(id.ConfigPath); !os.IsNotExist(err) {
			t.Errorf("a config was written for %s: stat err = %v", address, err)
		}
	}
}

func TestLoadLocalConfig_NamedCluster_AnIPv6ListenAddressFlagNeedsTheDomainFlag(t *testing.T) {
	_, id := namedClusterForTest(t)

	_, _, _, err := loadLocalConfig(&LocalClusterCreateOptions{ListenAddress: "fd00::2"}, id)

	wantError(t, err, "--listen-address fd00::2", "nip.io", "--domain")
	if _, err := os.Stat(id.ConfigPath); !os.IsNotExist(err) {
		t.Errorf("a config was written: stat err = %v", err)
	}
}

func TestLoadLocalConfig_NamedClusterWithConfigFlag_TakesTheConfigFromTheFile(t *testing.T) {
	_, id := namedClusterForTest(t)
	path := writeFixture(t, "apiVersion: cli.educates.dev/v1alpha1\nkind: EducatesLocalConfig\n"+
		"cluster:\n  listenAddress: 10.10.10.2\n")

	cfg, gotPath, created, err := loadLocalConfig(&LocalClusterCreateOptions{Config: path}, id)
	if err != nil {
		t.Fatalf("loadLocalConfig: %v", err)
	}

	if gotPath != path || created {
		t.Errorf("path, created = %q, %v, want %q, false", gotPath, created, path)
	}
	if got := cfg.(*v1alpha1.EducatesLocalConfig).Ingress.Domain; got != "10-10-10-2.nip.io" {
		t.Errorf("ingress.domain = %q, want the named cluster's nip.io fallback", got)
	}
	if _, err := os.Stat(id.ConfigPath); !os.IsNotExist(err) {
		t.Errorf("a config was written in the data home: stat err = %v", err)
	}
}

func TestLoadLocalConfig_NamedClusterWithConfigFlag_MakesTheClusterFolder(t *testing.T) {
	_, id := namedClusterForTest(t)
	path := writeFixture(t, "apiVersion: cli.educates.dev/v1alpha1\nkind: EducatesLocalConfig\n"+
		"cluster:\n  listenAddress: 10.10.10.2\n")

	if _, _, _, err := loadLocalConfig(&LocalClusterCreateOptions{Config: path}, id); err != nil {
		t.Fatalf("loadLocalConfig: %v", err)
	}

	if err := localclusters.CheckEducatesCluster(id); err != nil {
		t.Errorf("the created cluster is not an Educates cluster: %v", err)
	}
	if _, err := os.Stat(id.ConfigPath); !os.IsNotExist(err) {
		t.Errorf("the config was copied into the cluster's folder: stat err = %v", err)
	}
}

func TestLoadLocalConfig_NamedClusterWithConfigFlag_AppliesTheNamedClusterRules(t *testing.T) {
	_, id := namedClusterForTest(t)
	path := writeFixture(t, "apiVersion: cli.educates.dev/v1alpha1\nkind: EducatesLocalConfig\n")

	_, _, _, err := loadLocalConfig(&LocalClusterCreateOptions{Config: path}, id)

	wantError(t, err, path, "cluster.listenAddress")
}

func TestLoadLocalConfig_ConfigWritingFlagsNeedANamedClusterAndNoConfigFlag(t *testing.T) {
	dataHome, id := namedClusterForTest(t)

	_, _, _, err := loadLocalConfig(&LocalClusterCreateOptions{ListenAddress: "10.10.10.2"}, localclusters.Default(dataHome))
	wantError(t, err, "--listen-address", "named local cluster", "--local-cluster", "EDUCATES_LOCAL_CLUSTER")

	path := writeFixture(t, "apiVersion: cli.educates.dev/v1alpha1\nkind: EducatesLocalConfig\n"+
		"cluster:\n  listenAddress: 10.10.10.2\n")
	_, _, _, err = loadLocalConfig(&LocalClusterCreateOptions{Config: path, Domain: "test2.educates.test"}, id)
	wantError(t, err, "--domain", "--config")
}

func TestCreateDeployFlags_TargetTheCreatedCluster(t *testing.T) {
	dataHome, named := namedClusterForTest(t)
	const kindKubeconfig = "/home/developer/.kube/config"

	for _, id := range []localclusters.Identity{localclusters.Default(dataHome), named} {
		t.Run(id.KindName, func(t *testing.T) {
			o := &LocalClusterCreateOptions{Timeout: 7 * time.Minute, Verbose: true}

			flags := createDeployFlags(o, kindKubeconfig, id)

			want := deployPipelineFlags{Kubeconfig: kindKubeconfig, Context: id.Context, Timeout: 7 * time.Minute, Verbose: true}
			if flags != want {
				t.Errorf("deploy flags = %+v, want %+v", flags, want)
			}
		})
	}
}

// writeNamedConfig writes body as the config of the named local cluster
// id.
func writeNamedConfig(t *testing.T, id localclusters.Identity, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(id.ConfigPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(id.ConfigPath, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestLoadLocalConfig_NamedCluster_FlagsThatDisagreeWithTheConfigAreAnError(t *testing.T) {
	const existing = "apiVersion: cli.educates.dev/v1alpha1\nkind: EducatesLocalConfig\n" +
		"cluster:\n  listenAddress: 10.10.10.2\n"
	cases := []struct {
		name string
		o    LocalClusterCreateOptions
		want []string
	}{
		{
			name: "another listen address",
			o:    LocalClusterCreateOptions{ListenAddress: "10.10.10.3"},
			want: []string{"--listen-address 10.10.10.3", "cluster.listenAddress 10.10.10.2"},
		},
		{
			name: "a domain the config does not declare",
			o:    LocalClusterCreateOptions{ListenAddress: "10.10.10.2", Domain: "test2.educates.test"},
			want: []string{"--domain test2.educates.test", "declares none", "nip.io"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, id := namedClusterForTest(t)
			writeNamedConfig(t, id, existing)

			_, _, _, err := loadLocalConfig(&tc.o, id)

			wantError(t, err, append([]string{id.ConfigPath, "disagrees"}, tc.want...)...)
			if body, _ := os.ReadFile(id.ConfigPath); string(body) != existing {
				t.Errorf("the config was changed to:\n%s", body)
			}
		})
	}
}

func TestLoadLocalConfig_NamedCluster_FlagsThatAgreeWithTheConfigAreAccepted(t *testing.T) {
	_, id := namedClusterForTest(t)
	writeNamedConfig(t, id, "apiVersion: cli.educates.dev/v1alpha1\nkind: EducatesLocalConfig\n"+
		"cluster:\n  listenAddress: 10.10.10.2\ningress:\n  domain: test2.educates.test\n")

	cfg, _, created, err := loadLocalConfig(&LocalClusterCreateOptions{ListenAddress: "10.10.10.2", Domain: "test2.educates.test"}, id)
	if err != nil {
		t.Fatalf("loadLocalConfig: %v", err)
	}

	if created {
		t.Errorf("created = true, want false for an existing config")
	}
	if got := cfg.(*v1alpha1.EducatesLocalConfig).Ingress.Domain; got != "test2.educates.test" {
		t.Errorf("ingress.domain = %q, want test2.educates.test", got)
	}
}
