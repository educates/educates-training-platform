package config

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// configWithListenAddress is a default config, as a 4.0 pre-release wrote
// it before every local cluster was served on the router address.
const configWithListenAddress = localConfigHeader +
	"cluster:\n" +
	"  # This host's IP address when this file was written. An alias such as\n" +
	"  # 10.10.10.1, added to a network interface, survives network changes.\n" +
	"  listenAddress: 10.10.10.1\n" +
	"  apiServer:\n" +
	"    port: 6443\n" +
	"ingress:\n" +
	"  domain: educates.test # resolved by the local resolver\n"

// configWithoutListenAddress is configWithListenAddress once its listen
// address is moved.
const configWithoutListenAddress = localConfigHeader +
	"cluster:\n" +
	"  apiServer:\n" +
	"    port: 6443\n" +
	"ingress:\n" +
	"  domain: educates.test # resolved by the local resolver\n"

// Loading the default config of the data home moves its listen address
// to the router file first, once, so it loads, and the previous config is
// kept beside it.
func TestLoadMovesTheListenAddressOfTheDefaultConfigToTheRouterFile(t *testing.T) {
	dataHome := t.TempDir()
	t.Setenv("EDUCATES_CLI_DATA_HOME", dataHome)
	path := writeDataHomeFile(t, dataHome, "config.yaml", configWithListenAddress)

	for range 2 {
		if _, err := Load(path); err != nil {
			t.Fatalf("Load: %v", err)
		}
	}

	router, err := LoadLocalRouter(dataHome)
	if err != nil || router.Address != "10.10.10.1" {
		t.Errorf("router.yaml declares %+v, %v, want the address 10.10.10.1", router, err)
	}
	if got := string(readFile(t, path)); got != configWithoutListenAddress {
		t.Errorf("config.yaml =\n%s\nwant\n%s", got, configWithoutListenAddress)
	}
	if got := string(readFile(t, path+".bak")); got != configWithListenAddress {
		t.Errorf("config.yaml.bak =\n%s\nwant the previous config", got)
	}
	if backups, _ := filepath.Glob(path + ".*.bak"); len(backups) != 0 {
		t.Errorf("a second load backed the config up again: %v", backups)
	}
}

func TestMoveClusterListenAddressOfTheDefaultConfigToTheRouterFile(t *testing.T) {
	dataHome := t.TempDir()
	path := writeDataHomeFile(t, dataHome, "config.yaml", configWithListenAddress)
	var note bytes.Buffer

	if err := MoveClusterListenAddress(dataHome, path, &note); err != nil {
		t.Fatalf("MoveClusterListenAddress: %v", err)
	}

	routerPath := filepath.Join(dataHome, "router.yaml")
	if got, want := string(readFile(t, routerPath)), routerHeader+"address: 10.10.10.1\n"; got != want {
		t.Errorf("router.yaml =\n%s\nwant\n%s", got, want)
	}
	if got := string(readFile(t, path)); got != configWithoutListenAddress {
		t.Errorf("config.yaml =\n%s\nwant\n%s", got, configWithoutListenAddress)
	}
	backup := path + ".bak"
	assertMode(t, backup, 0o600)
	assertMode(t, path, 0o644)
	for _, want := range []string{"moved cluster.listenAddress of " + path, "10.10.10.1", "to " + routerPath,
		"router address", "every local cluster", backup} {
		if !strings.Contains(note.String(), want) {
			t.Errorf("note does not mention %q:\n%s", want, note.String())
		}
	}
	if strings.Count(note.String(), "\n") != 1 {
		t.Errorf("note is not one line:\n%s", note.String())
	}
}

// The router address is the one address every local cluster is served
// on, so an existing router file is never changed: a named cluster's
// listen address, or the default cluster's once the file exists, is
// removed, and the previous config kept beside it.
func TestMoveClusterListenAddressDropsItWhenTheRouterFileExistsOrTheClusterIsNamed(t *testing.T) {
	cases := []struct {
		name, rel    string
		router       string
		wantRouter   string
		wantNoRouter bool
	}{
		{name: "a named cluster's config", rel: "clusters/test2/config.yaml", wantNoRouter: true},
		{name: "a named cluster's config beside a router file", rel: "clusters/test2/config.yaml",
			router: routerHeader + "# Mine.\naddress: 10.10.10.9\n", wantRouter: routerHeader + "# Mine.\naddress: 10.10.10.9\n"},
		{name: "the default config beside a router file", rel: "config.yaml",
			router: routerHeader + "# Mine.\naddress: 10.10.10.9\n", wantRouter: routerHeader + "# Mine.\naddress: 10.10.10.9\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dataHome := t.TempDir()
			path := writeDataHomeFile(t, dataHome, tc.rel, configWithListenAddress)
			routerPath := filepath.Join(dataHome, "router.yaml")
			if tc.router != "" {
				writeDataHomeFile(t, dataHome, "router.yaml", tc.router)
			}
			var note bytes.Buffer

			if err := MoveClusterListenAddress(dataHome, path, &note); err != nil {
				t.Fatalf("MoveClusterListenAddress: %v", err)
			}

			if got := string(readFile(t, path)); got != configWithoutListenAddress {
				t.Errorf("config =\n%s\nwant\n%s", got, configWithoutListenAddress)
			}
			if got := string(readFile(t, path+".bak")); got != configWithListenAddress {
				t.Errorf("backup =\n%s\nwant the previous config", got)
			}
			if tc.wantNoRouter {
				if _, err := os.Stat(routerPath); !os.IsNotExist(err) {
					t.Errorf("router.yaml was written for a named cluster's listen address: %v", err)
				}
			} else if got := string(readFile(t, routerPath)); got != tc.wantRouter {
				t.Errorf("router.yaml =\n%s\nwant it left as it was", got)
			}
			for _, want := range []string{"removed cluster.listenAddress 10.10.10.1 from " + path, routerPath,
				"router address", "every local cluster", path + ".bak"} {
				if !strings.Contains(note.String(), want) {
					t.Errorf("note does not mention %q:\n%s", want, note.String())
				}
			}
			if strings.Count(note.String(), "\n") != 1 {
				t.Errorf("note is not one line:\n%s", note.String())
			}
		})
	}
}

