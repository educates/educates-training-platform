package localclusters

import (
	"errors"
	"io/fs"
	"testing"
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

func TestLoadConfigAppliesTheRulesOfTheCluster(t *testing.T) {
	dataHome := t.TempDir()
	writeClusterConfig(t, dataHome, "", localConfigHeader)
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

	if got, want := def.ListenAddress(), "127.0.0.1"; got != want {
		t.Errorf("default cluster's ListenAddress() = %q, want the static default %q", got, want)
	}
	if got, want := test2.ListenAddress(), "10.10.10.2"; got != want {
		t.Errorf("test2's ListenAddress() = %q, want %q", got, want)
	}
	if test2.Source != test2.Identity.ConfigPath {
		t.Errorf("test2's Source = %q, want its config path %q", test2.Source, test2.Identity.ConfigPath)
	}
	wantErrorMentioning(t, test3Err, "loopback")
}

func TestLoadedConfigDomain(t *testing.T) {
	dataHome := t.TempDir()
	cases := []struct {
		name    string
		cluster string
		body    string
		hostIP  string
		want    string
	}{
		{name: "a declared domain", body: localConfigHeader + "ingress:\n  domain: educates.test\n",
			hostIP: "192.168.1.10", want: "educates.test"},
		{name: "the default cluster's host IP nip.io domain", body: localConfigHeader,
			hostIP: "192.168.1.10", want: "192-168-1-10.nip.io"},
		{name: "no domain for the default cluster without a host IP", body: localConfigHeader},
		{name: "a named cluster's listen address nip.io domain", cluster: "test2",
			body: localConfigHeader + "cluster:\n  listenAddress: 10.10.10.2\n", hostIP: "192.168.1.10", want: "10-10-10-2.nip.io"},
		{name: "the domain an EducatesConfig declares", body: escapeConfig("", "workshops.example.test"),
			hostIP: "192.168.1.10", want: "workshops.example.test"},
		{name: "no domain for an EducatesConfig that declares none", body: escapeConfig("", ""), hostIP: "192.168.1.10"},
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

			if got := loaded.Domain(tc.hostIP); got != tc.want {
				t.Errorf("Domain(%q) = %q, want %q", tc.hostIP, got, tc.want)
			}
		})
	}
}
