package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	apiv1 "k8s.io/api/core/v1"

	"github.com/educates/educates-training-platform/client-programs/internal/testpki"
	"github.com/educates/educates-training-platform/client-programs/pkg/secrets"
)

const (
	emptyLocal = `apiVersion: cli.educates.dev/v1alpha1
kind: EducatesLocalConfig
`
	localWithListenAddress = `apiVersion: cli.educates.dev/v1alpha1
kind: EducatesLocalConfig
cluster:
  listenAddress: 10.10.10.1
`
	localWithDomain = `apiVersion: cli.educates.dev/v1alpha1
kind: EducatesLocalConfig
cluster:
  listenAddress: 10.10.10.1
ingress:
  domain: workshop.test
operator:
  image:
    repository: ghcr.io/educates/educates-operator
    tag: 4.0.0
`
	escapeMinimal = `apiVersion: cli.educates.dev/v1alpha1
kind: EducatesConfig
`
)

func writeFixture(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "cfg.yaml")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// stageRootCA registers a root CA named test-ca in the local secrets
// cache of the current data home, as
// `educates local secrets add ca test-ca --cert --key` does.
func stageRootCA(t *testing.T) {
	t.Helper()
	root := testpki.NewRootCA()
	for _, s := range []*apiv1.Secret{
		secrets.NewCATrustSecret("test-ca", root.CertPEM),
		secrets.NewCARootKeyPairSecret("test-ca", root.CertPEM, root.KeyPEM),
	} {
		if err := secrets.WriteCachedSecret(s); err != nil {
			t.Fatal(err)
		}
	}
}

// withRootCA creates a fresh temp data home, sets
// $EDUCATES_CLI_DATA_HOME to it and registers the root CA test-ca in its
// local secrets cache. Use it when the test doesn't already manage its
// own data home.
func withRootCA(t *testing.T) (dataHome string) {
	t.Helper()
	dataHome = t.TempDir()
	t.Setenv("EDUCATES_CLI_DATA_HOME", dataHome)
	stageRootCA(t)
	return
}

func TestRender_Config_MissingDomain_Errors(t *testing.T) {
	p := ProjectInfo{Version: "test", ImageRepository: "ghcr.io/educates"}
	o := &PlatformRenderOptions{Config: writeFixture(t, emptyLocal)}

	var buf bytes.Buffer
	err := p.runRender(&buf, o)
	if err == nil {
		t.Fatal("expected error for missing ingress.domain in --config mode")
	}
	if !strings.Contains(err.Error(), "ingress.domain is required") {
		t.Errorf("error %q does not mention required field", err)
	}
	if buf.Len() != 0 {
		t.Errorf("expected no output when erroring, got %d bytes", buf.Len())
	}
}

func TestRender_Config_WithDomain_NoHostHeader(t *testing.T) {
	withRootCA(t)
	p := ProjectInfo{Version: "test", ImageRepository: "ghcr.io/educates"}
	o := &PlatformRenderOptions{Config: writeFixture(t, localWithDomain)}

	var buf bytes.Buffer
	if err := p.runRender(&buf, o); err != nil {
		t.Fatalf("runRender: %v", err)
	}
	s := buf.String()

	// No host-derivation note (user-provided domain).
	if strings.Contains(s, "auto-derived from host IP") {
		t.Errorf("--config mode should not emit host-defaulting note:\n%s", s)
	}
	// Both sections present.
	for _, want := range []string{
		"# === operator chart values",
		"# === platform CRs",
		"domain: workshop.test",
		"tag: 4.0.0",
		"repository: ghcr.io/educates/educates-operator",
		"kind: EducatesClusterConfig",
		"kind: SecretsManager",
		"kind: SessionManager",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("output missing %q", want)
		}
	}
}

func TestRender_Config_CLIDefaults_AppliedWhenImageEmpty(t *testing.T) {
	// Tag and repository come from ProjectInfo when config leaves them empty.
	withRootCA(t)
	cfgYAML := `apiVersion: cli.educates.dev/v1alpha1
kind: EducatesLocalConfig
ingress:
  domain: workshop.test
`
	p := ProjectInfo{Version: "v9.9.9-test", ImageRepository: "ghcr.io/custom"}
	o := &PlatformRenderOptions{Config: writeFixture(t, cfgYAML)}

	var buf bytes.Buffer
	if err := p.runRender(&buf, o); err != nil {
		t.Fatalf("runRender: %v", err)
	}
	s := buf.String()
	for _, want := range []string{
		"tag: v9.9.9-test",
		"repository: ghcr.io/custom/educates-operator",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("CLI defaults: output missing %q:\n%s", want, s)
		}
	}
}

