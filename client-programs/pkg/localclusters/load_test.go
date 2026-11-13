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
		"educates local config init --local-cluster test2 --listen-address <ip>",
		"educates local cluster create --local-cluster test2 --listen-address <ip>")
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
	writeClusterConfig(t, dataHome, "test2", localConfigHeader+
		"cluster:\n  listenAddress: 10.10.10.2\n  registryMirrors:\n    - mirror: ghcr.io\n")

	if _, err := LoadConfig(namedID(t, "test2", dataHome)); err != nil {
		t.Fatalf("LoadConfig(test2): %v", err)
	}

	assertRecordedMirrors(t, dataHome, v1alpha1.RegistryMirror{Mirror: "ghcr.io"})
}

func TestLoadConfigAppliesTheRulesOfTheCluster(t *testing.T) {
	dataHome := t.TempDir()
	writeClusterConfig(t, dataHome, "", localConfigHeader+"cluster:\n  listenAddress: 10.10.10.1\n")
	writeClusterConfig(t, dataHome, "test2", localConfigHeader+"cluster:\n  listenAddress: 10.10.10.2\n")
	writeClusterConfig(t, dataHome, "test3", localConfigHeader+"cluster:\n  listenAddress: 127.0.0.1\n")

	def, err := LoadConfig(Default(dataHome))
	if err != nil {
		t.Fatalf("LoadConfig(default): %v", err)
	}
	test2, err := LoadConfig(namedID(t, "test2", dataHome))
	if err != nil {
		t.Fatalf("LoadConfig(test2): %v", err)
	}
	_, test3Err := LoadConfig(namedID(t, "test3", dataHome))

	if got, want := def.ListenAddress(), "10.10.10.1"; got != want {
		t.Errorf("default cluster's ListenAddress() = %q, want %q", got, want)
	}
	if got, want := test2.ListenAddress(), "10.10.10.2"; got != want {
		t.Errorf("test2's ListenAddress() = %q, want %q", got, want)
	}
	if test2.Source != test2.Identity.ConfigPath {
		t.Errorf("test2's Source = %q, want its config path %q", test2.Source, test2.Identity.ConfigPath)
	}
	wantErrorMentioning(t, test3Err, "loopback")
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

func TestLoadConfigRefusesADefaultConfigWithoutASpecificListenAddress(t *testing.T) {
	cases := []struct {
		name   string
		body   string
		reason []string
	}{
		{name: "missing", body: localConfigHeader, reason: []string{"must be set"}},
		{name: "every address", body: localConfigHeader + "cluster:\n  listenAddress: 0.0.0.0\n",
			reason: []string{"0.0.0.0", "every host address"}},
		{name: "loopback", body: localConfigHeader + "cluster:\n  listenAddress: 127.0.0.1\n",
			reason: []string{"127.0.0.1", "loopback", "pods resolve the cluster's ingress domain to it"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			stubHostIP(t, "192.168.50.151")
			dataHome := t.TempDir()
			writeClusterConfig(t, dataHome, "", tc.body)
			id := Default(dataHome)

			_, err := LoadConfig(id)

			wantErrorMentioning(t, err, append([]string{id.ConfigPath, "cluster.listenAddress",
				"educates local config set cluster.listenAddress 192.168.50.151",
				"An alias such as 10.10.10.1"}, tc.reason...)...)
		})
	}
}

func TestLoadConfigOfADefaultConfigWithoutAListenAddressSaysWhatToSetWithNoHostIP(t *testing.T) {
	stubHostIP(t, "")
	dataHome := t.TempDir()
	writeClusterConfig(t, dataHome, "", localConfigHeader)

	_, err := LoadConfig(Default(dataHome))

	wantErrorMentioning(t, err, "cluster.listenAddress", "must be set",
		"educates local config set cluster.listenAddress <ip>")
}

func TestLoadConfigOfANamedConfigWithoutAListenAddressSaysHowToSetIt(t *testing.T) {
	stubHostIP(t, "192.168.50.151")
	dataHome := t.TempDir()
	writeClusterConfig(t, dataHome, "test2", localConfigHeader)

	_, err := LoadConfig(namedID(t, "test2", dataHome))

	wantErrorMentioning(t, err, "cluster.listenAddress", "must be set",
		"educates local config set --local-cluster test2 cluster.listenAddress <ip>")
	if strings.Contains(err.Error(), "192.168.50.151") {
		t.Errorf("error %q suggests the host's IP address, which the default local cluster uses", err)
	}
}

func TestLoadConfigOfAnEducatesConfigWithoutAListenAddressNamesItsFile(t *testing.T) {
	stubHostIP(t, "192.168.50.151")
	dataHome := t.TempDir()
	writeClusterConfig(t, dataHome, "", escapeConfig("  cluster: {}\n", ""))
	id := Default(dataHome)

	_, err := LoadConfig(id)

	wantErrorMentioning(t, err, "target.cluster.listenAddress", "must be set",
		"Set it in "+id.ConfigPath+" to this host's IP address, 192.168.50.151")
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
		{name: "the default cluster's listen address nip.io domain", body: localConfigHeader + listen,
			want: "10-10-10-1.nip.io"},
		{name: "a named cluster's listen address nip.io domain", cluster: "test2",
			body: localConfigHeader + "cluster:\n  listenAddress: 10.10.10.2\n", want: "10-10-10-2.nip.io"},
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
