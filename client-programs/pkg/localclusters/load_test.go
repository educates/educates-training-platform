package localclusters

import (
	"errors"
	"io/fs"
	"strings"
	"testing"

	"github.com/educates/educates-training-platform/client-programs/pkg/config/hostinfo"
	"github.com/educates/educates-training-platform/client-programs/pkg/config/v1alpha1"
)

func TestLoadConfigOfAMissingNamedConfigSaysHowToWriteOne(t *testing.T) {
	dataHome := t.TempDir()
	id := namedID(t, "test2", dataHome)

	_, err := LoadConfig(id)

	wantErrorMentioning(t, err, `local cluster "test2" has no config`, id.ConfigPath,
		"educates local config init --local-cluster test2\n",
		"educates local cluster create --local-cluster test2\n",
		"--domain <domain>", "test2.<router address with dashes>.nip.io")
	if strings.Contains(err.Error(), "listen") {
		t.Errorf("error %q names a listen address, which a named cluster no longer needs", err)
	}
	if !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("errors.Is(%v, fs.ErrNotExist) = false, want true", err)
	}
}

func TestLoadConfigOfAMissingDefaultConfigSaysHowToWriteOne(t *testing.T) {
	_, err := LoadConfig(Default(t.TempDir()))

	wantErrorMentioning(t, err, "educates local config init")
	if !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("errors.Is(%v, fs.ErrNotExist) = false, want true", err)
	}
}

// A named cluster's config that still declares registry mirrors has them
// moved to mirrors.yaml when it is loaded, and then loads.
func TestLoadConfigMovesTheRegistryMirrorsOfANamedConfig(t *testing.T) {
	dataHome := t.TempDir()
	t.Setenv("EDUCATES_CLI_DATA_HOME", dataHome)
	routerFile(t, dataHome, routerHeader+"address: 10.10.10.1\n")
	writeClusterConfig(t, dataHome, "test2", localConfigHeader+
		"cluster:\n  registryMirrors:\n    - mirror: ghcr.io\n")

	if _, err := LoadConfig(namedID(t, "test2", dataHome)); err != nil {
		t.Fatalf("LoadConfig(test2): %v", err)
	}

	assertRecordedMirrors(t, dataHome, v1alpha1.RegistryMirror{Mirror: "ghcr.io"})
}

func TestLoadConfigAppliesTheRulesOfTheCluster(t *testing.T) {
	dataHome := t.TempDir()
	routerFile(t, dataHome, routerHeader+"address: 10.10.10.1\n")
	writeClusterConfig(t, dataHome, "", localConfigHeader)
	writeClusterConfig(t, dataHome, "test2", localConfigHeader)
	writeClusterConfig(t, dataHome, "test3", localConfigHeader+
		"cluster:\n  nodes:\n  - role: control-plane\n  - role: control-plane\n")

	def, err := LoadConfig(Default(dataHome))
	if err != nil {
		t.Fatalf("LoadConfig(default): %v", err)
	}
	test2, err := LoadConfig(namedID(t, "test2", dataHome))
	if err != nil {
		t.Fatalf("LoadConfig(test2): %v", err)
	}
	_, test3Err := LoadConfig(namedID(t, "test3", dataHome))

	if got, want := def.Domain(), "10-10-10-1.nip.io"; got != want {
		t.Errorf("default cluster's Domain() = %q, want %q", got, want)
	}
	if got, want := test2.Domain(), "test2.10-10-10-1.nip.io"; got != want {
		t.Errorf("test2's Domain() = %q, want %q", got, want)
	}
	if test2.Source != test2.Identity.ConfigPath {
		t.Errorf("test2's Source = %q, want its config path %q", test2.Source, test2.Identity.ConfigPath)
	}
	wantErrorMentioning(t, test3Err, "cluster.nodes", `2 nodes have role "control-plane"`)
}

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

// A config needs no listen address, and one it still declares, whatever
// it is, is no longer checked.
func TestLoadConfigTakesAConfigWithOrWithoutAListenAddress(t *testing.T) {
	cases := map[string]string{
		"no listen address":             localConfigHeader,
		"every address":                 localConfigHeader + "cluster:\n  listenAddress: 0.0.0.0\n",
		"loopback":                      localConfigHeader + "cluster:\n  listenAddress: 127.0.0.1\n",
		"an EducatesConfig without one": escapeConfig("  cluster: {}\n", ""),
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			for _, cluster := range []string{"", "test2"} {
				dataHome := t.TempDir()
				routerFile(t, dataHome, routerHeader+"address: 10.10.10.1\n")
				writeClusterConfig(t, dataHome, cluster, body)
				id := Default(dataHome)
				if cluster != "" {
					id = namedID(t, cluster, dataHome)
				}

				if _, err := LoadConfig(id); err != nil {
					t.Errorf("LoadConfig of %s: %v", id.Description(), err)
				}
			}
		})
	}
}

func TestLoadConfigOfTheDefaultClusterTakesOtherKindsAsTheyAre(t *testing.T) {
	dataHome := t.TempDir()
	writeClusterConfig(t, dataHome, "", "apiVersion: cli.educates.dev/v1alpha1\nkind: EducatesGKEConfig\n"+
		"gcp:\n  project: p\ndomain: academy.example.com\nacme:\n  email: ops@example.com\n")

	if _, err := LoadConfig(Default(dataHome)); err != nil {
		t.Errorf("LoadConfig of the default cluster's EducatesGKEConfig: %v", err)
	}
}

