package cmd

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/educates/educates-training-platform/client-programs/pkg/config"
	"github.com/educates/educates-training-platform/client-programs/pkg/localclusters"
)

// hostIPRouterFile is the router file local config init and local cluster
// create write on a host whose IP address is 192.168.50.151.
const hostIPRouterFile = config.LocalRouterModeline + "\n" +
	"apiVersion: cli.educates.dev/v1alpha1\nkind: EducatesLocalRouter\n" +
	"# This host's IP address when this file was written. An alias such as\n" +
	"# 10.10.10.1, added to a network interface, survives network changes.\n" +
	"address: 192.168.50.151\n"

// handWrittenRouterFile is a router file a user wrote, with a comment.
const handWrittenRouterFile = "apiVersion: cli.educates.dev/v1alpha1\nkind: EducatesLocalRouter\n" +
	"# The alias on bridge1.\naddress: 10.10.10.1\n"

// writeRouterFile writes body as the router file of the data home
// dataHome.
func writeRouterFile(t *testing.T, dataHome, body string) {
	t.Helper()
	if err := os.MkdirAll(dataHome, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(config.LocalRouterPath(dataHome), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// wantRouterFile fails t unless the router file of the data home dataHome
// holds want.
func wantRouterFile(t *testing.T, dataHome, want string) {
	t.Helper()
	if body, err := os.ReadFile(config.LocalRouterPath(dataHome)); err != nil || string(body) != want {
		t.Errorf("router.yaml = %v\n%s\nwant\n%s", err, body, want)
	}
}

func TestLocalConfigInit_WritesTheRouterFileWithTheHostIPBesideTheConfig(t *testing.T) {
	stubHostIP(t, "192.168.50.151")
	dataHome := t.TempDir()
	t.Setenv("EDUCATES_CLI_DATA_HOME", dataHome)

	var out bytes.Buffer
	if err := (&ProjectInfo{}).runLocalConfigInit(&LocalConfigInitOptions{}, defaultLocalCluster(), &out); err != nil {
		t.Fatalf("init: %v", err)
	}

	wantRouterFile(t, dataHome, hostIPRouterFile)
	if _, err := os.Stat(filepath.Join(dataHome, "config.yaml")); err != nil {
		t.Errorf("init wrote no config.yaml beside router.yaml: %v", err)
	}
	if !strings.Contains(out.String(), config.LocalRouterPath(dataHome)) {
		t.Errorf("init output does not name router.yaml:\n%s", out.String())
	}
}

func TestLocalConfigInit_NamedCluster_WritesTheRouterFile(t *testing.T) {
	dataHome, id := namedClusterForTest(t)
	stubHostIP(t, "192.168.50.151")

	if err := (&ProjectInfo{}).runLocalConfigInit(&LocalConfigInitOptions{ListenAddress: "10.10.10.2"}, id, io.Discard); err != nil {
		t.Fatalf("init: %v", err)
	}

	wantRouterFile(t, dataHome, hostIPRouterFile)
}

// An existing router file is left as it is, comments included, whatever
// else init writes.
func TestLocalConfigInit_LeavesAnExistingRouterFile(t *testing.T) {
	for name, o := range map[string]*LocalConfigInitOptions{
		"a minimal config":         {},
		"the defaulted config":     {Defaults: true},
		"an overwritten config":    {Force: true},
		"the given listen address": {ListenAddress: "10.10.10.1"},
	} {
		t.Run(name, func(t *testing.T) {
			stubHostIP(t, "192.168.50.151")
			dataHome := t.TempDir()
			t.Setenv("EDUCATES_CLI_DATA_HOME", dataHome)
			writeRouterFile(t, dataHome, handWrittenRouterFile)

			var out bytes.Buffer
			if err := (&ProjectInfo{}).runLocalConfigInit(o, defaultLocalCluster(), &out); err != nil {
				t.Fatalf("init: %v", err)
			}

			wantRouterFile(t, dataHome, handWrittenRouterFile)
			if strings.Contains(out.String(), "router.yaml") {
				t.Errorf("init says it wrote router.yaml:\n%s", out.String())
			}
		})
	}
}

// With no router file and no host IP address to write in one, init fails
// saying the router address must be set, and writes nothing.
func TestLocalConfigInit_WithNoRouterFileOrHostIP_FailsSayingTheRouterAddressMustBeSet(t *testing.T) {
	stubHostIP(t, "")
	dataHome := t.TempDir()
	t.Setenv("EDUCATES_CLI_DATA_HOME", dataHome)

	err := (&ProjectInfo{}).runLocalConfigInit(&LocalConfigInitOptions{}, defaultLocalCluster(), io.Discard)

	wantError(t, err, "router.yaml", "must be set", "educates local router update --address <ip>")
	for _, name := range []string{"router.yaml", "config.yaml"} {
		if _, err := os.Stat(filepath.Join(dataHome, name)); !os.IsNotExist(err) {
			t.Errorf("init wrote %s: stat err = %v", name, err)
		}
	}
}

// secureLocalConfig is a default local cluster's config on a domain of
// its own, served over https, which a data home without a certificate
// source can't create.
const secureLocalConfig = minimalLocalConfigYAML +
	"cluster:\n  listenAddress: 10.10.10.1\n" +
	"ingress:\n  domain: educates.test\n"

// runCreateUntilTheCertificatePreflight runs local cluster create of the
// default local cluster of the data home dataHome, whose config is
// secureLocalConfig, and returns its error. The data home holds no
// certificate source, so create fails at its certificate preflight,
// which comes before it touches Docker; Docker is pointed at a socket
// that doesn't exist anyway, so that the run never reaches the host's.
func runCreateUntilTheCertificatePreflight(t *testing.T, dataHome string) error {
	t.Helper()
	t.Setenv("EDUCATES_CLI_DATA_HOME", dataHome)
	t.Setenv("DOCKER_HOST", "unix://"+filepath.Join(t.TempDir(), "no-docker.sock"))
	if err := os.WriteFile(config.LocalConfigPath(dataHome), []byte(secureLocalConfig), 0o644); err != nil {
		t.Fatal(err)
	}
	o := &LocalClusterCreateOptions{Kubeconfig: filepath.Join(t.TempDir(), "kubeconfig")}
	err := (&ProjectInfo{}).runLocalClusterCreate(context.Background(), io.Discard, o, localclusters.Default(dataHome))
	wantError(t, err, "educates.test")
	return err
}

// Create writes a missing router file before anything that can fail on
// the host: here the certificate preflight, the first of its checks.
func TestLocalClusterCreate_WritesAMissingRouterFileFirst(t *testing.T) {
	stubHostIP(t, "192.168.50.151")
	dataHome := t.TempDir()

	runCreateUntilTheCertificatePreflight(t, dataHome)

	wantRouterFile(t, dataHome, hostIPRouterFile)
}

func TestLocalClusterCreate_LeavesAnExistingRouterFile(t *testing.T) {
	stubHostIP(t, "192.168.50.151")
	dataHome := t.TempDir()
	writeRouterFile(t, dataHome, handWrittenRouterFile)

	runCreateUntilTheCertificatePreflight(t, dataHome)

	wantRouterFile(t, dataHome, handWrittenRouterFile)
}
