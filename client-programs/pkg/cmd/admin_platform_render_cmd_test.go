package cmd

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
	"helm.sh/helm/v4/pkg/chart/v2/loader"
	apiv1 "k8s.io/api/core/v1"

	"github.com/educates/educates-training-platform/client-programs/internal/testpki"
	"github.com/educates/educates-training-platform/client-programs/pkg/secrets"
)

const (
	emptyLocal = `apiVersion: cli.educates.dev/v1alpha1
kind: EducatesLocalConfig
`
	localWithDomain = `apiVersion: cli.educates.dev/v1alpha1
kind: EducatesLocalConfig
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
	gkeMinimal = `apiVersion: cli.educates.dev/v1alpha1
kind: EducatesGKEConfig
gcp:
  project: my-gcp-project
domain: workshops.example.com
acme:
  email: ops@example.com
`
	eksMinimal = `apiVersion: cli.educates.dev/v1alpha1
kind: EducatesEKSConfig
aws:
  accountId: "123456789012"
  region: us-east-1
  route53HostedZoneId: Z0123456789ABCDEF
domain: workshops.example.com
acme:
  email: ops@example.com
`
	inlineMinimal = `apiVersion: cli.educates.dev/v1alpha1
kind: EducatesInlineConfig
domain: workshop.test
ingressClassName: contour
wildcardCertificateSecret: educates-wildcard-tls
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

func TestRender_Config_ScenarioKinds_CLIDefaultsTheOperatorImage(t *testing.T) {
	for kind, cfgYAML := range map[string]string{
		"EducatesGKEConfig":    gkeMinimal,
		"EducatesEKSConfig":    eksMinimal,
		"EducatesInlineConfig": inlineMinimal,
	} {
		t.Run(kind, func(t *testing.T) {
			p := ProjectInfo{Version: "v9.9.9-test", ImageRepository: "ghcr.io/custom"}
			o := &PlatformRenderOptions{Config: writeFixture(t, cfgYAML)}

			var buf bytes.Buffer
			if err := p.runRender(&buf, o); err != nil {
				t.Fatalf("runRender: %v", err)
			}
			s := buf.String()
			want := "image:\n  repository: ghcr.io/custom/educates-operator\n  tag: v9.9.9-test\n"
			if !strings.Contains(s, want) {
				t.Errorf("output missing %q:\n%s", want, s)
			}
		})
	}
}

func TestRender_Config_ScenarioKinds_NameThePullSecretsTheOperatorNamespaceNeeds(t *testing.T) {
	for kind, cfgYAML := range map[string]string{
		"EducatesGKEConfig":    gkeMinimal,
		"EducatesEKSConfig":    eksMinimal,
		"EducatesInlineConfig": inlineMinimal,
	} {
		t.Run(kind, func(t *testing.T) {
			p := ProjectInfo{Version: "test", ImageRepository: "ghcr.io/educates"}
			o := &PlatformRenderOptions{Config: writeFixture(t, cfgYAML+"\nimageRegistry:\n  pullSecrets: [mirror]\n")}

			var buf bytes.Buffer
			if err := p.runRender(&buf, o); err != nil {
				t.Fatalf("runRender: %v", err)
			}
			want := "# Before 'helm install', create the image pull secret \"mirror\", of type\n# kubernetes.io/dockerconfigjson, in the operator namespace, educates-installer."
			if s := buf.String(); !strings.Contains(s, want) {
				t.Errorf("output missing %q:\n%s", want, s)
			}
		})
	}
}

// A local config whose TLS ends at the router references the cached CA
// trust Secret of the router's certificate, so render says to sync it.
func TestRender_TLSEndedAtTheRouter_SaysToSyncTheCATrust(t *testing.T) {
	dataHome := withRootCA(t)
	config := strings.Replace(localWithDomain, "  domain: workshop.test\n", "  domain: workshop.test\n  externalTLSTermination: true\n", 1)
	if err := os.WriteFile(filepath.Join(dataHome, "config.yaml"), []byte(config), 0o644); err != nil {
		t.Fatal(err)
	}

	p := ProjectInfo{Version: "test", ImageRepository: "ghcr.io/educates"}
	var buf bytes.Buffer
	if err := p.runRender(&buf, &PlatformRenderOptions{LocalConfig: true}); err != nil {
		t.Fatalf("runRender: %v", err)
	}
	if want := "# Before 'helm install', run 'educates local secrets sync' to copy"; !strings.Contains(buf.String(), want) {
		t.Errorf("render output missing %q:\n%s", want, buf.String())
	}
}

func TestRender_LocalConfig_NipDomainOfTheRouterAddress_DefaultsInsecure(t *testing.T) {
	// Point --local-config at a temp data home so the test doesn't touch
	// the user's actual data home.
	dataHome := t.TempDir()
	if err := os.WriteFile(filepath.Join(dataHome, "config.yaml"), []byte(emptyLocal), 0o644); err != nil {
		t.Fatal(err)
	}
	writeRouterFile(t, dataHome, handWrittenRouterFile)
	// No CA is staged: a local config with no domain falls back to the
	// nip.io name of the router address, which defaults to an insecure
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

// A config without a domain takes it from the router address, so with no
// router file render says how to set the router address.
func TestRender_LocalConfig_RefusesADefaultConfigWithoutADomainOrARouterFile(t *testing.T) {
	dataHome := t.TempDir()
	if err := os.WriteFile(filepath.Join(dataHome, "config.yaml"), []byte(emptyLocal), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("EDUCATES_CLI_DATA_HOME", dataHome)

	p := ProjectInfo{Version: "test", ImageRepository: "ghcr.io/educates"}
	err := p.runRender(&bytes.Buffer{}, &PlatformRenderOptions{LocalConfig: true})

	wantError(t, err, "router.yaml", "educates local router update --address <ip>")
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
		"caKeyPairSecretRef:\n            name: workshop.test-ca-key-pair\n",
		"caCertificateSecretRef:\n            name: test-ca\n",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("render output missing %q:\n%s", want, s)
		}
	}
	// The references name no namespace: the operator namespace, where
	// the cache is synced.
	if strings.Contains(s, "namespace: educates-secrets") {
		t.Errorf("render output references the retired educates-secrets namespace:\n%s", s)
	}
	if want := "# Before 'helm install', run 'educates local secrets sync' to copy the cached secrets"; !strings.Contains(s, want) {
		t.Errorf("render output missing %q:\n%s", want, s)
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
				"tlsSecretRef:\n          name: workshop-tls\n",
				"caCertificateSecretRef:\n          name: mkcert-ca\n",
				"# Serving TLS certificate \"workshop-tls\" for ingress domain \"workshop.test\" with the\n# StaticCertificate provider: cert-manager will not be installed.",
				"#   educates local secrets remove workshop-tls\n",
				"# It also serves these other domains, which removing it affects too: other.test.\n",
			},
			absent: []string{"warning:", "caKeyPairSecretRef", "namespace: educates-secrets"},
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

// renderDocuments decodes a render stream into its YAML documents.
func renderDocuments(t *testing.T, stream []byte) []map[string]interface{} {
	t.Helper()
	var docs []map[string]interface{}
	dec := yaml.NewDecoder(bytes.NewReader(stream))
	for {
		var doc map[string]interface{}
		err := dec.Decode(&doc)
		if errors.Is(err, io.EOF) {
			return docs
		}
		if err != nil {
			t.Fatalf("decode render output: %v\n%s", err, stream)
		}
		docs = append(docs, doc)
	}
}

// renderKinds returns the kind of each YAML document of a render
// stream, nil for a document without one.
func renderKinds(t *testing.T, stream []byte) []interface{} {
	t.Helper()
	var kinds []interface{}
	for _, doc := range renderDocuments(t, stream) {
		kinds = append(kinds, doc["kind"])
	}
	return kinds
}

// platformCRKinds are the kinds of the platform CRs rendered for a
// scenario kind with the lookup service on, in deploy order.
var platformCRKinds = []interface{}{"EducatesClusterConfig", "SecretsManager", "LookupService", "SessionManager"}

// helmValues reads stream as `helm install -f` reads a values file.
func helmValues(t *testing.T, stream []byte) map[string]interface{} {
	t.Helper()
	values, err := loader.LoadValues(bytes.NewReader(stream))
	if err != nil {
		t.Fatalf("helm reading values: %v\n%s", err, stream)
	}
	return values
}

func TestRender_Stdout_ValuesAreADocumentOfTheirOwn(t *testing.T) {
	p := ProjectInfo{Version: "v9.9.9-test", ImageRepository: "ghcr.io/custom"}
	o := &PlatformRenderOptions{Config: writeFixture(t, gkeMinimal)}

	var buf bytes.Buffer
	if err := p.runRender(&buf, o); err != nil {
		t.Fatalf("runRender: %v", err)
	}

	docs := renderDocuments(t, buf.Bytes())
	want := append([]interface{}{nil}, platformCRKinds...)
	if kinds := renderKinds(t, buf.Bytes()); !reflect.DeepEqual(kinds, want) {
		t.Fatalf("document kinds = %v, want %v:\n%s", kinds, want, buf.String())
	}
	if docs[0]["logLevel"] != "info" {
		t.Errorf("first document = %v, want the operator chart values", docs[0])
	}
	if _, ok := docs[1]["logLevel"]; ok {
		t.Errorf("EducatesClusterConfig carries the chart values: %v", docs[1])
	}
}

func TestRender_FileFlags_WriteEachPartToItsFile(t *testing.T) {
	dir := t.TempDir()
	valuesFile := filepath.Join(dir, "values.yaml")
	resourcesFile := filepath.Join(dir, "resources.yaml")
	p := ProjectInfo{Version: "v9.9.9-test", ImageRepository: "ghcr.io/custom"}
	o := &PlatformRenderOptions{
		Config:        writeFixture(t, gkeMinimal),
		ValuesFile:    valuesFile,
		ResourcesFile: resourcesFile,
	}

	var buf bytes.Buffer
	if err := p.runRender(&buf, o); err != nil {
		t.Fatalf("runRender: %v", err)
	}

	// Helm reads every document of a values file and merges them, so
	// the values file must hold the chart values and nothing else.
	raw, err := os.ReadFile(valuesFile)
	if err != nil {
		t.Fatal(err)
	}
	wantValues := map[string]interface{}{
		"image": map[string]interface{}{
			"repository": "ghcr.io/custom/educates-operator",
			"tag":        "v9.9.9-test",
		},
		"logLevel": "info",
	}
	if values := helmValues(t, raw); !reflect.DeepEqual(values, wantValues) {
		t.Errorf("helm reads values %v, want %v", values, wantValues)
	}

	raw, err = os.ReadFile(resourcesFile)
	if err != nil {
		t.Fatal(err)
	}
	if kinds := renderKinds(t, raw); !reflect.DeepEqual(kinds, platformCRKinds) {
		t.Errorf("resources file kinds = %v, want %v:\n%s", kinds, platformCRKinds, raw)
	}

	if docs := renderDocuments(t, buf.Bytes()); len(docs) != 0 {
		t.Errorf("stdout carries %d documents, want none:\n%s", len(docs), buf.String())
	}
}

func TestRender_FileFlags_StdoutCarriesThePartNotSentToAFile(t *testing.T) {
	p := ProjectInfo{Version: "v9.9.9-test", ImageRepository: "ghcr.io/custom"}

	t.Run("--resources-file leaves the values, for helm install -f -", func(t *testing.T) {
		o := &PlatformRenderOptions{
			Config:        writeFixture(t, gkeMinimal),
			ResourcesFile: filepath.Join(t.TempDir(), "resources.yaml"),
		}
		var buf bytes.Buffer
		if err := p.runRender(&buf, o); err != nil {
			t.Fatalf("runRender: %v", err)
		}
		if values := helmValues(t, buf.Bytes()); values["kind"] != nil || values["logLevel"] != "info" {
			t.Errorf("helm reads values %v from stdout, want the chart values alone", values)
		}
	})

	t.Run("--values-file leaves the CRs, for kubectl apply -f -", func(t *testing.T) {
		o := &PlatformRenderOptions{
			Config:     writeFixture(t, gkeMinimal),
			ValuesFile: filepath.Join(t.TempDir(), "values.yaml"),
		}
		var buf bytes.Buffer
		if err := p.runRender(&buf, o); err != nil {
			t.Fatalf("runRender: %v", err)
		}
		if kinds := renderKinds(t, buf.Bytes()); !reflect.DeepEqual(kinds, platformCRKinds) {
			t.Errorf("stdout kinds = %v, want %v:\n%s", kinds, platformCRKinds, buf.String())
		}
	})

	t.Run("a file named - is stdout", func(t *testing.T) {
		t.Chdir(t.TempDir())
		o := &PlatformRenderOptions{
			Config:        writeFixture(t, gkeMinimal),
			ValuesFile:    "-",
			ResourcesFile: "resources.yaml",
		}
		var buf bytes.Buffer
		if err := p.runRender(&buf, o); err != nil {
			t.Fatalf("runRender: %v", err)
		}
		if values := helmValues(t, buf.Bytes()); values["logLevel"] != "info" {
			t.Errorf("helm reads values %v from stdout, want the chart values", values)
		}
		if _, err := os.Stat("-"); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("render wrote a file named -")
		}
	})
}

func TestRender_FileFlags_LocalConfigNotesStayOnStdout(t *testing.T) {
	// A cached TLS certificate for the ingress domain makes render note
	// that it serves the domain.
	dataHome := t.TempDir()
	t.Setenv("EDUCATES_CLI_DATA_HOME", dataHome)
	mkcert := testpki.NewRootCA()
	leaf := mkcert.NewLeaf(testpki.WithDNSNames("*.workshop.test"))
	for _, s := range []*apiv1.Secret{
		secrets.NewTLSSecret("workshop-tls", leaf.CertPEM, leaf.KeyPEM),
		secrets.NewCATrustSecret("mkcert-ca", mkcert.CertPEM),
	} {
		if err := secrets.WriteCachedSecret(s); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dataHome, "config.yaml"), []byte(localWithDomain), 0o644); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()

	p := ProjectInfo{Version: "test", ImageRepository: "ghcr.io/educates"}
	o := &PlatformRenderOptions{
		LocalConfig:   true,
		ValuesFile:    filepath.Join(dir, "values.yaml"),
		ResourcesFile: filepath.Join(dir, "resources.yaml"),
	}
	var buf bytes.Buffer
	if err := p.runRender(&buf, o); err != nil {
		t.Fatalf("runRender: %v", err)
	}

	if buf.Len() == 0 || len(renderDocuments(t, buf.Bytes())) != 0 {
		t.Errorf("stdout = %q, want the certificate notes alone", buf.String())
	}
	for _, line := range strings.SplitAfter(strings.TrimSuffix(buf.String(), "\n"), "\n") {
		if !strings.HasPrefix(line, "#") {
			t.Errorf("stdout line %q is not a note", line)
		}
	}
	for _, f := range []string{o.ValuesFile, o.ResourcesFile} {
		if raw, _ := os.ReadFile(f); bytes.Contains(raw, []byte("#")) {
			t.Errorf("%s carries the notes:\n%s", f, raw)
		}
	}
}

