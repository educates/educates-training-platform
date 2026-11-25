package cmd

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/educates/educates-training-platform/client-programs/pkg/config"
	"github.com/educates/educates-training-platform/client-programs/pkg/localclusters"
)

// wantListenAddressIgnored fails t unless out warns that --listen-address
// address is ignored, naming the router file of the data home dataHome.
func wantListenAddressIgnored(t *testing.T, out, address, dataHome string) {
	t.Helper()
	for _, part := range []string{"Warning: --listen-address " + address + " is ignored",
		config.LocalRouterPath(dataHome), "educates local router update --address <ip>"} {
		if !strings.Contains(out, part) {
			t.Errorf("output does not say %q:\n%s", part, out)
		}
	}
}

// --listen-address is taken and ignored with a warning: create carries
// on as without it, here up to its certificate preflight.
func TestLocalClusterCreate_IgnoresTheListenAddressFlagWithAWarning(t *testing.T) {
	stubHostIP(t, "192.168.50.151")
	dataHome := t.TempDir()
	t.Setenv("EDUCATES_CLI_DATA_HOME", dataHome)
	t.Setenv("DOCKER_HOST", "unix://"+filepath.Join(t.TempDir(), "no-docker.sock"))
	if err := os.WriteFile(config.LocalConfigPath(dataHome), []byte(secureLocalConfig), 0o644); err != nil {
		t.Fatal(err)
	}
	o := &LocalClusterCreateOptions{Kubeconfig: filepath.Join(t.TempDir(), "kubeconfig"), ListenAddress: "10.10.10.2"}

	var out bytes.Buffer
	err := (&ProjectInfo{}).runLocalClusterCreate(context.Background(), &out, o, localclusters.Default(dataHome))

	wantError(t, err, "educates.test")
	wantListenAddressIgnored(t, out.String(), "10.10.10.2", dataHome)
}

func TestLocalConfigInit_IgnoresTheListenAddressFlagWithAWarning(t *testing.T) {
	dataHome, id := namedClusterForTest(t)

	var out bytes.Buffer
	if err := (&ProjectInfo{}).runLocalConfigInit(&LocalConfigInitOptions{ListenAddress: "10.10.10.2"}, id, &out); err != nil {
		t.Fatalf("init: %v", err)
	}

	wantListenAddressIgnored(t, out.String(), "10.10.10.2", dataHome)
	if body, _ := os.ReadFile(id.ConfigPath); string(body) != minimalLocalConfigYAML {
		t.Errorf("init wrote:\n%s\nwant:\n%s", body, minimalLocalConfigYAML)
	}
}
