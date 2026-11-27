package config

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/educates/educates-training-platform/client-programs/pkg/config/v1alpha1"
)

// configWithSecretPropagation is a default config that names its pull
// secrets in secretPropagation, as a user wrote it before
// imageRegistry.pullSecrets replaced it.
const configWithSecretPropagation = localConfigHeader +
	"imageRegistry:\n" +
	"  pullSecrets:\n" +
	"    - shared\n" +
	"secretPropagation:\n" +
	"  imagePullSecretNames:\n" +
	"    - mirror\n" +
	"    - shared\n"

// configWithMovedPullSecrets is configWithSecretPropagation once moved.
const configWithMovedPullSecrets = localConfigHeader +
	"imageRegistry:\n" +
	"  pullSecrets:\n" +
	"    - shared\n" +
	"    - mirror\n"

func TestMoveSecretPropagationOfTheDefaultConfig(t *testing.T) {
	dataHome := t.TempDir()
	path := writeDataHomeFile(t, dataHome, "config.yaml", configWithSecretPropagation)
	var note bytes.Buffer

	if err := MoveSecretPropagation(dataHome, path, &note); err != nil {
		t.Fatalf("MoveSecretPropagation: %v", err)
	}

	if got := string(readFile(t, path)); got != configWithMovedPullSecrets {
		t.Errorf("config.yaml =\n%s\nwant\n%s", got, configWithMovedPullSecrets)
	}
	backup := path + ".bak"
	if got := string(readFile(t, backup)); got != configWithSecretPropagation {
		t.Errorf("config.yaml.bak =\n%s\nwant the previous config", got)
	}
	for _, want := range []string{"secretPropagation.imagePullSecretNames", "imageRegistry.pullSecrets", path, backup} {
		if !strings.Contains(note.String(), want) {
			t.Errorf("note does not mention %q:\n%s", want, note.String())
		}
	}
	if strings.Count(note.String(), "\n") != 1 {
		t.Errorf("note is not one line:\n%s", note.String())
	}
}

// Loading a config of the data home moves its secretPropagation first, so
// it loads with the names in imageRegistry.pullSecrets.
func TestLoadMovesTheSecretPropagationOfAConfigInTheDataHome(t *testing.T) {
	dataHome := t.TempDir()
	t.Setenv("EDUCATES_CLI_DATA_HOME", dataHome)
	path := writeDataHomeFile(t, dataHome, filepath.Join("clusters", "test2", "config.yaml"), configWithSecretPropagation)

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	local := cfg.(*v1alpha1.EducatesLocalConfig)
	if want := []string{"shared", "mirror"}; !reflect.DeepEqual(local.ImageRegistry.PullSecrets, want) {
		t.Errorf("imageRegistry.pullSecrets = %v, want %v", local.ImageRegistry.PullSecrets, want)
	}
}

func TestMoveSecretPropagationLeavesAnyOtherConfig(t *testing.T) {
	dataHome := t.TempDir()
	path := filepath.Join(t.TempDir(), "config.yaml")
	must(t, os.WriteFile(path, []byte(configWithSecretPropagation), 0o644))

	if err := MoveSecretPropagation(dataHome, path, &bytes.Buffer{}); err != nil {
		t.Fatalf("MoveSecretPropagation: %v", err)
	}
	if got := string(readFile(t, path)); got != configWithSecretPropagation {
		t.Errorf("a config outside the data home was changed:\n%s", got)
	}
}

// A config elsewhere that still has secretPropagation fails to load,
// naming what replaced it, in every kind that had it.
func TestLoadRefusesSecretPropagationNamingItsReplacement(t *testing.T) {
	for _, fixture := range []string{"local-empty.yaml", "gke-minimal.yaml", "eks-minimal.yaml", "inline-minimal.yaml"} {
		t.Run(fixture, func(t *testing.T) {
			base := readFile(t, filepath.Join("testdata", fixture))
			_, err := LoadBytes(append(base, "\nsecretPropagation:\n  imagePullSecretNames: [mirror]\n"...), fixture)
			if err == nil {
				t.Fatal("LoadBytes accepted secretPropagation")
			}
			for _, want := range []string{"secretPropagation", "imageRegistry.pullSecrets", "mirror"} {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("error %q does not mention %q", err, want)
				}
			}
		})
	}
}
