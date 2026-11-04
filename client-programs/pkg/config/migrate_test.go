package config

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/educates/educates-training-platform/client-programs/pkg/config/v1alpha1"
)

const v3KindValues = `
clusterInfrastructure:
  provider: kind
clusterIngress:
  domain: educates.test
localKindCluster:
  listenAddress: 192.168.1.10
  apiServer:
    address: 192.168.1.10
    port: 6443
  networking:
    serviceSubnet: 10.96.0.0/12
    podSubnet: 10.244.0.0/16
  volumeMounts:
    - hostPath: /tmp/data
      containerPath: /data
      readOnly: true
  registryMirrors:
    - mirror: docker.io
      url: https://proxy.local
localDNSResolver:
  targetAddress: 192.168.1.10
  extraDomains:
    - example.test
imageVersions:
  - name: "1.0"
    image: ghcr.io/educates/example:1.0
websiteStyling:
  defaultTheme: educates-default
  themeDataRefs:
    - namespace: educates
      name: my-theme-data
secretPropagation:
  imagePullSecretNames:
    - my-pull-secret
clusterSecurity:
  policyEngine: pod-security-standards
`

func TestMaybeMigrateV3_NoState_Noop(t *testing.T) {
	dataHome := t.TempDir()
	if err := MaybeMigrateV3(dataHome, io.Discard); err != nil {
		t.Fatalf("MaybeMigrateV3: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dataHome, "config.yaml")); err == nil {
		t.Error("config.yaml should not have been created from empty data home")
	}
}

func TestMaybeMigrateV3_ConfigYAMLPresent_Noop(t *testing.T) {
	dataHome := t.TempDir()
	must(t, os.WriteFile(filepath.Join(dataHome, "config.yaml"), []byte("existing"), 0o644))
	must(t, os.WriteFile(filepath.Join(dataHome, "values.yaml"), []byte(v3KindValues), 0o644))

	if err := MaybeMigrateV3(dataHome, io.Discard); err != nil {
		t.Fatalf("MaybeMigrateV3: %v", err)
	}

	body := readFile(t, filepath.Join(dataHome, "config.yaml"))
	if string(body) != "existing" {
		t.Errorf("config.yaml content overwritten: %q", body)
	}
	if _, err := os.Stat(filepath.Join(dataHome, "values.yaml.v3-backup")); err == nil {
		t.Error("v3 backup should not have been created when config.yaml already present")
	}
}

func TestMaybeMigrateV3_KindProvider_Migrates(t *testing.T) {
	dataHome := t.TempDir()
	must(t, os.WriteFile(filepath.Join(dataHome, "values.yaml"), []byte(v3KindValues), 0o644))

	if err := MaybeMigrateV3(dataHome, io.Discard); err != nil {
		t.Fatalf("MaybeMigrateV3: %v", err)
	}

	// values.yaml renamed.
	if _, err := os.Stat(filepath.Join(dataHome, "values.yaml")); err == nil {
		t.Error("values.yaml should have been renamed")
	}
	if _, err := os.Stat(filepath.Join(dataHome, "values.yaml.v3-backup")); err != nil {
		t.Errorf("values.yaml.v3-backup missing: %v", err)
	}

	// config.yaml should load cleanly via the v4 loader.
	cfgPath := filepath.Join(dataHome, "config.yaml")
	cfg, err := LoadLocal(cfgPath)
	if err != nil {
		t.Fatalf("LoadLocal on migrated file: %v", err)
	}

	// Spot-check a representative scattering of fields across the
	// translation table; full coverage is the schema's job at load.
	if got, want := cfg.Ingress.Domain, "educates.test"; got != want {
		t.Errorf("Ingress.Domain = %q, want %q", got, want)
	}
	if got, want := cfg.Cluster.ApiServer.Port, 6443; got != want {
		t.Errorf("Cluster.ApiServer.Port = %d, want %d", got, want)
	}
	if got := len(cfg.Cluster.VolumeMounts); got != 1 {
		t.Fatalf("VolumeMounts len = %d, want 1", got)
	}
	if cfg.Cluster.VolumeMounts[0].ReadOnly == nil || !*cfg.Cluster.VolumeMounts[0].ReadOnly {
		t.Errorf("VolumeMounts[0].ReadOnly = %v, want true", cfg.Cluster.VolumeMounts[0].ReadOnly)
	}
	if got, want := cfg.Resolver.TargetAddress, "192.168.1.10"; got != want {
		t.Errorf("Resolver.TargetAddress = %q, want %q", got, want)
	}
	if got := len(cfg.Resolver.ExtraDomains); got != 1 {
		t.Errorf("ExtraDomains len = %d, want 1", got)
	}
	if got, want := cfg.WebsiteStyling.DefaultTheme, "educates-default"; got != want {
		t.Errorf("DefaultTheme = %q, want %q", got, want)
	}
	if got, want := len(cfg.WebsiteStyling.ThemeDataRefs), 1; got != want {
		t.Errorf("ThemeDataRefs len = %d, want %d", got, want)
	}
}

func TestMaybeMigrateV3_EmptyProvider_Migrates(t *testing.T) {
	dataHome := t.TempDir()
	must(t, os.WriteFile(filepath.Join(dataHome, "values.yaml"),
		[]byte("clusterIngress:\n  domain: workshop.test\n"), 0o644))

	if err := MaybeMigrateV3(dataHome, io.Discard); err != nil {
		t.Fatalf("MaybeMigrateV3: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dataHome, "config.yaml")); err != nil {
		t.Errorf("config.yaml not written: %v", err)
	}
}

func TestMaybeMigrateV3_GKEProvider_Refuses(t *testing.T) {
	dataHome := t.TempDir()
	must(t, os.WriteFile(filepath.Join(dataHome, "values.yaml"),
		[]byte("clusterInfrastructure:\n  provider: gke\n"), 0o644))

	err := MaybeMigrateV3(dataHome, io.Discard)
	if err == nil {
		t.Fatal("expected refuse-with-clear-error for gke provider")
	}
	for _, want := range []string{"gke", "values.yaml", "EducatesGKEConfig"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q missing hint %q", err, want)
		}
	}
	// Original file left untouched.
	if _, err := os.Stat(filepath.Join(dataHome, "values.yaml")); err != nil {
		t.Errorf("values.yaml should have been left alone: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dataHome, "config.yaml")); err == nil {
		t.Error("config.yaml should not have been written")
	}
}

func TestEnsureLocalConfigFile_MigratesAndPasses(t *testing.T) {
	dataHome := t.TempDir()
	must(t, os.WriteFile(filepath.Join(dataHome, "values.yaml"), []byte(v3KindValues), 0o644))

	if err := EnsureLocalConfigFile(dataHome); err != nil {
		t.Fatalf("EnsureLocalConfigFile: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dataHome, "config.yaml")); err != nil {
		t.Errorf("config.yaml missing after Ensure: %v", err)
	}
}

func TestNoteIfV3CACachePresent_OpaqueCACertOnly_ExplainsItIsACATrustSecret(t *testing.T) {
	dataHome := t.TempDir()
	secretsDir := filepath.Join(dataHome, "secrets")
	must(t, os.MkdirAll(secretsDir, 0o755))
	v3CASecret := `apiVersion: v1
kind: Secret
metadata:
  name: workshop.test-ca
  annotations:
    training.educates.dev/domain: workshop.test
type: Opaque
data:
  ca.crt: dGVzdA==
`
	must(t, os.WriteFile(filepath.Join(secretsDir, "workshop.test-ca.yaml"), []byte(v3CASecret), 0o644))

	var buf bytes.Buffer
	noteIfV3CACachePresent(&buf, secretsDir, "workshop.test")

	s := buf.String()
	for _, want := range []string{
		filepath.Join(secretsDir, "workshop.test-ca.yaml"),
		"is a valid CA trust Secret",
		"*.workshop.test",
		"StaticCertificate provider",
		"no cert-manager",
		"educates local secrets add ca <name>\n",
		"educates local secrets add ca <name> --cert ROOT.pem --key ROOT-KEY.pem\n",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("note missing %q in:\n%s", want, s)
		}
	}
	for _, unwanted := range []string{"--domain", "WARNING", "kubernetes.io/tls"} {
		if strings.Contains(s, unwanted) {
			t.Errorf("note mentions %q, which no longer applies:\n%s", unwanted, s)
		}
	}
}

func TestNoteIfV3CACachePresent_TLSShape_NoNote(t *testing.T) {
	dataHome := t.TempDir()
	secretsDir := filepath.Join(dataHome, "secrets")
	must(t, os.MkdirAll(secretsDir, 0o755))
	// A kubernetes.io/tls Secret is not a v3 CA certificate.
	v4Secret := `apiVersion: v1
kind: Secret
metadata:
  name: workshop.test-ca
  annotations:
    training.educates.dev/domain: workshop.test
type: kubernetes.io/tls
data:
  tls.crt: dGVzdA==
  tls.key: dGVzdA==
`
	must(t, os.WriteFile(filepath.Join(secretsDir, "workshop.test-ca.yaml"), []byte(v4Secret), 0o644))

	var buf bytes.Buffer
	noteIfV3CACachePresent(&buf, secretsDir, "workshop.test")
	if buf.Len() != 0 {
		t.Errorf("a kubernetes.io/tls Secret should not be noted, got:\n%s", buf.String())
	}
}

func TestEnsureLocalConfigFile_NoMigrationNeeded_SurfacesMissingError(t *testing.T) {
	dataHome := t.TempDir()
	err := EnsureLocalConfigFile(dataHome)
	if err == nil {
		t.Fatal("expected MissingLocalConfigError for empty data home")
	}
	if !strings.Contains(err.Error(), "config init") {
		t.Errorf("expected local config init hint, got %q", err)
	}
}

func TestEnsureOrInitLocalConfigFile_EmptyDataHome_WritesDefault(t *testing.T) {
	dataHome := filepath.Join(t.TempDir(), "missing-subdir")
	created, err := EnsureOrInitLocalConfigFile(dataHome)
	if err != nil {
		t.Fatalf("EnsureOrInitLocalConfigFile: %v", err)
	}
	if !created {
		t.Error("expected created=true when writing a fresh default config")
	}
	body := readFile(t, filepath.Join(dataHome, "config.yaml"))
	if string(body) != DefaultLocalConfigYAML {
		t.Errorf("written config does not match DefaultLocalConfigYAML:\n%s", body)
	}
}

func TestEnsureOrInitLocalConfigFile_ConfigPresent_LeavesAlone(t *testing.T) {
	dataHome := t.TempDir()
	existing := []byte("# hand-edited config\napiVersion: cli.educates.dev/v1alpha1\nkind: EducatesLocalConfig\n")
	must(t, os.WriteFile(filepath.Join(dataHome, "config.yaml"), existing, 0o644))

	created, err := EnsureOrInitLocalConfigFile(dataHome)
	if err != nil {
		t.Fatalf("EnsureOrInitLocalConfigFile: %v", err)
	}
	if created {
		t.Error("expected created=false when config.yaml already exists")
	}
	if got := readFile(t, filepath.Join(dataHome, "config.yaml")); string(got) != string(existing) {
		t.Errorf("existing config was overwritten:\n%s", got)
	}
}

func TestEnsureOrInitLocalConfigFile_MigratesV3_NotCreated(t *testing.T) {
	dataHome := t.TempDir()
	must(t, os.WriteFile(filepath.Join(dataHome, "values.yaml"), []byte(v3KindValues), 0o644))

	created, err := EnsureOrInitLocalConfigFile(dataHome)
	if err != nil {
		t.Fatalf("EnsureOrInitLocalConfigFile: %v", err)
	}
	if created {
		t.Error("expected created=false when a v3 install was migrated")
	}
	// Migration should have produced a config carrying the v3 domain, not
	// the empty default template.
	body := readFile(t, filepath.Join(dataHome, "config.yaml"))
	if !strings.Contains(string(body), "educates.test") {
		t.Errorf("migrated config missing v3 domain:\n%s", body)
	}
}

func TestEnsureOrInitLocalConfigFile_NonKindV3_ErrorsNoDefault(t *testing.T) {
	dataHome := t.TempDir()
	must(t, os.WriteFile(filepath.Join(dataHome, "values.yaml"),
		[]byte("clusterInfrastructure:\n  provider: gke\n"), 0o644))

	created, err := EnsureOrInitLocalConfigFile(dataHome)
	if err == nil {
		t.Fatal("expected migration to refuse a non-kind v3 install")
	}
	if created {
		t.Error("expected created=false when migration refuses")
	}
	if _, statErr := os.Stat(filepath.Join(dataHome, "config.yaml")); statErr == nil {
		t.Error("a default config.yaml must not be written over an unmigratable v3 install")
	}
}

// migrateV3Values migrates a version 3 values.yaml holding values in a
// temporary data home and loads the result. It returns the loaded
// configuration, the written file and the migration's notice.
func migrateV3Values(t *testing.T, values string) (*v1alpha1.EducatesLocalConfig, string, string) {
	t.Helper()
	dataHome := t.TempDir()
	must(t, os.WriteFile(filepath.Join(dataHome, "values.yaml"), []byte(values), 0o644))

	var notice bytes.Buffer
	if err := MaybeMigrateV3(dataHome, &notice); err != nil {
		t.Fatalf("MaybeMigrateV3: %v", err)
	}
	cfgPath := filepath.Join(dataHome, "config.yaml")
	written := string(readFile(t, cfgPath))
	cfg, err := LoadLocal(cfgPath)
	if err != nil {
		t.Fatalf("LoadLocal on migrated file: %v\n%s", err, written)
	}
	return cfg, written, notice.String()
}

func TestMaybeMigrateV3_NoImagePuller_LeavesImagePrePullerUnset(t *testing.T) {
	cfg, written, notice := migrateV3Values(t, v3KindValues)

	if strings.Contains(written, "imagePrePuller") {
		t.Errorf("migrated file sets imagePrePuller, want it left to the defaults:\n%s", written)
	}
	if cfg.ImagePrePuller.Enabled == nil || !*cfg.ImagePrePuller.Enabled {
		t.Errorf("ImagePrePuller.Enabled = %v, want the default true", cfg.ImagePrePuller.Enabled)
	}
	if strings.Contains(notice, "imagePuller") {
		t.Errorf("notice mentions imagePuller for a file without one:\n%s", notice)
	}
}

func TestMaybeMigrateV3_ImagePullerDisabled_CarriesEnabled(t *testing.T) {
	cfg, _, _ := migrateV3Values(t, "imagePuller:\n  enabled: false\n")

	assertPrePuller(t, cfg, v1alpha1.ImagePrePullerConfig{Enabled: boolPtr(false)})
}

func TestMaybeMigrateV3_PrePullImagesWithBaseEnvironment_KeepsDefaultsAndAddsNames(t *testing.T) {
	cfg, written, _ := migrateV3Values(t, `imagePuller:
  prePullImages:
    - jdk21-environment
    - base-environment
    - docker-in-docker
`)

	assertPrePuller(t, cfg, v1alpha1.ImagePrePullerConfig{
		Enabled: boolPtr(true),
		Images: []v1alpha1.PrePulledImage{
			{Name: "jdk21-environment"},
			{Name: "docker-in-docker"},
		},
	})
	if strings.Contains(written, "includeDefaultImages") {
		t.Errorf("migrated file sets includeDefaultImages, want the default kept:\n%s", written)
	}
}

func TestMaybeMigrateV3_PrePullImagesUnset_KeepsDefaultsOnly(t *testing.T) {
	cfg, _, _ := migrateV3Values(t, "imagePuller:\n  enabled: true\n")

	assertPrePuller(t, cfg, v1alpha1.ImagePrePullerConfig{Enabled: boolPtr(true)})
}

func TestMaybeMigrateV3_PrePullImagesWithoutBaseEnvironment_ReproducesTheV3Set(t *testing.T) {
	for _, tc := range []struct {
		name   string
		values string
		want   v1alpha1.ImagePrePullerConfig
	}{
		{
			name: "names",
			values: `imagePuller:
  prePullImages:
    - jdk21-environment
    - docker-in-docker
`,
			want: v1alpha1.ImagePrePullerConfig{
				Enabled:              boolPtr(true),
				IncludeDefaultImages: boolPtr(false),
				Images: []v1alpha1.PrePulledImage{
					{Name: "training-portal"},
					{Name: "jdk21-environment"},
					{Name: "docker-in-docker"},
				},
			},
		},
		{
			name:   "empty list",
			values: "imagePuller:\n  prePullImages: []\n",
			want: v1alpha1.ImagePrePullerConfig{
				Enabled:              boolPtr(true),
				IncludeDefaultImages: boolPtr(false),
				Images:               []v1alpha1.PrePulledImage{{Name: "training-portal"}},
			},
		},
		{
			name:   "empty list while disabled",
			values: "imagePuller:\n  enabled: false\n  prePullImages: []\n",
			want: v1alpha1.ImagePrePullerConfig{
				Enabled:              boolPtr(false),
				IncludeDefaultImages: boolPtr(false),
				Images:               []v1alpha1.PrePulledImage{{Name: "training-portal"}},
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg, _, _ := migrateV3Values(t, tc.values)
			assertPrePuller(t, cfg, tc.want)
		})
	}
}

func TestMaybeMigrateV3_CustomPrePullName_KeepsItsImageVersionsEntry(t *testing.T) {
	cfg, _, _ := migrateV3Values(t, `imageVersions:
  - name: my-tools
    image: ghcr.io/example/my-tools:1.0
imagePuller:
  prePullImages:
    - base-environment
    - my-tools
`)

	assertPrePuller(t, cfg, v1alpha1.ImagePrePullerConfig{
		Enabled: boolPtr(true),
		Images:  []v1alpha1.PrePulledImage{{Name: "my-tools"}},
	})
	want := []v1alpha1.ImageVersion{{Name: "my-tools", Image: "ghcr.io/example/my-tools:1.0"}}
	if !reflect.DeepEqual(cfg.ImageVersions, want) {
		t.Errorf("ImageVersions = %v, want %v", cfg.ImageVersions, want)
	}
}

func TestMaybeMigrateV3_PauseContainerOverride_IsDroppedAndNoted(t *testing.T) {
	cfg, _, notice := migrateV3Values(t, `imageVersions:
  - name: pause-container
    image: registry.example.com/educates-pause-container:3.8.1
  - name: base-environment
    image: registry.example.com/educates-base-environment:3.8.1
`)

	want := []v1alpha1.ImageVersion{{Name: "base-environment", Image: "registry.example.com/educates-base-environment:3.8.1"}}
	if !reflect.DeepEqual(cfg.ImageVersions, want) {
		t.Errorf("ImageVersions = %v, want %v", cfg.ImageVersions, want)
	}
	wantLine := "dropped imageVersions entry pause-container: the image pre-puller now runs its own image-puller helper, which a pause container image cannot replace\n"
	if !strings.Contains(notice, wantLine) {
		t.Errorf("notice missing %q in:\n%s", wantLine, notice)
	}
}

func TestMaybeMigrateV3_PrePullNamesMissingFromV4Inventory_AreDroppedAndNoted(t *testing.T) {
	for _, tc := range []struct {
		name     string
		values   string
		want     v1alpha1.ImagePrePullerConfig
		wantLine string
	}{
		{
			name: "default images kept",
			values: `imagePuller:
  prePullImages:
    - base-environment
    - loftsh-kubernetes-v1.31
    - jdk21-environment
    - session-manager
`,
			want: v1alpha1.ImagePrePullerConfig{
				Enabled: boolPtr(true),
				Images:  []v1alpha1.PrePulledImage{{Name: "jdk21-environment"}},
			},
			wantLine: "dropped imagePuller.prePullImages names the v4 image inventory does not hold: loftsh-kubernetes-v1.31, session-manager; to pull one, add its full reference to imagePrePuller.images as an image entry\n",
		},
		{
			name: "every name dropped with the default images",
			values: `imagePuller:
  prePullImages:
    - secrets-manager
    - pause-container
`,
			want: v1alpha1.ImagePrePullerConfig{
				Enabled:              boolPtr(true),
				IncludeDefaultImages: boolPtr(false),
				Images:               []v1alpha1.PrePulledImage{{Name: "training-portal"}},
			},
			wantLine: "dropped imagePuller.prePullImages names the v4 image inventory does not hold: secrets-manager, pause-container; to pull one, add its full reference to imagePrePuller.images as an image entry\n",
		},
		{
			// An override adds its name to the inventory unless it sets
			// an image of its own: the CLI passes lookup-service to its own
			// custom resource, the operator routes node-ca-injector to a
			// chart value of its own, and the pause-container override is
			// dropped.
			name: "names defined in imageVersions",
			values: `imageVersions:
  - name: loftsh-kubernetes-v1.32
    image: ghcr.io/loft-sh/kubernetes:v1.32.1
  - name: lookup-service
    image: registry.example.com/educates-lookup-service:3.8.1
  - name: node-ca-injector
    image: registry.example.com/educates-node-ca-injector:3.8.1
  - name: pause-container
    image: registry.example.com/educates-pause-container:3.8.1
imagePuller:
  prePullImages:
    - base-environment
    - loftsh-kubernetes-v1.32
    - lookup-service
    - node-ca-injector
    - pause-container
`,
			want: v1alpha1.ImagePrePullerConfig{
				Enabled: boolPtr(true),
				Images:  []v1alpha1.PrePulledImage{{Name: "loftsh-kubernetes-v1.32"}},
			},
			wantLine: "dropped imagePuller.prePullImages names the v4 image inventory does not hold: lookup-service, node-ca-injector, pause-container; to pull one, add its full reference to imagePrePuller.images as an image entry\n",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg, _, notice := migrateV3Values(t, tc.values)
			assertPrePuller(t, cfg, tc.want)
			if !strings.Contains(notice, tc.wantLine) {
				t.Errorf("notice missing %q in:\n%s", tc.wantLine, notice)
			}
		})
	}
}

func TestMaybeMigrateV3_ImagePuller_NoticeSaysWhatWasCarriedOver(t *testing.T) {
	for _, tc := range []struct {
		name     string
		values   string
		wantLine string
	}{
		{
			name:     "switch",
			values:   "imagePuller:\n  enabled: false\n",
			wantLine: "carried imagePuller over to imagePrePuller: enabled: false\n",
		},
		{
			name: "default images kept",
			values: `imagePuller:
  enabled: true
  prePullImages:
    - base-environment
    - jdk21-environment
    - docker-in-docker
`,
			wantLine: "carried imagePuller over to imagePrePuller: enabled: true, images: jdk21-environment, docker-in-docker\n",
		},
		{
			name: "default images dropped",
			values: `imagePuller:
  prePullImages:
    - jdk21-environment
`,
			wantLine: "carried imagePuller over to imagePrePuller: includeDefaultImages: false, images: training-portal, jdk21-environment\n",
		},
		{
			name: "same as the defaults",
			values: `imagePuller:
  prePullImages:
    - base-environment
`,
			wantLine: "carried imagePuller over: it matches the imagePrePuller defaults, so imagePrePuller is left unset\n",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, _, notice := migrateV3Values(t, tc.values)
			if !strings.Contains(notice, tc.wantLine) {
				t.Errorf("notice missing %q in:\n%s", tc.wantLine, notice)
			}
		})
	}
}

// assertPrePuller compares the loaded pre-puller block with want.
func assertPrePuller(t *testing.T, cfg *v1alpha1.EducatesLocalConfig, want v1alpha1.ImagePrePullerConfig) {
	t.Helper()
	if !reflect.DeepEqual(cfg.ImagePrePuller, want) {
		t.Errorf("ImagePrePuller = %s, want %s", describePrePuller(cfg.ImagePrePuller), describePrePuller(want))
	}
}

func boolPtr(b bool) *bool { return &b }

// describePrePuller renders a pre-puller block for failure messages.
func describePrePuller(c v1alpha1.ImagePrePullerConfig) string {
	flag := func(p *bool) string {
		if p == nil {
			return "unset"
		}
		if *p {
			return "true"
		}
		return "false"
	}
	entries := make([]string, 0, len(c.Images))
	for _, e := range c.Images {
		if e.Name != "" {
			entries = append(entries, "name "+e.Name)
		} else {
			entries = append(entries, "image "+e.Image)
		}
	}
	return "{enabled: " + flag(c.Enabled) +
		", includeDefaultImages: " + flag(c.IncludeDefaultImages) +
		", images: [" + strings.Join(entries, ", ") + "]}"
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func readFile(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
