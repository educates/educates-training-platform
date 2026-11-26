package config

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/educates/educates-training-platform/client-programs/internal/testpki"
	"github.com/educates/educates-training-platform/client-programs/pkg/config/hostinfo"
	"github.com/educates/educates-training-platform/client-programs/pkg/config/v1alpha1"
	"github.com/educates/educates-training-platform/client-programs/pkg/secrets"
	"github.com/educates/educates-training-platform/client-programs/pkg/utils"
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

func TestMaybeMigrateV3_WritesTheSchemaModeline(t *testing.T) {
	_, written, _ := migrateV3Values(t, v3KindValues)

	const modeline = "# yaml-language-server: $schema=https://schemas.educates.dev/cli/v1alpha1/EducatesLocalConfig.json\n"
	if !strings.HasPrefix(written, modeline+"apiVersion: cli.educates.dev/v1alpha1\nkind: EducatesLocalConfig\n") {
		t.Errorf("migrated file does not start with the schema modeline and then the config:\n%s", written)
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

// migrateV3Values migrates a version 3 values.yaml holding values in a
// temporary data home and loads the result. It returns the loaded
// configuration, the written file and the migration's notice.
func migrateV3Values(t *testing.T, values string) (*v1alpha1.EducatesLocalConfig, string, string) {
	t.Helper()
	return migrateV3ValuesIn(t, t.TempDir(), values)
}

// migrateV3ValuesIn is migrateV3Values in the data home dataHome, which
// may already hold a local secrets cache.
func migrateV3ValuesIn(t *testing.T, dataHome, values string) (*v1alpha1.EducatesLocalConfig, string, string) {
	t.Helper()
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

func TestMaybeMigrateV3_ResolverTargetAddress_IsDroppedAndNoted(t *testing.T) {
	cfg, written, notice := migrateV3Values(t, `localKindCluster:
  listenAddress: 127.0.0.1
localDNSResolver:
  targetAddress: 192.168.1.10
  extraDomains:
    - example.test
`)

	if strings.Contains(written, "targetAddress") {
		t.Errorf("migrated file sets targetAddress:\n%s", written)
	}
	if got, want := cfg.Cluster.ListenAddress, "127.0.0.1"; got != want {
		t.Errorf("Cluster.ListenAddress = %q, want the v3 listen address %q", got, want)
	}
	if !reflect.DeepEqual(cfg.Resolver.ExtraDomains, []string{"example.test"}) {
		t.Errorf("Resolver.ExtraDomains = %v, want the v3 extra domains", cfg.Resolver.ExtraDomains)
	}
	wantLine := "dropped localDNSResolver.targetAddress 192.168.1.10: a local cluster's names now resolve to its " +
		"cluster.listenAddress; set cluster.listenAddress to 192.168.1.10 to keep resolving the cluster there, " +
		"or for other names " +
		"add address=/<name>/192.168.1.10 to a *.conf file in the dnsmasq.d folder of the data home\n"
	if !strings.Contains(notice, wantLine) {
		t.Errorf("notice missing %q in:\n%s", wantLine, notice)
	}
}

const v3RegistryMirrorsValues = `localKindCluster:
  listenAddress: 10.10.10.1
  registryMirrors:
    - mirror: docker.io
      url: https://registry-1.docker.io
      username: alice
      password: secret
    - mirror: ghcr.io
      port: "5002"
      bindIP: 127.0.0.1
`

func TestMaybeMigrateV3_RegistryMirrors_AreWrittenToMirrorsYAML(t *testing.T) {
	dataHome := t.TempDir()
	must(t, os.WriteFile(filepath.Join(dataHome, "values.yaml"), []byte(v3RegistryMirrorsValues), 0o644))
	var notice bytes.Buffer

	if err := MaybeMigrateV3(dataHome, &notice); err != nil {
		t.Fatalf("MaybeMigrateV3: %v", err)
	}

	mirrorsPath := filepath.Join(dataHome, "mirrors.yaml")
	got, err := LoadLocalMirrors(dataHome)
	if err != nil {
		t.Fatalf("LoadLocalMirrors on the migrated mirrors.yaml: %v\n%s", err, readFile(t, mirrorsPath))
	}
	want := []v1alpha1.RegistryMirror{
		{Mirror: "docker.io", URL: "https://registry-1.docker.io", Username: "alice", Password: "secret"},
		{Mirror: "ghcr.io"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("migrated registry mirrors = %+v, want %+v", got, want)
	}
	// A v3 mirror's port and bindIP never had any effect, so they go.
	if wantLine := "port and bindIP of registry mirror ghcr.io were dropped: they never had any effect\n"; !strings.Contains(notice.String(), wantLine) {
		t.Errorf("notice does not say %q:\n%s", wantLine, notice.String())
	}
	if strings.Contains(notice.String(), "of registry mirror docker.io") {
		t.Errorf("notice says something was dropped of docker.io, which has no port or bindIP:\n%s", notice.String())
	}
	if body := string(readFile(t, mirrorsPath)); !strings.HasPrefix(body, LocalMirrorsModeline+"\n") {
		t.Errorf("mirrors.yaml does not start with the schema modeline:\n%s", body)
	}
	if written := string(readFile(t, filepath.Join(dataHome, "config.yaml"))); strings.Contains(written, "registryMirrors") {
		t.Errorf("the migrated config declares registry mirrors:\n%s", written)
	}
	for _, want := range []string{"moved localKindCluster.registryMirrors to " + mirrorsPath, "every local cluster",
		"docker.io", "ghcr.io"} {
		if !strings.Contains(notice.String(), want) {
			t.Errorf("notice does not mention %q:\n%s", want, notice.String())
		}
	}
	if strings.Contains(notice.String(), "secret") {
		t.Errorf("notice gives a mirror's password:\n%s", notice.String())
	}
}

func TestMaybeMigrateV3_RegistryMirrors_WithAMirrorsYAML_LeaveItAndSayWhatToAdd(t *testing.T) {
	dataHome := t.TempDir()
	must(t, os.WriteFile(filepath.Join(dataHome, "values.yaml"), []byte(v3RegistryMirrorsValues), 0o644))
	mirrorsPath := filepath.Join(dataHome, "mirrors.yaml")
	const existing = "apiVersion: cli.educates.dev/v1alpha1\nkind: EducatesLocalMirrors\nregistryMirrors:\n- mirror: quay.io\n"
	must(t, os.WriteFile(mirrorsPath, []byte(existing), 0o644))
	var notice bytes.Buffer

	if err := MaybeMigrateV3(dataHome, &notice); err != nil {
		t.Fatalf("MaybeMigrateV3: %v", err)
	}

	if body := string(readFile(t, mirrorsPath)); body != existing {
		t.Errorf("mirrors.yaml was overwritten:\n%s", body)
	}
	if written := string(readFile(t, filepath.Join(dataHome, "config.yaml"))); strings.Contains(written, "registryMirrors") {
		t.Errorf("the migrated config declares registry mirrors:\n%s", written)
	}
	for _, want := range []string{mirrorsPath + " already exists", "docker.io, ghcr.io", "by hand",
		"localKindCluster.registryMirrors", filepath.Join(dataHome, "values.yaml.v3-backup")} {
		if !strings.Contains(notice.String(), want) {
			t.Errorf("notice does not mention %q:\n%s", want, notice.String())
		}
	}
	if strings.Contains(notice.String(), "secret") {
		t.Errorf("notice gives a mirror's password:\n%s", notice.String())
	}
}

func TestMaybeMigrateV3_NoRegistryMirrors_WritesNoMirrorsYAML(t *testing.T) {
	dataHome := t.TempDir()
	must(t, os.WriteFile(filepath.Join(dataHome, "values.yaml"), []byte("localKindCluster:\n  listenAddress: 10.10.10.1\n"), 0o644))
	var notice bytes.Buffer

	if err := MaybeMigrateV3(dataHome, &notice); err != nil {
		t.Fatalf("MaybeMigrateV3: %v", err)
	}

	if _, err := os.Stat(filepath.Join(dataHome, "mirrors.yaml")); err == nil {
		t.Error("mirrors.yaml was written for a v3 file without registry mirrors")
	}
	if strings.Contains(notice.String(), "registryMirrors") {
		t.Errorf("notice mentions registry mirrors for a file without any:\n%s", notice.String())
	}
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

func TestMaybeMigrateV3_ListenAddressOnEveryHostAddress_BecomesTheHostIP(t *testing.T) {
	for _, tc := range []struct {
		name   string
		values string
		was    string
	}{
		{name: "missing", values: "clusterIngress:\n  domain: educates.test\n", was: "was not set"},
		{name: "0.0.0.0", values: "localKindCluster:\n  listenAddress: 0.0.0.0\n", was: "was 0.0.0.0"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			stubHostIP(t, "192.168.50.151")

			cfg, _, notice := migrateV3Values(t, tc.values)

			if got, want := cfg.Cluster.ListenAddress, "192.168.50.151"; got != want {
				t.Errorf("Cluster.ListenAddress = %q, want the host's IP address %q", got, want)
			}
			for _, want := range []string{"set cluster.listenAddress to 192.168.50.151, this host's IP address",
				"localKindCluster.listenAddress " + tc.was, "every host address", "specific IP address",
				"An alias such as 10.10.10.1", "survives network changes"} {
				if !strings.Contains(notice, want) {
					t.Errorf("notice does not mention %q:\n%s", want, notice)
				}
			}
		})
	}
}

func TestMaybeMigrateV3_ListenAddress_IsCommentedOnlyWhenItIsTheHostIP(t *testing.T) {
	const comment = "cluster:\n" +
		"  # This host's IP address when this file was written. An alias such as\n" +
		"  # 10.10.10.1, added to a network interface, survives network changes.\n" +
		"  listenAddress: 192.168.50.151\n"
	stubHostIP(t, "192.168.50.151")

	_, written, _ := migrateV3Values(t, "localKindCluster:\n  listenAddress: 0.0.0.0\n")
	if !strings.Contains(written, comment) {
		t.Errorf("migrated file does not comment the host's IP address as its listen address:\n%s", written)
	}

	_, written, _ = migrateV3Values(t, "localKindCluster:\n  listenAddress: 10.10.10.1\n")
	if strings.Contains(written, "This host's IP address") {
		t.Errorf("migrated file comments a listen address kept from v3 as the host's IP address:\n%s", written)
	}
}

func TestMaybeMigrateV3_ListenAddressOnEveryHostAddress_WithNoHostIP_SaysToSetIt(t *testing.T) {
	stubHostIP(t, "")

	cfg, _, notice := migrateV3Values(t, "clusterIngress:\n  domain: educates.test\n")

	if cfg.Cluster.ListenAddress != "" {
		t.Errorf("Cluster.ListenAddress = %q, want none with no host IP address detected", cfg.Cluster.ListenAddress)
	}
	for _, want := range []string{"left cluster.listenAddress unset", "no IP address of this host was detected",
		"educates local config set cluster.listenAddress <ip>"} {
		if !strings.Contains(notice, want) {
			t.Errorf("notice does not mention %q:\n%s", want, notice)
		}
	}
}

func TestMaybeMigrateV3_SpecificListenAddress_IsKeptWithoutANote(t *testing.T) {
	stubHostIP(t, "192.168.50.151")

	cfg, _, notice := migrateV3Values(t, "localKindCluster:\n  listenAddress: 10.10.10.1\n")

	if got, want := cfg.Cluster.ListenAddress, "10.10.10.1"; got != want {
		t.Errorf("Cluster.ListenAddress = %q, want the v3 listen address %q", got, want)
	}
	if strings.Contains(notice, "listenAddress") {
		t.Errorf("notice mentions the listen address it kept:\n%s", notice)
	}
}

func TestMaybeMigrateV3_NipDomainWithNoCachedCertificate_IsDroppedForTheV4Default(t *testing.T) {
	for _, domain := range []string{"10.10.10.1.nip.io", "10-10-10-1.nip.io"} {
		t.Run(domain, func(t *testing.T) {
			cfg, written, notice := migrateV3Values(t,
				"clusterIngress:\n  domain: "+domain+"\nlocalKindCluster:\n  listenAddress: 10.10.10.2\n")

			if cfg.Ingress.Domain != "" || strings.Contains(written, "domain:") {
				t.Errorf("migrated file keeps ingress.domain %s, want it left to the nip.io default:\n%s", domain, written)
			}
			for _, want := range []string{"dropped clusterIngress.domain " + domain,
				"no wildcard TLS certificate for it is cached", "10-10-10-2.nip.io", "plain HTTP",
				"to keep " + domain + ", set ingress.domain to it and ingress.insecure to true"} {
				if !strings.Contains(notice, want) {
					t.Errorf("notice does not mention %q:\n%s", want, notice)
				}
			}
		})
	}
}

func TestMaybeMigrateV3_NipDomainWithACachedCertificate_IsKept(t *testing.T) {
	dataHome := t.TempDir()
	t.Setenv(utils.EducatesCLIDataHomeEnv, dataHome)
	leaf := testpki.NewRootCA().NewLeaf(testpki.WithDNSNames("*.10.10.10.1.nip.io"))
	must(t, secrets.WriteCachedSecret(secrets.NewTLSSecret("nip-tls", leaf.ChainPEM(), leaf.KeyPEM)))

	cfg, _, notice := migrateV3ValuesIn(t, dataHome, "clusterIngress:\n  domain: 10.10.10.1.nip.io\n")

	if got, want := cfg.Ingress.Domain, "10.10.10.1.nip.io"; got != want {
		t.Errorf("Ingress.Domain = %q, want %q, which the cached wildcard certificate serves", got, want)
	}
	if strings.Contains(notice, "clusterIngress.domain") {
		t.Errorf("notice mentions the domain it kept:\n%s", notice)
	}
}

func TestMaybeMigrateV3_NipDomainWithAnExpiredCachedCertificate_IsDropped(t *testing.T) {
	dataHome := t.TempDir()
	t.Setenv(utils.EducatesCLIDataHomeEnv, dataHome)
	leaf := testpki.NewRootCA().NewLeaf(testpki.WithDNSNames("*.10.10.10.1.nip.io"),
		testpki.WithValidity(time.Now().Add(-48*time.Hour), time.Now().Add(-24*time.Hour)))
	must(t, secrets.WriteCachedSecret(secrets.NewTLSSecret("nip-tls", leaf.ChainPEM(), leaf.KeyPEM)))

	cfg, _, notice := migrateV3ValuesIn(t, dataHome, "clusterIngress:\n  domain: 10.10.10.1.nip.io\n")

	if cfg.Ingress.Domain != "" {
		t.Errorf("Ingress.Domain = %q, want it left to the nip.io default, since no deploy serves an expired certificate", cfg.Ingress.Domain)
	}
	if !strings.Contains(notice, "dropped clusterIngress.domain 10.10.10.1.nip.io") {
		t.Errorf("notice does not say the domain was dropped:\n%s", notice)
	}
}

func TestMaybeMigrateV3_NipDomainVersion3ServedOverHTTPS_IsKept(t *testing.T) {
	const domain = "clusterIngress:\n  domain: 10.10.10.1.nip.io\n"
	for name, values := range map[string]string{
		"tlsCertificate":    domain + "  tlsCertificate:\n    tls.crt: CERT\n    tls.key: KEY\n",
		"tlsCertificateRef": domain + "  tlsCertificateRef:\n    namespace: default\n    name: wildcard\n",
		"protocol https":    domain + "  protocol: https\n",
		"clusterInfrastructure.caCertificateRef": domain +
			"clusterInfrastructure:\n  caCertificateRef:\n    namespace: default\n    name: root-ca\n",
	} {
		t.Run(name, func(t *testing.T) {
			cfg, _, notice := migrateV3Values(t, values)

			if got, want := cfg.Ingress.Domain, "10.10.10.1.nip.io"; got != want {
				t.Errorf("Ingress.Domain = %q, want %q, which version 3 served over HTTPS", got, want)
			}
			if strings.Contains(notice, "dropped clusterIngress.domain") {
				t.Errorf("notice says the domain was dropped:\n%s", notice)
			}
		})
	}
}

func TestMaybeMigrateV3_DroppedNipDomain_GetsNoCACacheNote(t *testing.T) {
	dataHome := t.TempDir()
	must(t, os.MkdirAll(filepath.Join(dataHome, "secrets"), 0o755))
	must(t, os.WriteFile(filepath.Join(dataHome, "secrets", "nip-ca.yaml"), []byte(`apiVersion: v1
kind: Secret
metadata:
  name: nip-ca
  annotations:
    training.educates.dev/domain: 10.10.10.1.nip.io
type: Opaque
data:
  ca.crt: dGVzdA==
`), 0o644))

	_, _, notice := migrateV3ValuesIn(t, dataHome, "clusterIngress:\n  domain: 10.10.10.1.nip.io\n")

	if strings.Contains(notice, "the cached v3 CA certificate") {
		t.Errorf("notice explains a CA certificate for the domain it dropped:\n%s", notice)
	}
}

func TestMaybeMigrateV3_NipDomainOverPlainHTTP_IsKept(t *testing.T) {
	cfg, _, notice := migrateV3Values(t, "clusterIngress:\n  domain: 10.10.10.1.nip.io\n  protocol: http\n")

	if got, want := cfg.Ingress.Domain, "10.10.10.1.nip.io"; got != want || !cfg.Ingress.Insecure {
		t.Errorf("Ingress = %+v, want domain %q served over plain HTTP, as version 3 served it", cfg.Ingress, want)
	}
	if strings.Contains(notice, "clusterIngress.domain") {
		t.Errorf("notice mentions the domain it kept:\n%s", notice)
	}
}

func TestMaybeMigrateV3_DroppedTopLevelSettings_AreEachNotedWithTheirV4Home(t *testing.T) {
	for _, tc := range []struct {
		values string
		want   []string
	}{
		{"debug: true\n", []string{"dropped debug: it has no version 4 equivalent", "educates admin platform render --local-config"}},
		{"clusterPackages:\n  kyverno:\n    enabled: false\n", []string{"dropped clusterPackages: a local cluster always installs Contour and Kyverno", "in an EducatesConfig"}},
		{"imageRegistry:\n  host: registry.internal\n", []string{"dropped imageRegistry:", "educatesClusterConfig.imageRegistry.prefix in an EducatesConfig"}},
		{"version: 3.7.1\n", []string{"dropped version:", "imageVersions"}},
		{"clusterRuntime:\n  class: kata\n", []string{"dropped clusterRuntime: it has no version 4 equivalent"}},
		{"sessionCookies:\n  domain: educates.test\n", []string{"dropped sessionCookies:", "sessionManager.sessionCookieDomain in an EducatesConfig"}},
		{"clusterStorage:\n  class: fast\n", []string{"dropped clusterStorage:", "sessionManager.storage"}},
		{"clusterSecrets:\n  pullSecretRefs:\n    - namespace: default\n      name: pull\n", []string{"dropped clusterSecrets:", "educatesClusterConfig.imageRegistry.pullSecrets"}},
		{"clusterSecurity:\n  policyEngine: none\n", []string{"dropped clusterSecurity: a local cluster enforces cluster policy with Kyverno", "educatesClusterConfig.policyEnforcement.clusterPolicy.engine"}},
		{"workshopSecurity:\n  rulesEngine: none\n", []string{"dropped workshopSecurity: a local cluster enforces workshop policy with Kyverno", "educatesClusterConfig.policyEnforcement.workshopPolicy.engine"}},
		{"trainingPortal:\n  credentials:\n    admin:\n      password: secret\n", []string{"dropped trainingPortal:", "spec.portal.credentials"}},
		{"dockerDaemon:\n  networkMTU: 1300\n", []string{"dropped dockerDaemon:", "sessionManager.network.packetSize", "proxyCache, rootless and privileged have no version 4 equivalent"}},
		{"clusterNetwork:\n  blockCIDRs:\n    - 10.0.0.0/8\n", []string{"dropped clusterNetwork:", "sessionManager.network.blockedCidrs"}},
		{"workshopAnalytics:\n  google:\n    trackingId: G-123\n", []string{"dropped workshopAnalytics:", "sessionManager.tracking"}},
		{"notAV3Setting: 1\n", []string{"dropped notAV3Setting: it is not a version 3 setting the migration knows"}},
	} {
		name, _, _ := strings.Cut(tc.values, ":")
		t.Run(name, func(t *testing.T) {
			_, _, notice := migrateV3Values(t, tc.values)

			for _, want := range tc.want {
				if !strings.Contains(notice, want) {
					t.Errorf("notice does not mention %q:\n%s", want, notice)
				}
			}
		})
	}
}

func TestMaybeMigrateV3_SettingsDroppedFromCarriedBlocks_AreEachNoted(t *testing.T) {
	_, _, notice := migrateV3Values(t, `
clusterInfrastructure:
  provider: kind
  aws:
    region: eu-west-1
  gcp:
    project: my-project
  caCertificateRef:
    namespace: default
    name: root-ca
clusterIngress:
  domain: educates.test
  class: nginx
  tlsCertificate:
    tls.crt: CERT
  tlsCertificateRef:
    namespace: default
    name: wildcard
  caCertificate:
    ca.crt: CA
  caCertificateRef:
    namespace: default
    name: ca
  caNodeInjector:
    enabled: true
websiteStyling:
  defaultTheme: my-theme
  workshopDashboard:
    html: <p>dashboard</p>
  workshopInstructions:
    style: p {}
  workshopStarted:
    html: <p>started</p>
  workshopFinished:
    html: <p>finished</p>
  trainingPortal:
    script: x()
  frameAncestors:
    - https://example.com
`)

	for path, want := range map[string]string{
		"clusterInfrastructure.aws":              "EducatesEKSConfig",
		"clusterInfrastructure.gcp":              "EducatesGKEConfig",
		"clusterInfrastructure.caCertificateRef": "educates local secrets add ca <name> --cert ROOT.pem --key ROOT-KEY.pem",
		"clusterIngress.class":                   "educatesClusterConfig.ingress.ingressClassName in an EducatesConfig",
		"clusterIngress.tlsCertificate":          "educates local secrets add tls <name> --cert FILE --key FILE",
		"clusterIngress.tlsCertificateRef":       "educates local secrets add tls <name> --cert FILE --key FILE",
		"clusterIngress.caCertificate":           "educates local secrets add ca <name> --cert FILE",
		"clusterIngress.caCertificateRef":        "educates local secrets add ca <name> --cert FILE",
		"clusterIngress.caNodeInjector":          "sessionManager.nodeCATrust.mode in an EducatesConfig",
		"websiteStyling.workshopDashboard":       "websiteStyling.themeDataRefs",
		"websiteStyling.workshopInstructions":    "websiteStyling.themeDataRefs",
		"websiteStyling.workshopStarted":         "websiteStyling.themeDataRefs",
		"websiteStyling.workshopFinished":        "websiteStyling.themeDataRefs",
		"websiteStyling.trainingPortal":          "websiteStyling.themeDataRefs",
		"websiteStyling.frameAncestors":          "sessionManager.allowedEmbeddingHosts in an EducatesConfig",
	} {
		line := lineStartingWith(notice, "dropped "+path+":")
		if !strings.Contains(line, want) {
			t.Errorf("notice line for %s does not mention %q:\n%s", path, want, notice)
		}
	}
	for _, carried := range []string{"clusterInfrastructure", "clusterIngress", "websiteStyling",
		"clusterInfrastructure.provider", "clusterIngress.domain", "websiteStyling.defaultTheme"} {
		if strings.Contains(notice, "dropped "+carried+":") {
			t.Errorf("notice says the migration dropped %s, which it carries over:\n%s", carried, notice)
		}
	}
}

func TestMaybeMigrateV3_SessionManagerClusterAdmin_IsCarriedOverAndNotedAsNotAppliedYet(t *testing.T) {
	for _, value := range []string{"true", "false"} {
		t.Run(value, func(t *testing.T) {
			_, written, notice := migrateV3Values(t, "sessionManager:\n  clusterAdmin: "+value+"\n")

			if !strings.Contains(written, "\nclusterAdmin: "+value+"\n") {
				t.Errorf("migrated file does not set clusterAdmin: %s:\n%s", value, written)
			}
			line := lineStartingWith(notice, "carried sessionManager.clusterAdmin over to clusterAdmin: "+value)
			if !strings.Contains(line, "a local cluster does not apply clusterAdmin yet") {
				t.Errorf("notice does not say clusterAdmin is carried over and not applied yet:\n%s", notice)
			}
			if strings.Contains(notice, "dropped sessionManager") {
				t.Errorf("notice says sessionManager was dropped:\n%s", notice)
			}
		})
	}
}

func TestMaybeMigrateV3_LookupService_CarriesASetSwitchAndNotesThePrefix(t *testing.T) {
	for _, tc := range []struct {
		name, values, written string
		want                  []string
		dontWant              []string
	}{
		{
			name:    "on, default prefix",
			values:  "lookupService:\n  enabled: true\n",
			written: "\nlookupService: true\n",
			want: []string{"carried lookupService.enabled over to lookupService: true; a local cluster serves it " +
				"under the prefix lookup, where version 3 used educates-api"},
		},
		{
			name:    "on, own prefix",
			values:  "lookupService:\n  enabled: true\n  ingressPrefix: api\n",
			written: "\nlookupService: true\n",
			want: []string{"carried lookupService.enabled over to lookupService: true\n",
				"dropped lookupService.ingressPrefix: a local cluster serves the lookup service under the prefix lookup; " +
					"set lookupService.ingress.prefix in an EducatesConfig for another"},
		},
		{
			name:    "on, empty prefix",
			values:  "lookupService:\n  enabled: true\n  ingressPrefix: \"\"\n",
			written: "\nlookupService: true\n",
			want: []string{"carried lookupService.enabled over to lookupService: true; a local cluster serves it " +
				"under the prefix lookup, where version 3 used educates-api"},
		},
		{
			name:     "off",
			values:   "lookupService:\n  enabled: false\n",
			written:  "\nlookupService: false\n",
			want:     []string{"carried lookupService.enabled over to lookupService: false\n"},
			dontWant: []string{"prefix"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, written, notice := migrateV3Values(t, tc.values)

			if !strings.Contains(written, tc.written) {
				t.Errorf("migrated file does not hold %q:\n%s", tc.written, written)
			}
			for _, want := range tc.want {
				if !strings.Contains(notice, want) {
					t.Errorf("notice does not mention %q:\n%s", want, notice)
				}
			}
			for _, dontWant := range append(tc.dontWant, "dropped lookupService:") {
				if strings.Contains(notice, dontWant) {
					t.Errorf("notice mentions %q:\n%s", dontWant, notice)
				}
			}
		})
	}
}

func TestMaybeMigrateV3_NoLookupServiceSwitch_LeavesTheDefault(t *testing.T) {
	_, written, notice := migrateV3Values(t, "lookupService:\n  ingressPrefix: api\n")

	if strings.Contains(written, "lookupService") {
		t.Errorf("migrated file sets lookupService, want it left to the default:\n%s", written)
	}
	if strings.Contains(notice, "carried lookupService") {
		t.Errorf("notice says lookupService was carried over:\n%s", notice)
	}
}

func TestMaybeMigrateV3_SettingsWithNoValue_AreNotNoted(t *testing.T) {
	_, _, notice := migrateV3Values(t,
		"version: \"\"\nclusterNetwork: {}\nclusterSecrets:\n  pullSecretRefs: []\ndebug:\nclusterIngress:\n  class: \"\"\n")

	if strings.Contains(notice, "dropped ") {
		t.Errorf("notice notes settings that hold no value:\n%s", notice)
	}
}

// lineStartingWith returns the line of notice that starts with prefix,
// or "" when there is none.
func lineStartingWith(notice, prefix string) string {
	for line := range strings.Lines(notice) {
		if strings.HasPrefix(line, prefix) {
			return line
		}
	}
	return ""
}

func TestMaybeMigrateV3_NoResolverTargetAddress_NoNote(t *testing.T) {
	_, _, notice := migrateV3Values(t, "localDNSResolver:\n  extraDomains:\n    - example.test\n")

	if strings.Contains(notice, "targetAddress") {
		t.Errorf("notice mentions targetAddress for a file without one:\n%s", notice)
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
