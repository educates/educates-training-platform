package cmd

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/educates/educates-training-platform/client-programs/pkg/config"
	"github.com/educates/educates-training-platform/client-programs/pkg/config/hostinfo"
	"github.com/educates/educates-training-platform/client-programs/pkg/config/v1alpha1"
	"github.com/educates/educates-training-platform/client-programs/pkg/localclusters"
)

// namedClusterForTest sets a temporary data home, on a host whose IP
// address is 192.168.50.151, and returns it with the identity of the
// named local cluster test2 in it.
func namedClusterForTest(t *testing.T) (string, localclusters.Identity) {
	t.Helper()
	stubHostIP(t, "192.168.50.151")
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

// A named cluster needs nothing but its name: its missing config is
// written with no address, and it takes its name under the nip.io name
// of the router address, which create writes first when it is missing.
func TestLoadLocalConfig_NamedCluster_WritesTheMissingConfigWithNothingButItsName(t *testing.T) {
	dataHome, id := namedClusterForTest(t)

	var out bytes.Buffer
	cfg, path, created, err := loadLocalConfig(&out, &LocalClusterCreateOptions{}, id)
	if err != nil {
		t.Fatalf("loadLocalConfig: %v", err)
	}

	if !created {
		t.Errorf("created = false, want true for a missing config")
	}
	if path != id.ConfigPath {
		t.Errorf("path = %q, want the named cluster's config path %q", path, id.ConfigPath)
	}
	if body, _ := os.ReadFile(id.ConfigPath); string(body) != minimalLocalConfigYAML {
		t.Errorf("create wrote:\n%s\nwant:\n%s", body, minimalLocalConfigYAML)
	}
	wantRouterFile(t, dataHome, hostIPRouterFile)
	if !strings.Contains(out.String(), config.LocalRouterPath(dataHome)) {
		t.Errorf("output does not name the router file it wrote:\n%s", out.String())
	}
	local := cfg.(*v1alpha1.EducatesLocalConfig)
	if local.Ingress.Domain != "test2.192-168-50-151.nip.io" || !local.Ingress.Insecure {
		t.Errorf("ingress = %+v, want the insecure test2.192-168-50-151.nip.io", local.Ingress)
	}
	if _, err := os.Stat(config.LocalConfigPath(dataHome)); !os.IsNotExist(err) {
		t.Errorf("the default cluster's config was touched: stat err = %v", err)
	}
}

// The router address comes from router.yaml, whatever the host's IP.
func TestLoadLocalConfig_NamedCluster_TakesItsNameUnderTheRouterAddress(t *testing.T) {
	dataHome, id := namedClusterForTest(t)
	writeRouterFile(t, dataHome, handWrittenRouterFile)

	cfg, _, _, err := loadLocalConfig(io.Discard, &LocalClusterCreateOptions{}, id)
	if err != nil {
		t.Fatalf("loadLocalConfig: %v", err)
	}

	if got, want := cfg.(*v1alpha1.EducatesLocalConfig).Ingress.Domain, "test2.10-10-10-1.nip.io"; got != want {
		t.Errorf("ingress.domain = %q, want %q", got, want)
	}
	wantRouterFile(t, dataHome, handWrittenRouterFile)
}

func TestLoadLocalConfig_NamedCluster_WritesTheDomain(t *testing.T) {
	_, id := namedClusterForTest(t)

	cfg, _, _, err := loadLocalConfig(io.Discard, &LocalClusterCreateOptions{Domain: "test2.educates.test"}, id)
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

// minimalLocalConfigYAML is the EducatesLocalConfig the CLI writes for a
// local cluster when no flag gives it a value: the modeline, apiVersion
// and kind.
const minimalLocalConfigYAML = config.DefaultLocalConfigModeline + "\n" +
	"apiVersion: cli.educates.dev/v1alpha1\nkind: EducatesLocalConfig\n"

// stubHostIP makes host IP detection report ip for the rest of the test
// t, or fail when ip is empty.
func stubHostIP(t *testing.T, ip string) {
	t.Helper()
	detect := hostinfo.DetectHostIP
	t.Cleanup(func() { hostinfo.DetectHostIP = detect })
	hostinfo.DetectHostIP = func() (string, error) {
		if ip == "" {
			return "", errors.New("no route to the outside world")
		}
		return ip, nil
	}
}

// A router address with no nip.io name gives a cluster without a domain
// no name, so no config is written for it.
func TestLoadLocalConfig_NamedCluster_AnIPv6RouterAddressNeedsTheDomainFlag(t *testing.T) {
	dataHome, id := namedClusterForTest(t)
	writeRouterFile(t, dataHome, "apiVersion: cli.educates.dev/v1alpha1\nkind: EducatesLocalRouter\naddress: 'fd00::1'\n")

	_, _, _, err := loadLocalConfig(io.Discard, &LocalClusterCreateOptions{}, id)

	wantError(t, err, "router address fd00::1", "nip.io", "ingress.domain")
	if _, err := os.Stat(id.ConfigPath); !os.IsNotExist(err) {
		t.Errorf("a config was written: stat err = %v", err)
	}
}

func TestLoadLocalConfig_NamedClusterWithConfigFlag_TakesTheConfigFromTheFile(t *testing.T) {
	_, id := namedClusterForTest(t)
	path := writeFixture(t, "apiVersion: cli.educates.dev/v1alpha1\nkind: EducatesLocalConfig\n")

	cfg, gotPath, created, err := loadLocalConfig(io.Discard, &LocalClusterCreateOptions{Config: path}, id)
	if err != nil {
		t.Fatalf("loadLocalConfig: %v", err)
	}

	if gotPath != path || created {
		t.Errorf("path, created = %q, %v, want %q, false", gotPath, created, path)
	}
	if got := cfg.(*v1alpha1.EducatesLocalConfig).Ingress.Domain; got != "test2.192-168-50-151.nip.io" {
		t.Errorf("ingress.domain = %q, want the named cluster's nip.io fallback", got)
	}
	if _, err := os.Stat(id.ConfigPath); !os.IsNotExist(err) {
		t.Errorf("a config was written in the data home: stat err = %v", err)
	}
}

func TestLoadLocalConfig_NamedClusterWithConfigFlag_MakesTheClusterFolder(t *testing.T) {
	_, id := namedClusterForTest(t)
	path := writeFixture(t, "apiVersion: cli.educates.dev/v1alpha1\nkind: EducatesLocalConfig\n")

	if _, _, _, err := loadLocalConfig(io.Discard, &LocalClusterCreateOptions{Config: path}, id); err != nil {
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
	path := writeFixture(t, "apiVersion: cli.educates.dev/v1alpha1\nkind: EducatesLocalConfig\n"+
		"cluster:\n  nodes:\n  - role: control-plane\n  - role: control-plane\n")

	_, _, _, err := loadLocalConfig(io.Discard, &LocalClusterCreateOptions{Config: path}, id)

	wantError(t, err, path, "cluster.nodes")
}

func TestLoadLocalConfig_ConfigWritingFlagsNeedNoConfigFlag(t *testing.T) {
	dataHome, id := namedClusterForTest(t)
	path := writeFixture(t, "apiVersion: cli.educates.dev/v1alpha1\nkind: EducatesLocalConfig\n")

	for _, target := range []localclusters.Identity{id, localclusters.Default(dataHome)} {
		_, _, _, err := loadLocalConfig(io.Discard, &LocalClusterCreateOptions{Config: path, Domain: "test2.educates.test"}, target)
		wantError(t, err, "--domain", "--config")
	}
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

func TestLoadLocalConfig_NamedCluster_ADomainThatDisagreesWithTheConfigIsAnError(t *testing.T) {
	const existing = "apiVersion: cli.educates.dev/v1alpha1\nkind: EducatesLocalConfig\n"
	_, id := namedClusterForTest(t)
	writeNamedConfig(t, id, existing)

	_, _, _, err := loadLocalConfig(io.Discard, &LocalClusterCreateOptions{Domain: "test2.educates.test"}, id)

	wantError(t, err, id.ConfigPath, "disagrees", "--domain test2.educates.test", "declares none",
		"nip.io name of the router address")
	if body, _ := os.ReadFile(id.ConfigPath); string(body) != existing {
		t.Errorf("the config was changed to:\n%s", body)
	}
}

func TestLoadLocalConfig_NamedCluster_ADomainThatAgreesWithTheConfigIsAccepted(t *testing.T) {
	_, id := namedClusterForTest(t)
	writeNamedConfig(t, id, "apiVersion: cli.educates.dev/v1alpha1\nkind: EducatesLocalConfig\n"+
		"ingress:\n  domain: test2.educates.test\n")

	cfg, _, created, err := loadLocalConfig(io.Discard, &LocalClusterCreateOptions{Domain: "test2.educates.test"}, id)
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

// defaultClusterForTest sets a temporary data home, which does not exist
// yet, and returns it with the identity of its default local cluster.
func defaultClusterForTest(t *testing.T) (string, localclusters.Identity) {
	t.Helper()
	dataHome := filepath.Join(t.TempDir(), "educates")
	t.Setenv("EDUCATES_CLI_DATA_HOME", dataHome)
	return dataHome, localclusters.Default(dataHome)
}

func TestLoadLocalConfig_DefaultCluster_WritesTheConfigAndTheRouterFileWhenItHasNone(t *testing.T) {
	stubHostIP(t, "192.168.50.151")
	dataHome, id := defaultClusterForTest(t)

	cfg, path, created, err := loadLocalConfig(io.Discard, &LocalClusterCreateOptions{}, id)
	if err != nil {
		t.Fatalf("loadLocalConfig: %v", err)
	}

	if !created || path != id.ConfigPath {
		t.Errorf("created, path = %t, %q, want true, %q", created, path, id.ConfigPath)
	}
	if body, _ := os.ReadFile(id.ConfigPath); string(body) != minimalLocalConfigYAML {
		t.Errorf("create wrote:\n%s\nwant:\n%s", body, minimalLocalConfigYAML)
	}
	wantRouterFile(t, dataHome, hostIPRouterFile)
	if got := cfg.(*v1alpha1.EducatesLocalConfig).Ingress.Domain; got != "192-168-50-151.nip.io" {
		t.Errorf("ingress.domain = %q, want the nip.io name of the router address", got)
	}
}

// With no router file and no host IP address to write in one, create
// fails saying the router address must be set, and writes no config.
func TestLoadLocalConfig_DefaultCluster_WithNoRouterFileOrHostIPFailsSayingWhatToSet(t *testing.T) {
	stubHostIP(t, "")
	_, id := defaultClusterForTest(t)

	_, _, _, err := loadLocalConfig(io.Discard, &LocalClusterCreateOptions{}, id)

	wantError(t, err, "router.yaml", "must be set", "educates local router update --address <ip>")
	if _, err := os.Stat(id.ConfigPath); !os.IsNotExist(err) {
		t.Errorf("a config was written: stat err = %v", err)
	}
}

func TestLoadLocalConfig_DefaultCluster_WritesTheDomainGiven(t *testing.T) {
	stubHostIP(t, "192.168.50.151")
	_, id := defaultClusterForTest(t)

	cfg, _, created, err := loadLocalConfig(io.Discard, &LocalClusterCreateOptions{Domain: "educates.test"}, id)
	if err != nil {
		t.Fatalf("loadLocalConfig: %v", err)
	}

	written := declaredConfig(t, id.ConfigPath)
	if !created || written.Ingress.Domain != "educates.test" {
		t.Errorf("created %t, wrote ingress.domain %q; want true, educates.test", created, written.Ingress.Domain)
	}
	if got := cfg.(*v1alpha1.EducatesLocalConfig).Ingress.Domain; got != "educates.test" {
		t.Errorf("ingress.domain = %q, want educates.test", got)
	}
}

// The default config of a 4.0 pre-release declares the address its
// cluster was served on, which becomes the router address before create
// would write the host's IP address there, whichever local cluster is
// created.
func TestLoadLocalConfig_TakesTheRouterAddressFromTheDefaultConfigsListenAddress(t *testing.T) {
	for _, cluster := range []string{"", "test2"} {
		t.Run("cluster "+cluster, func(t *testing.T) {
			stubHostIP(t, "192.168.50.151")
			dataHome, id := defaultClusterForTest(t)
			writeLocalClusterListFile(t, config.LocalConfigPath(dataHome),
				"apiVersion: cli.educates.dev/v1alpha1\nkind: EducatesLocalConfig\ncluster:\n  listenAddress: 10.10.10.1\n")
			wantDomain := "10-10-10-1.nip.io"
			if cluster != "" {
				var err error
				if id, err = localclusters.Named(cluster, dataHome); err != nil {
					t.Fatal(err)
				}
				wantDomain = cluster + "." + wantDomain
			}

			cfg, _, _, err := loadLocalConfig(io.Discard, &LocalClusterCreateOptions{}, id)
			if err != nil {
				t.Fatalf("loadLocalConfig: %v", err)
			}

			if router, err := config.LoadLocalRouter(dataHome); err != nil || router.Address != "10.10.10.1" {
				t.Errorf("router.yaml declares %+v, %v, want the default config's listen address 10.10.10.1", router, err)
			}
			if got := cfg.(*v1alpha1.EducatesLocalConfig).Ingress.Domain; got != wantDomain {
				t.Errorf("ingress.domain = %q, want %q", got, wantDomain)
			}
			if body, _ := os.ReadFile(config.LocalConfigPath(dataHome)); strings.Contains(string(body), "listenAddress") {
				t.Errorf("the default config still declares a listen address:\n%s", body)
			}
		})
	}
}

func TestLoadLocalConfig_DefaultCluster_KeepsAnExistingConfig(t *testing.T) {
	stubHostIP(t, "192.168.50.151")
	_, id := defaultClusterForTest(t)
	existing := "# hand-edited config\napiVersion: cli.educates.dev/v1alpha1\nkind: EducatesLocalConfig\n" +
		"ingress:\n  domain: educates.test\n"
	writeNamedConfig(t, id, existing)

	_, _, created, err := loadLocalConfig(io.Discard, &LocalClusterCreateOptions{}, id)
	if err != nil {
		t.Fatalf("loadLocalConfig: %v", err)
	}
	if created {
		t.Errorf("created = true, want false for an existing config")
	}
	if body, _ := os.ReadFile(id.ConfigPath); string(body) != existing {
		t.Errorf("the existing config was overwritten:\n%s", body)
	}
}

func TestLoadLocalConfig_DefaultCluster_MigratesAV3InstallInsteadOfWritingOne(t *testing.T) {
	stubHostIP(t, "192.168.50.151")
	dataHome, id := defaultClusterForTest(t)
	writeLocalClusterListFile(t, filepath.Join(dataHome, "values.yaml"),
		"clusterInfrastructure:\n  provider: kind\nclusterIngress:\n  domain: educates.test\n"+
			"localKindCluster:\n  listenAddress: 10.10.10.1\n")

	cfg, _, created, err := loadLocalConfig(io.Discard, &LocalClusterCreateOptions{}, id)
	if err != nil {
		t.Fatalf("loadLocalConfig: %v", err)
	}

	if created {
		t.Errorf("created = true, want false for a migrated v3 install")
	}
	if got := cfg.(*v1alpha1.EducatesLocalConfig).Ingress.Domain; got != "educates.test" {
		t.Errorf("ingress.domain = %q, want the v3 domain educates.test", got)
	}
}

func TestLoadLocalConfig_DefaultCluster_NonKindV3InstallRefusesWithoutWritingOne(t *testing.T) {
	dataHome, id := defaultClusterForTest(t)
	writeLocalClusterListFile(t, filepath.Join(dataHome, "values.yaml"),
		"clusterInfrastructure:\n  provider: gke\n")

	_, _, created, err := loadLocalConfig(io.Discard, &LocalClusterCreateOptions{}, id)

	wantError(t, err, "provider")
	if created {
		t.Errorf("created = true, want false when the migration refuses")
	}
	if _, err := os.Stat(id.ConfigPath); !os.IsNotExist(err) {
		t.Errorf("a default config was written over an unmigratable v3 install: stat err = %v", err)
	}
}
