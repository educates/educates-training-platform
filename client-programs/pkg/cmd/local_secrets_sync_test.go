package cmd

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/educates/educates-training-platform/client-programs/pkg/localclusters"
)

func TestSecretsSync_BuildsThePlanFromTheSelectedLocalClustersConfig(t *testing.T) {
	const defaultConfig = "apiVersion: cli.educates.dev/v1alpha1\nkind: EducatesLocalConfig\ningress:\n  domain: workshop.test\n"
	cases := []struct {
		name          string
		defaultConfig string
		cluster       string
		namedConfig   string
		want          []string
	}{
		{
			name:          "the default cluster's secure domain",
			defaultConfig: defaultConfig,
			want:          []string{"test-ca", "workshop.test-ca-key-pair"},
		},
		{
			name: "the default cluster without a config",
			want: []string{"test-ca"},
		},
		{
			name:          "a named cluster's nip.io domain served over plain HTTP",
			defaultConfig: defaultConfig,
			cluster:       "test2",
			namedConfig:   "apiVersion: cli.educates.dev/v1alpha1\nkind: EducatesLocalConfig\ncluster:\n  listenAddress: 10.10.10.2\n",
			want:          []string{"test-ca"},
		},
		{
			name:          "a named cluster's secure domain",
			defaultConfig: defaultConfig,
			cluster:       "test2",
			namedConfig: "apiVersion: cli.educates.dev/v1alpha1\nkind: EducatesLocalConfig\n" +
				"cluster:\n  listenAddress: 10.10.10.2\ningress:\n  domain: test2.educates.test\n",
			want: []string{"test-ca", "test2.educates.test-ca-key-pair"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dataHome := withRootCA(t)
			if tc.defaultConfig != "" {
				if err := os.WriteFile(filepath.Join(dataHome, "config.yaml"), []byte(tc.defaultConfig), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			id := localclusters.Default(dataHome)
			if tc.cluster != "" {
				var err error
				if id, err = localclusters.Named(tc.cluster, dataHome); err != nil {
					t.Fatal(err)
				}
				writeNamedConfig(t, id, tc.namedConfig)
			}

			plan, err := resolveLocalSecretsSync(id)
			if err != nil {
				t.Fatalf("resolveLocalSecretsSync: %v", err)
			}
			if !slices.Equal(plan.SyncSecretNames, tc.want) {
				t.Errorf("sync pushes %v, want %v", plan.SyncSecretNames, tc.want)
			}
		})
	}
}

func TestSecretsSync_NamedClusterWithoutConfig_SaysHowToWriteOne(t *testing.T) {
	_, id := namedClusterForTest(t)

	_, err := resolveLocalSecretsSync(id)

	wantError(t, err, `"test2"`, "educates local config init --local-cluster test2 --listen-address <ip>")
}

func TestSecretsSync_InvalidDefaultConfig_NamesIt(t *testing.T) {
	dataHome := t.TempDir()
	t.Setenv("EDUCATES_CLI_DATA_HOME", dataHome)
	if err := os.WriteFile(filepath.Join(dataHome, "config.yaml"), []byte("kind: [not yaml"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := resolveLocalSecretsSync(localclusters.Default(dataHome))

	wantError(t, err, "config.yaml")
}