func TestLoadedConfigDomain(t *testing.T) {
	dataHome := t.TempDir()
	routerFile(t, dataHome, routerHeader+"address: 10.10.10.9\n")
	const listen = "cluster:\n  listenAddress: 10.10.10.1\n"
	const targetListen = "  cluster:\n    listenAddress: 10.10.10.1\n"
	cases := []struct {
		name    string
		cluster string
		body    string
		want    string
	}{
		{name: "a declared domain", body: localConfigHeader + listen + "ingress:\n  domain: educates.test\n",
			want: "educates.test"},
		{name: "the default cluster's router address nip.io domain", body: localConfigHeader + listen,
			want: "10-10-10-9.nip.io"},
		{name: "a named cluster's name under the router address nip.io domain", cluster: "test2",
			body: localConfigHeader + "cluster:\n  listenAddress: 10.10.10.2\n", want: "test2.10-10-10-9.nip.io"},
		{name: "the domain an EducatesConfig declares", body: escapeConfig(targetListen, "workshops.example.test"),
			want: "workshops.example.test"},
		{name: "no domain for an EducatesConfig that declares none", body: escapeConfig(targetListen, "")},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			writeClusterConfig(t, dataHome, tc.cluster, tc.body)
			id := Default(dataHome)
			if tc.cluster != "" {
				id = namedID(t, tc.cluster, dataHome)
			}
			loaded, err := LoadConfig(id)
			if err != nil {
				t.Fatalf("LoadConfig: %v", err)
			}

			if got := loaded.Domain(); got != tc.want {
				t.Errorf("Domain() = %q, want %q", got, tc.want)
			}
		})
	}
}

// The default local cluster, declaring no ingress domain, is served at the
// nip.io name of the router address, the one address every local cluster
// is served on, whatever its own listen address.
func TestLoadConfigOfTheDefaultClusterTakesTheNipDomainOfTheRouterAddress(t *testing.T) {
	dataHome := t.TempDir()
	routerFile(t, dataHome, routerHeader+"address: 10.10.10.1\n")
	writeClusterConfig(t, dataHome, "", localConfigHeader+"cluster:\n  listenAddress: 192.168.1.20\n")

	loaded, err := LoadConfig(Default(dataHome))
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}

	if got, want := loaded.Domain(), "10-10-10-1.nip.io"; got != want {
		t.Errorf("Domain() = %q, want %q", got, want)
	}
	if c := loaded.Config.(*v1alpha1.EducatesLocalConfig); !c.Ingress.Insecure {
		t.Errorf("ingress.insecure = false, want true for the nip.io domain")
	}
}

// A named local cluster declaring no ingress domain is served at its name
// under the nip.io name of the router address, so under the default local
// cluster's, and needs no address of its own.
func TestLoadConfigOfANamedClusterTakesItsNameUnderTheNipDomainOfTheRouterAddress(t *testing.T) {
	dataHome := t.TempDir()
	routerFile(t, dataHome, routerHeader+"address: 10.10.10.1\n")
	writeClusterConfig(t, dataHome, "test2", localConfigHeader)

	loaded, err := LoadConfig(namedID(t, "test2", dataHome))
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}

	if got, want := loaded.Domain(), "test2.10-10-10-1.nip.io"; got != want {
		t.Errorf("Domain() = %q, want %q", got, want)
	}
	if c := loaded.Config.(*v1alpha1.EducatesLocalConfig); !c.Ingress.Insecure {
		t.Errorf("ingress.insecure = false, want true for the nip.io domain")
	}
}

// The default local cluster's domain comes from the router address alone,
// so with no router file it says how to set the router address, whatever
// listen address its config still declares.
func TestLoadConfigOfTheDefaultClusterWithNoRouterFileSaysHowToSetTheRouterAddress(t *testing.T) {
	dataHome := t.TempDir()
	writeClusterConfig(t, dataHome, "", localConfigHeader+"cluster:\n  listenAddress: 192.168.1.20\n")
	id := Default(dataHome)

	_, err := LoadConfig(id)

	wantErrorMentioning(t, err, id.ConfigPath, "router.yaml", "educates local router update --address <ip>")
	if errors.Is(err, fs.ErrNotExist) {
		t.Errorf("errors.Is(%v, fs.ErrNotExist) = true, want false for a config that exists", err)
	}
}

func TestLoadConfigOfTheDefaultClusterRefusesARouterAddressWithNoNipName(t *testing.T) {
	dataHome := t.TempDir()
	routerFile(t, dataHome, routerHeader+"address: fd00::20\n")
	writeClusterConfig(t, dataHome, "", localConfigHeader+"cluster:\n  listenAddress: 192.168.1.20\n")
	id := Default(dataHome)

	_, err := LoadConfig(id)

	wantErrorMentioning(t, err, id.ConfigPath, "router address fd00::20", "nip.io", "set ingress.domain")
}

func TestLoadConfigOfTheDefaultClusterRefusesARouterAddressTheRulesRefuse(t *testing.T) {
	dataHome := t.TempDir()
	path := routerFile(t, dataHome, routerHeader+"address: 127.0.0.1\n")
	writeClusterConfig(t, dataHome, "", localConfigHeader+"cluster:\n  listenAddress: 192.168.1.20\n")

	_, err := LoadConfig(Default(dataHome))

	wantErrorMentioning(t, err, path, "loopback")
}

// A declared domain needs no router address.
func TestLoadConfigOfTheDefaultClusterKeepsADeclaredDomainWhateverTheRouterFile(t *testing.T) {
	dataHome := t.TempDir()
	routerFile(t, dataHome, routerHeader+"address: 127.0.0.1\n")
	writeClusterConfig(t, dataHome, "", localConfigHeader+
		"cluster:\n  listenAddress: 192.168.1.20\ningress:\n  domain: educates.test\n")

	loaded, err := LoadConfig(Default(dataHome))
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if got := loaded.Domain(); got != "educates.test" {
		t.Errorf("Domain() = %q, want educates.test", got)
	}
}