func TestRender_LocalConfig_NipDomainOfTheListenAddress_DefaultsInsecure(t *testing.T) {
	// Point --local-config at a temp data home so the test doesn't touch
	// the user's actual data home.
	dataHome := t.TempDir()
	if err := os.WriteFile(filepath.Join(dataHome, "config.yaml"), []byte(localWithListenAddress), 0o644); err != nil {
		t.Fatal(err)
	}
	// No CA is staged: a local config with no domain falls back to the
	// nip.io name of its listen address, which defaults to an insecure
	// plain-HTTP install needing no CA.
	t.Setenv("EDUCATES_CLI_DATA_HOME", dataHome)

	p := ProjectInfo{Version: "test", ImageRepository: "ghcr.io/educates"}
	o := &PlatformRenderOptions{LocalConfig: true}

	var buf bytes.Buffer
	if err := p.runRender(&buf, o); err != nil {
		t.Fatalf("runRender: %v", err)
	}
	s := buf.String()
	for _, want := range []string{"domain: 10-10-10-1.nip.io", "provider: None", "protocol: http"} {
		if !strings.Contains(s, want) {
			t.Errorf("--local-config with no domain should render %q:\n%s", want, s)
		}
	}
}

func TestRender_LocalConfig_RefusesADefaultConfigWithoutAListenAddress(t *testing.T) {
	stubHostIP(t, "192.168.50.151")
	dataHome := t.TempDir()
	if err := os.WriteFile(filepath.Join(dataHome, "config.yaml"), []byte(emptyLocal), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("EDUCATES_CLI_DATA_HOME", dataHome)

	p := ProjectInfo{Version: "test", ImageRepository: "ghcr.io/educates"}
	err := p.runRender(&bytes.Buffer{}, &PlatformRenderOptions{LocalConfig: true})

	wantError(t, err, "cluster.listenAddress", "must be set",
		"educates local config set cluster.listenAddress 192.168.50.151")
}

func TestRender_RootCA_ReferencesTheDomainCAKeyPairWithoutWritingIt(t *testing.T) {
	dataHome := withRootCA(t)
	if err := os.WriteFile(filepath.Join(dataHome, "config.yaml"), []byte(localWithDomain), 0o644); err != nil {
		t.Fatal(err)
	}
	before := cachedSecretNames(t)

	p := ProjectInfo{Version: "test", ImageRepository: "ghcr.io/educates"}
	var buf bytes.Buffer
	if err := p.runRender(&buf, &PlatformRenderOptions{LocalConfig: true}); err != nil {
		t.Fatalf("runRender: %v", err)
	}

	s := buf.String()
	for _, want := range []string{
		"caKeyPairSecretRef:\n            name: workshop.test-ca-key-pair\n            namespace: educates-secrets",
		"caCertificateSecretRef:\n            name: test-ca\n            namespace: educates-secrets",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("render output missing %q:\n%s", want, s)
		}
	}
	if after := cachedSecretNames(t); !reflect.DeepEqual(after, before) {
		t.Errorf("render changed the local secrets cache from %v to %v", before, after)
	}
}