// An EducatesConfig for a local cluster declares its listen address in
// target.cluster, and moves the same way, its cluster block with it once
// nothing else is left in it.
func TestMoveClusterListenAddressOfAnEducatesConfig(t *testing.T) {
	dataHome := t.TempDir()
	path := writeDataHomeFile(t, dataHome, "config.yaml", "apiVersion: cli.educates.dev/v1alpha1\nkind: EducatesConfig\n"+
		"target:\n  provider: kind\n  cluster:\n    listenAddress: 10.10.10.2\n")
	var note bytes.Buffer

	if err := MoveClusterListenAddress(dataHome, path, &note); err != nil {
		t.Fatalf("MoveClusterListenAddress: %v", err)
	}

	if router, err := LoadLocalRouter(dataHome); err != nil || router.Address != "10.10.10.2" {
		t.Errorf("router.yaml declares %+v, %v, want the address 10.10.10.2", router, err)
	}
	want := "apiVersion: cli.educates.dev/v1alpha1\nkind: EducatesConfig\ntarget:\n  provider: kind\n"
	if got := string(readFile(t, path)); got != want {
		t.Errorf("config.yaml =\n%s\nwant\n%s", got, want)
	}
	if !strings.Contains(note.String(), "moved target.cluster.listenAddress of "+path) {
		t.Errorf("note does not name target.cluster.listenAddress of %s:\n%s", path, note.String())
	}
}

// The default config a 4.0 pre-release's 'local config init' wrote holds
// nothing but the listen address in its cluster block, which goes with it.
func TestMoveClusterListenAddressRemovesTheClusterBlockItLeavesEmpty(t *testing.T) {
	dataHome := t.TempDir()
	path := writeDataHomeFile(t, dataHome, "config.yaml", localConfigHeader+
		"cluster:\n  # This host's IP address when this file was written.\n  listenAddress: 192.168.50.151\n")

	if err := MoveClusterListenAddress(dataHome, path, io.Discard); err != nil {
		t.Fatalf("MoveClusterListenAddress: %v", err)
	}

	if got := string(readFile(t, path)); got != localConfigHeader {
		t.Errorf("config.yaml =\n%s\nwant\n%s", got, localConfigHeader)
	}
}

// Only a specific IP address of the host can be the router address, so
// the default config's 0.0.0.0, loopback or empty listen address is
// dropped, and no router file written: the command that needs one writes
// the host's IP address in it.
func TestMoveClusterListenAddressDropsADefaultAddressThatIsNotSpecific(t *testing.T) {
	for _, address := range []string{"0.0.0.0", "127.0.0.1", `"::"`, "localhost", `""`, "null"} {
		t.Run(address, func(t *testing.T) {
			dataHome := t.TempDir()
			path := writeDataHomeFile(t, dataHome, "config.yaml", localConfigHeader+
				"cluster:\n  listenAddress: "+address+"\n  apiServer:\n    port: 6443\n")
			var note bytes.Buffer

			if err := MoveClusterListenAddress(dataHome, path, &note); err != nil {
				t.Fatalf("MoveClusterListenAddress: %v", err)
			}

			if _, err := os.Stat(filepath.Join(dataHome, "router.yaml")); !os.IsNotExist(err) {
				t.Errorf("router.yaml was written: %v", err)
			}
			if got, want := string(readFile(t, path)), localConfigHeader+"cluster:\n  apiServer:\n    port: 6443\n"; got != want {
				t.Errorf("config.yaml =\n%s\nwant\n%s", got, want)
			}
			if !strings.Contains(note.String(), "removed cluster.listenAddress") || strings.Count(note.String(), "\n") != 1 {
				t.Errorf("note is not one line saying the listen address was removed:\n%s", note.String())
			}
		})
	}
}

// A config anywhere but where the default and named local clusters keep
// theirs in the data home is never rewritten, and nor is one without a
// listen address.
func TestMoveClusterListenAddressLeavesAnyOtherConfig(t *testing.T) {
	dataHome := t.TempDir()
	cases := map[string]string{
		"elsewhere/config.yaml":     configWithListenAddress,
		"clusters/test2/other.yaml": configWithListenAddress,
		"my-config.yaml":            configWithListenAddress,
		"config.yaml":               configWithoutListenAddress,
	}
	for rel, body := range cases {
		t.Run(rel, func(t *testing.T) {
			path := writeDataHomeFile(t, dataHome, rel, body)
			outside := writeDataHomeFile(t, t.TempDir(), "config.yaml", body)
			var note bytes.Buffer

			for _, p := range []string{path, outside} {
				if err := MoveClusterListenAddress(dataHome, p, &note); err != nil {
					t.Fatalf("MoveClusterListenAddress(%s): %v", p, err)
				}
				if got := string(readFile(t, p)); got != body {
					t.Errorf("%s was rewritten:\n%s", p, got)
				}
				if _, err := os.Stat(p + ".bak"); !os.IsNotExist(err) {
					t.Errorf("%s was backed up: %v", p, err)
				}
			}
			if note.Len() != 0 {
				t.Errorf("note written for a config that did not move:\n%s", note.String())
			}
			if _, err := os.Stat(filepath.Join(dataHome, "router.yaml")); !os.IsNotExist(err) {
				t.Errorf("router.yaml was written: %v", err)
			}
		})
	}
}
