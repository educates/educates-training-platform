package cmd

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/educates/educates-training-platform/client-programs/pkg/config"
	"github.com/educates/educates-training-platform/client-programs/pkg/localclusters"
)

// kindClusterExists is a kind cluster existence check that reports
// exists.
func kindClusterExists(exists bool) func() (bool, error) {
	return func() (bool, error) { return exists, nil }
}

func TestLocalConfigDelete_NamedCluster_RemovesItsFolderWhenTheClusterDoesNotExist(t *testing.T) {
	dataHome := stageInitConfig(t)
	id, _ := localclusters.Named("test2", dataHome)
	writeNamedConfig(t, id, namedConfigForTest)
	if err := os.WriteFile(id.KindConfigPath, []byte("kind: Cluster\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	var out bytes.Buffer
	if err := runLocalConfigDelete(&out, id, kindClusterExists(false)); err != nil {
		t.Fatalf("delete: %v", err)
	}

	folder := filepath.Dir(id.ConfigPath)
	if _, err := os.Stat(folder); !os.IsNotExist(err) {
		t.Errorf("%s was not removed: stat err = %v", folder, err)
	}
	if !strings.Contains(out.String(), folder) {
		t.Errorf("output %q does not name %s", out.String(), folder)
	}
	if _, err := os.Stat(config.LocalConfigPath(dataHome)); err != nil {
		t.Errorf("the default cluster's config was touched: %v", err)
	}
}

func TestLocalConfigDelete_NamedCluster_RefusesWhileTheClusterExists(t *testing.T) {
	_, id := namedClusterForTest(t)
	writeNamedConfig(t, id, namedConfigForTest)

	err := runLocalConfigDelete(io.Discard, id, kindClusterExists(true))

	wantError(t, err, "educates-test2", "exists", "educates local cluster delete --local-cluster test2")
	if _, err := os.Stat(id.ConfigPath); err != nil {
		t.Errorf("the config was removed: %v", err)
	}
}

func TestLocalConfigDelete_NamedCluster_RefusesWhenTheClusterCannotBeChecked(t *testing.T) {
	_, id := namedClusterForTest(t)
	writeNamedConfig(t, id, namedConfigForTest)

	err := runLocalConfigDelete(io.Discard, id, func() (bool, error) {
		return false, errors.New("cannot reach Docker")
	})

	wantError(t, err, "cannot reach Docker")
	if _, err := os.Stat(id.ConfigPath); err != nil {
		t.Errorf("the config was removed: %v", err)
	}
}

func TestLocalConfigDelete_RefusesTheDefaultCluster(t *testing.T) {
	dataHome := stageInitConfig(t)

	err := runLocalConfigDelete(io.Discard, localclusters.Default(dataHome), kindClusterExists(false))

	wantError(t, err, "default local cluster", "--local-cluster", "EDUCATES_LOCAL_CLUSTER")
	if _, err := os.Stat(config.LocalConfigPath(dataHome)); err != nil {
		t.Errorf("the default cluster's config was removed: %v", err)
	}
}

func TestLocalConfigDelete_NamedClusterWithoutConfig_Errors(t *testing.T) {
	_, id := namedClusterForTest(t)

	err := runLocalConfigDelete(io.Discard, id, kindClusterExists(false))

	wantError(t, err, `local cluster "test2"`, filepath.Dir(id.ConfigPath))
}