func TestRender_TLSCertificate_ReportsStaticCertificateWithoutWriting(t *testing.T) {
	mkcert := testpki.NewRootCA()
	leaf := mkcert.NewLeaf(testpki.WithDNSNames("*.workshop.test", "*.other.test"))

	for name, tc := range map[string]struct {
		cacheCA bool
		want    []string
		absent  []string
	}{
		"with the CA that issued it": {
			cacheCA: true,
			want: []string{
				"tlsSecretRef:\n          name: workshop-tls\n          namespace: educates-secrets",
				"caCertificateSecretRef:\n          name: mkcert-ca\n          namespace: educates-secrets",
				"# Serving TLS certificate \"workshop-tls\" for ingress domain \"workshop.test\" with the\n# StaticCertificate provider: cert-manager will not be installed.",
				"#   educates local secrets remove workshop-tls\n",
				"# It also serves these other domains, which removing it affects too: other.test.\n",
			},
			absent: []string{"warning:", "caKeyPairSecretRef"},
		},
		"without it": {
			want: []string{
				"tlsSecretRef:\n          name: workshop-tls",
				"# warning: no CA certificate in the local secrets cache verifies TLS certificate\n# \"workshop-tls\"",
			},
			absent: []string{"caCertificateSecretRef"},
		},
	} {
		t.Run(name, func(t *testing.T) {
			dataHome := t.TempDir()
			t.Setenv("EDUCATES_CLI_DATA_HOME", dataHome)
			if err := secrets.WriteCachedSecret(secrets.NewTLSSecret("workshop-tls", leaf.CertPEM, leaf.KeyPEM)); err != nil {
				t.Fatal(err)
			}
			if tc.cacheCA {
				if err := secrets.WriteCachedSecret(secrets.NewCATrustSecret("mkcert-ca", mkcert.CertPEM)); err != nil {
					t.Fatal(err)
				}
			}
			if err := os.WriteFile(filepath.Join(dataHome, "config.yaml"), []byte(localWithDomain), 0o644); err != nil {
				t.Fatal(err)
			}
			before := cachedSecretNames(t)

			p := ProjectInfo{Version: "test", ImageRepository: "ghcr.io/educates"}
			var buf bytes.Buffer
			if err := p.runRender(&buf, &PlatformRenderOptions{LocalConfig: true}); err != nil {
				t.Fatalf("runRender: %v", err)
			}

			s := buf.String()
			for _, want := range append(tc.want, "provider: StaticCertificate") {
				if !strings.Contains(s, want) {
					t.Errorf("render output missing %q:\n%s", want, s)
				}
			}
			for _, absent := range tc.absent {
				if strings.Contains(s, absent) {
					t.Errorf("render output contains %q:\n%s", absent, s)
				}
			}
			if after := cachedSecretNames(t); !reflect.DeepEqual(after, before) {
				t.Errorf("render changed the local secrets cache from %v to %v", before, after)
			}
		})
	}
}

func TestRender_AlphaCAEntry_Ignored(t *testing.T) {
	// A CA cached with its key by an alpha release, for the very domain
	// rendered, lists as invalid: render neither references it nor
	// accepts it in place of a root CA.
	t.Run("beside a root CA", func(t *testing.T) {
		dataHome := withRootCA(t)
		cacheAlphaCA(t, "workshop.test-ca", testpki.NewRootCA(), "workshop.test")
		if err := os.WriteFile(filepath.Join(dataHome, "config.yaml"), []byte(localWithDomain), 0o644); err != nil {
			t.Fatal(err)
		}

		p := ProjectInfo{Version: "test", ImageRepository: "ghcr.io/educates"}
		var buf bytes.Buffer
		if err := p.runRender(&buf, &PlatformRenderOptions{LocalConfig: true}); err != nil {
			t.Fatalf("runRender: %v", err)
		}

		s := buf.String()
		for _, want := range []string{
			"caKeyPairSecretRef:\n            name: workshop.test-ca-key-pair\n",
			"caCertificateSecretRef:\n            name: test-ca\n",
		} {
			if !strings.Contains(s, want) {
				t.Errorf("render output missing %q:\n%s", want, s)
			}
		}
		if strings.Contains(s, "name: workshop.test-ca\n") {
			t.Errorf("render output references the alpha CA:\n%s", s)
		}
	})

	t.Run("alone", func(t *testing.T) {
		dataHome := t.TempDir()
		t.Setenv("EDUCATES_CLI_DATA_HOME", dataHome)
		cacheAlphaCA(t, "workshop.test-ca", testpki.NewRootCA(), "workshop.test")
		if err := os.WriteFile(filepath.Join(dataHome, "config.yaml"), []byte(localWithDomain), 0o644); err != nil {
			t.Fatal(err)
		}

		p := ProjectInfo{Version: "test", ImageRepository: "ghcr.io/educates"}
		err := p.runRender(&bytes.Buffer{}, &PlatformRenderOptions{LocalConfig: true})
		if err == nil || !strings.Contains(err.Error(), "educates local secrets add ca <name>") {
			t.Errorf("render error = %v, want one saying no root CA is cached", err)
		}
	})
}

func TestRender_EscapeKind_PureUserOutput(t *testing.T) {
	p := ProjectInfo{Version: "test", ImageRepository: "ghcr.io/educates"}
	o := &PlatformRenderOptions{Config: writeFixture(t, escapeMinimal)}

	var buf bytes.Buffer
	if err := p.runRender(&buf, o); err != nil {
		t.Fatalf("runRender: %v", err)
	}
	s := buf.String()

	// Escape kind: no CLI defaulting. The operator chart values section
	// should NOT contain the CLI's projectVersion-derived tag — the
	// fixture didn't declare operator.image, so output is empty.
	if strings.Contains(s, "tag: test") {
		t.Errorf("escape kind should not apply CLI image defaults:\n%s", s)
	}
	if !strings.Contains(s, "kind: EducatesClusterConfig") {
		t.Errorf("escape kind should still emit CR wrappers:\n%s", s)
	}
}
