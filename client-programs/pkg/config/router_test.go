package config

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const routerHeader = "# yaml-language-server: $schema=https://schemas.educates.dev/cli/v1alpha1/EducatesLocalRouter.json\n" +
	"apiVersion: cli.educates.dev/v1alpha1\nkind: EducatesLocalRouter\n"

func TestLocalRouterPathIsRouterYAMLInTheDataHome(t *testing.T) {
	if got, want := LocalRouterPath("/home/me/.local/share/educates"), "/home/me/.local/share/educates/router.yaml"; got != want {
		t.Errorf("LocalRouterPath() = %q, want %q", got, want)
	}
}

func TestLoadLocalRouterReturnsTheAddressTheFileDeclares(t *testing.T) {
	dataHome := t.TempDir()
	must(t, os.WriteFile(filepath.Join(dataHome, "router.yaml"), []byte(routerHeader+"address: 10.10.10.1\n"), 0o644))

	got, err := LoadLocalRouter(dataHome)
	if err != nil {
		t.Fatalf("LoadLocalRouter: %v", err)
	}

	if got.Address != "10.10.10.1" {
		t.Errorf("LoadLocalRouter().Address = %q, want 10.10.10.1", got.Address)
	}
}

// A data home without the file declares no router address, which is an
// error a caller tells apart, since the CLI writes a missing file.
func TestLoadLocalRouterOfADataHomeWithoutTheFileIsNotExist(t *testing.T) {
	dataHome := t.TempDir()

	_, err := LoadLocalRouter(dataHome)

	if !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("LoadLocalRouter() error = %v, want one wrapping fs.ErrNotExist", err)
	}
	if !strings.Contains(err.Error(), filepath.Join(dataHome, "router.yaml")) {
		t.Errorf("LoadLocalRouter() error %q does not name the file", err)
	}
}

// Creating the router file in a data home without one writes it, with
// the data home, and a comment on an address that is the host's IP
// address saying that an alias survives network changes.
func TestCreateLocalRouterWritesTheMissingFile(t *testing.T) {
	dataHome := filepath.Join(t.TempDir(), "educates")

	created, err := CreateLocalRouter(dataHome, "192.168.50.151", true)
	if err != nil || !created {
		t.Fatalf("CreateLocalRouter() = %t, %v, want true, nil", created, err)
	}

	path := filepath.Join(dataHome, "router.yaml")
	want := routerHeader +
		"# This host's IP address when this file was written. An alias such as\n" +
		"# 10.10.10.1, added to a network interface, survives network changes.\n" +
		"address: 192.168.50.151\n"
	if got := string(readFile(t, path)); got != want {
		t.Errorf("router.yaml =\n%s\nwant\n%s", got, want)
	}
	assertMode(t, path, 0o644)
	if router, err := LoadLocalRouter(dataHome); err != nil || router.Address != "192.168.50.151" {
		t.Errorf("LoadLocalRouter() after CreateLocalRouter = %+v, %v", router, err)
	}
}

// An address that is not the host's IP address, such as one a user chose,
// is written without the comment.
func TestCreateLocalRouterOfAnAddressThatIsNotTheHostIPWritesNoComment(t *testing.T) {
	dataHome := t.TempDir()

	if _, err := CreateLocalRouter(dataHome, "10.10.10.1", false); err != nil {
		t.Fatalf("CreateLocalRouter: %v", err)
	}

	want := routerHeader + "address: 10.10.10.1\n"
	if got := string(readFile(t, filepath.Join(dataHome, "router.yaml"))); got != want {
		t.Errorf("router.yaml =\n%s\nwant\n%s", got, want)
	}
}

// handEditedRouter is a router file as a user writes it, with comments.
const handEditedRouter = routerHeader +
	"# The alias on bridge1.\n" +
	"address: 10.10.10.1 # added by my bridge script\n"

// An existing router file is never overwritten, whatever it holds: one
// that does not load is left for the loader to report.
func TestCreateLocalRouterLeavesAnExistingFileAsItIs(t *testing.T) {
	for name, body := range map[string]string{
		"a hand-edited file":        handEditedRouter,
		"a file that does not load": routerHeader + "address: 10.10.10.1\nport: 80\n",
	} {
		t.Run(name, func(t *testing.T) {
			dataHome := t.TempDir()
			path := filepath.Join(dataHome, "router.yaml")
			must(t, os.WriteFile(path, []byte(body), 0o600))

			created, err := CreateLocalRouter(dataHome, "192.168.50.151", true)

			if err != nil || created {
				t.Fatalf("CreateLocalRouter() = %t, %v, want false, nil", created, err)
			}
			if got := string(readFile(t, path)); got != body {
				t.Errorf("router.yaml changed to\n%s", got)
			}
			assertMode(t, path, 0o600)
		})
	}
}

// Setting the router address replaces it where it is, and keeps the rest
// of the file as it was, comments and permissions included.
func TestSetLocalRouterAddressKeepsTheRestOfTheFile(t *testing.T) {
	dataHome := t.TempDir()
	path := filepath.Join(dataHome, "router.yaml")
	must(t, os.WriteFile(path, []byte(handEditedRouter), 0o600))

	must(t, SetLocalRouterAddress(dataHome, "10.10.10.2"))

	want := routerHeader +
		"# The alias on bridge1.\n" +
		"address: 10.10.10.2 # added by my bridge script\n"
	if got := string(readFile(t, path)); got != want {
		t.Errorf("router.yaml =\n%s\nwant\n%s", got, want)
	}
	assertMode(t, path, 0o600)
}