func TestRender_FileFlags_AFailedWriteLeavesTheOtherFileAsItWas(t *testing.T) {
	dir := t.TempDir()
	valuesFile := filepath.Join(dir, "values.yaml")
	const earlier = "logLevel: debug\n"
	if err := os.WriteFile(valuesFile, []byte(earlier), 0o644); err != nil {
		t.Fatal(err)
	}

	p := ProjectInfo{Version: "v9.9.9-test", ImageRepository: "ghcr.io/custom"}
	o := &PlatformRenderOptions{
		Config:        writeFixture(t, gkeMinimal),
		ValuesFile:    valuesFile,
		ResourcesFile: filepath.Join(dir, "missing", "resources.yaml"),
	}
	var buf bytes.Buffer
	if err := p.runRender(&buf, o); err == nil {
		t.Fatal("runRender succeeded writing into a missing directory")
	}

	if raw, _ := os.ReadFile(valuesFile); string(raw) != earlier {
		t.Errorf("values file = %q, want it left as %q", raw, earlier)
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 1 {
		t.Errorf("render left %d entries in %s, want the values file alone", len(entries), dir)
	}
	if buf.Len() != 0 {
		t.Errorf("render wrote to stdout:\n%s", buf.String())
	}
}

func TestRender_FileFlags_RefuseToOverwriteAnotherInput(t *testing.T) {
	p := ProjectInfo{Version: "v9.9.9-test", ImageRepository: "ghcr.io/custom"}

	for name, tc := range map[string]struct {
		options func(cfgPath, dir string) PlatformRenderOptions
		want    string
	}{
		"both flags name one file": {
			options: func(cfgPath, dir string) PlatformRenderOptions {
				t.Chdir(dir)
				return PlatformRenderOptions{
					Config:        cfgPath,
					ValuesFile:    filepath.Join(dir, "install.yaml"),
					ResourcesFile: "install.yaml",
				}
			},
			want: "--values-file and --resources-file name the same file",
		},
		"the values file is the config": {
			options: func(cfgPath, _ string) PlatformRenderOptions {
				return PlatformRenderOptions{Config: cfgPath, ValuesFile: cfgPath}
			},
			want: "--values-file names the config file being rendered",
		},
		"the resources file is the config": {
			options: func(cfgPath, _ string) PlatformRenderOptions {
				return PlatformRenderOptions{Config: cfgPath, ResourcesFile: cfgPath}
			},
			want: "--resources-file names the config file being rendered",
		},
	} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			cfgPath := writeFixture(t, gkeMinimal)
			o := tc.options(cfgPath, dir)

			var buf bytes.Buffer
			err := p.runRender(&buf, &o)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("runRender error = %v, want one saying %q", err, tc.want)
			}
			if after, _ := os.ReadFile(cfgPath); string(after) != gkeMinimal {
				t.Errorf("render changed the config file to:\n%s", after)
			}
			if entries, _ := os.ReadDir(dir); len(entries) != 0 {
				t.Errorf("render wrote %d files into %s", len(entries), dir)
			}
			if buf.Len() != 0 {
				t.Errorf("render wrote to stdout:\n%s", buf.String())
			}
		})
	}
}

func TestRender_FileFlags_KeepALinkAndThePermissionsOfAFileReplaced(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "chart-values.yaml")
	if err := os.WriteFile(target, []byte("logLevel: debug\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "values.yaml")
	if err := os.Symlink("chart-values.yaml", link); err != nil {
		t.Fatal(err)
	}

	p := ProjectInfo{Version: "v9.9.9-test", ImageRepository: "ghcr.io/custom"}
	o := &PlatformRenderOptions{Config: writeFixture(t, gkeMinimal), ValuesFile: link}
	if err := p.runRender(&bytes.Buffer{}, o); err != nil {
		t.Fatalf("runRender: %v", err)
	}

	if info, err := os.Lstat(link); err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Errorf("%s is no longer a symbolic link", link)
	}
	info, err := os.Stat(target)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("%s has mode %v, want it kept as 0600", target, info.Mode().Perm())
	}
	raw, _ := os.ReadFile(target)
	if values := helmValues(t, raw); values["logLevel"] != "info" {
		t.Errorf("%s holds values %v, want the rendered chart values", target, values)
	}
}