// The comment the CLI writes on the host's IP address stops being true
// once another address replaces it, so it goes with it.
func TestSetLocalRouterAddressDropsTheHostIPComment(t *testing.T) {
	dataHome := t.TempDir()
	if _, err := CreateLocalRouter(dataHome, "192.168.50.151", true); err != nil {
		t.Fatal(err)
	}

	must(t, SetLocalRouterAddress(dataHome, "10.10.10.1"))

	want := routerHeader + "address: 10.10.10.1\n"
	if got := string(readFile(t, filepath.Join(dataHome, "router.yaml"))); got != want {
		t.Errorf("router.yaml =\n%s\nwant\n%s", got, want)
	}
}

// Setting the address the file already declares leaves it as it is,
// comment included.
func TestSetLocalRouterAddressToTheSameAddressLeavesTheFile(t *testing.T) {
	dataHome := t.TempDir()
	if _, err := CreateLocalRouter(dataHome, "192.168.50.151", true); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dataHome, "router.yaml")
	before := string(readFile(t, path))

	must(t, SetLocalRouterAddress(dataHome, "192.168.50.151"))

	if got := string(readFile(t, path)); got != before {
		t.Errorf("router.yaml =\n%s\nwant it unchanged:\n%s", got, before)
	}
}

// Setting the address in a data home without the router file creates it.
func TestSetLocalRouterAddressCreatesTheFileWhenMissing(t *testing.T) {
	dataHome := filepath.Join(t.TempDir(), "educates")

	must(t, SetLocalRouterAddress(dataHome, "10.10.10.1"))

	path := filepath.Join(dataHome, "router.yaml")
	if got, want := string(readFile(t, path)), routerHeader+"address: 10.10.10.1\n"; got != want {
		t.Errorf("router.yaml =\n%s\nwant\n%s", got, want)
	}
	assertMode(t, path, 0o644)
}

// A router file that doesn't load is left as it is, and the error names
// it and why.
func TestSetLocalRouterAddressRefusesAFileThatDoesNotLoad(t *testing.T) {
	dataHome := t.TempDir()
	path := filepath.Join(dataHome, "router.yaml")
	body := routerHeader + "address: 10.10.10.1\nport: 80\n"
	must(t, os.WriteFile(path, []byte(body), 0o644))

	err := SetLocalRouterAddress(dataHome, "10.10.10.2")

	if err == nil || !strings.Contains(err.Error(), path) || !strings.Contains(err.Error(), "port") {
		t.Errorf("SetLocalRouterAddress() error = %v, want one naming %s and the field port", err, path)
	}
	if got := string(readFile(t, path)); got != body {
		t.Errorf("router.yaml changed to\n%s", got)
	}
}

func TestLoadLocalRouterOfAFileThatDoesNotLoadNamesItsPathAndWhy(t *testing.T) {
	cases := []struct {
		name string
		body string
		why  []string
	}{
		{
			name: "no address",
			body: routerHeader,
			why:  []string{"schema validation failed", "address"},
		},
		{
			name: "an empty address",
			body: routerHeader + "address: \"\"\n",
			why:  []string{"schema validation failed", "address"},
		},
		{
			name: "an address that is not a string",
			body: routerHeader + "address: 10\n",
			why:  []string{"schema validation failed", "address"},
		},
		{
			name: "an unknown field",
			body: routerHeader + "address: 10.10.10.1\nport: 80\n",
			why:  []string{"schema validation failed", "port"},
		},
		{
			name: "another kind",
			body: "apiVersion: cli.educates.dev/v1alpha1\nkind: EducatesLocalMirrors\n",
			why:  []string{`kind "EducatesLocalMirrors"`, "EducatesLocalRouter"},
		},
		{
			name: "no kind",
			body: "address: 10.10.10.1\n",
			why:  []string{"apiVersion", "kind"},
		},
		{
			name: "another apiVersion",
			body: "apiVersion: cli.educates.dev/v1beta1\nkind: EducatesLocalRouter\naddress: 10.10.10.1\n",
			why:  []string{`apiVersion "cli.educates.dev/v1beta1"`},
		},
		{
			name: "not YAML",
			body: "address: [\n",
			why:  []string{"parse YAML"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dataHome := t.TempDir()
			path := filepath.Join(dataHome, "router.yaml")
			must(t, os.WriteFile(path, []byte(tc.body), 0o644))

			_, err := LoadLocalRouter(dataHome)

			if err == nil {
				t.Fatal("LoadLocalRouter() = nil error, want one")
			}
			if errors.Is(err, fs.ErrNotExist) {
				t.Errorf("LoadLocalRouter() error %q wraps fs.ErrNotExist for a file that exists", err)
			}
			for _, want := range append([]string{path}, tc.why...) {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("LoadLocalRouter() error %q does not mention %q", err, want)
				}
			}
		})
	}
}
