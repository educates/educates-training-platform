package localcerts_test

import (
	"crypto/x509"
	"encoding/base64"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/educates/educates-training-platform/client-programs/internal/testpki"
	"github.com/educates/educates-training-platform/client-programs/pkg/config/translator"
	"github.com/educates/educates-training-platform/client-programs/pkg/config/v1alpha1"
	"github.com/educates/educates-training-platform/client-programs/pkg/localcerts"
	"github.com/educates/educates-training-platform/client-programs/pkg/secrets"
	"github.com/educates/educates-training-platform/client-programs/pkg/utils"
	apiv1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// withDataHome points the CLI data home, and so the local secrets cache, at
// a fresh temporary directory and returns it.
func withDataHome(t *testing.T) string {
	t.Helper()
	dataHome := t.TempDir()
	t.Setenv(utils.EducatesCLIDataHomeEnv, dataHome)
	return dataHome
}

func localConfig(domain string, insecure bool) *v1alpha1.EducatesLocalConfig {
	cfg := &v1alpha1.EducatesLocalConfig{}
	cfg.Ingress.Domain = domain
	cfg.Ingress.Insecure = insecure
	return cfg
}

// translatedIngress translates cfg with the plan's translator options and
// returns the EducatesClusterConfig spec.ingress.
func translatedIngress(t *testing.T, cfg *v1alpha1.EducatesLocalConfig, plan *localcerts.Plan) map[string]interface{} {
	t.Helper()
	out, err := translator.Translate(cfg, plan.TranslatorOptions())
	if err != nil {
		t.Fatalf("translate: %v", err)
	}
	spec := out.EducatesClusterConfig["spec"].(map[string]interface{})
	return spec["ingress"].(map[string]interface{})
}

// nested walks a nested map by dotted path.
func nested(t *testing.T, root map[string]interface{}, path string) interface{} {
	t.Helper()
	var cur interface{} = root
	for _, seg := range strings.Split(path, ".") {
		m, ok := cur.(map[string]interface{})
		if !ok {
			t.Fatalf("%s: %q is not a map in %v", path, seg, root)
		}
		v, found := m[seg]
		if !found {
			t.Fatalf("%s: %q missing in %v", path, seg, root)
		}
		cur = v
	}
	return cur
}

// withDomainAnnotation records domain on s with the domain annotation, as
// caches from earlier releases do, and returns s.
func withDomainAnnotation(s *apiv1.Secret, domain string) *apiv1.Secret {
	if s.Annotations == nil {
		s.Annotations = map[string]string{}
	}
	s.Annotations[secrets.DomainAnnotation] = domain
	return s
}

func writeCached(t *testing.T, s *apiv1.Secret) {
	t.Helper()
	if err := secrets.WriteCachedSecret(s); err != nil {
		t.Fatal(err)
	}
}

// cacheRoot registers root as root CA name, as
// `educates local secrets add ca NAME --cert --key` does: its CA trust
// Secret and its CA root key pair.
func cacheRoot(t *testing.T, name string, root *testpki.Certificate) {
	t.Helper()
	writeCached(t, secrets.NewCATrustSecret(name, root.CertPEM))
	writeCached(t, secrets.NewCARootKeyPairSecret(name, root.CertPEM, root.KeyPEM))
}

// removeCached removes Secrets from the local secrets cache.
func removeCached(t *testing.T, names ...string) {
	t.Helper()
	for _, name := range names {
		if err := secrets.RemoveCachedSecret(name); err != nil {
			t.Fatal(err)
		}
	}
}

// cacheFiles snapshots the local secrets cache: file name to content.
func cacheFiles(t *testing.T, dataHome string) map[string]string {
	t.Helper()
	files := map[string]string{}
	entries, err := os.ReadDir(filepath.Join(dataHome, "secrets"))
	if os.IsNotExist(err) {
		return files
	}
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		data, err := os.ReadFile(filepath.Join(dataHome, "secrets", e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		files[e.Name()] = string(data)
	}
	return files
}

// noteRecorder is a localcerts.Reporter that records each note.
type noteRecorder struct{ notes []string }

func (r *noteRecorder) Note(msg string) { r.notes = append(r.notes, msg) }

// resolveAndCommit resolves cfg and commits the plan, as a deploy does,
// returning the plan and the lines the commit printed.
func resolveAndCommit(t *testing.T, cfg *v1alpha1.EducatesLocalConfig) (*localcerts.Plan, []string) {
	t.Helper()
	plan, err := localcerts.Resolve(cfg)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	var r noteRecorder
	if err := plan.Commit(&r); err != nil {
		t.Fatalf("Commit: %v", err)
	}
	return plan, r.notes
}

// mintedCAKeyPair is a CA key pair read back from the cache.
type mintedCAKeyPair struct {
	secret *apiv1.Secret
	// ca is the CA certificate with its key and issuer, usable to sign
	// test leaves.
	ca    *testpki.Certificate
	chain []*x509.Certificate
}

// cachedCAKeyPair loads the CA key pair name from the cache, checking the
// parts that make it a usable key pair: the chain in tls.crt, the key
// matching its first certificate, and ca.crt holding the chain's root.
func cachedCAKeyPair(t *testing.T, name string, root *testpki.Certificate) mintedCAKeyPair {
	t.Helper()
	s, err := secrets.LoadCachedSecret(name)
	if err != nil {
		t.Fatal(err)
	}
	if s.Type != apiv1.SecretTypeTLS {
		t.Errorf("%s type = %q, want kubernetes.io/tls", name, s.Type)
	}
	chain, err := secrets.ParseCertificatesPEM(s.Data["tls.crt"])
	if err != nil {
		t.Fatalf("%s tls.crt: %v", name, err)
	}
	if len(chain) != 2 || !chain[1].Equal(root.Cert) {
		t.Fatalf("%s tls.crt holds %d certificates, want the CA followed by the root", name, len(chain))
	}
	caCrt, err := secrets.ParseCertificatesPEM(s.Data["ca.crt"])
	if err != nil {
		t.Fatalf("%s ca.crt: %v", name, err)
	}
	if len(caCrt) != 1 || !caCrt[0].Equal(root.Cert) {
		t.Errorf("%s ca.crt is not the root certificate", name)
	}
	key, err := secrets.ParsePrivateKeyPEM(s.Data["tls.key"])
	if err != nil {
		t.Fatalf("%s tls.key: %v", name, err)
	}
	if !secrets.KeyMatchesCertificate(chain[0], key) {
		t.Errorf("%s tls.key does not match its CA certificate", name)
	}
	return mintedCAKeyPair{
		secret: s,
		ca:     &testpki.Certificate{Cert: chain[0], Key: key, Issuer: root},
		chain:  chain,
	}
}

// verifies reports whether leaf verifies to root through the minted CA,
// as a TLS client checking a server certificate for dnsName does.
func verifies(leaf *testpki.Certificate, minted mintedCAKeyPair, root *testpki.Certificate, dnsName string) error {
	roots := x509.NewCertPool()
	roots.AddCert(root.Cert)
	intermediates := x509.NewCertPool()
	intermediates.AddCert(minted.chain[0])
	_, err := leaf.Cert.Verify(x509.VerifyOptions{
		Roots:         roots,
		Intermediates: intermediates,
		DNSName:       dnsName,
	})
	return err
}

func TestResolve_Insecure_NoneProviderOverHTTP(t *testing.T) {
	// No CA is cached: an insecure install needs none.
	withDataHome(t)
	cfg := localConfig("workshop.test", true)

	plan, err := localcerts.Resolve(cfg)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if got, want := plan.Certificates.Provider, translator.CertificatesProviderNone; got != want {
		t.Errorf("plan provider = %q, want %q", got, want)
	}

	ingress := translatedIngress(t, cfg, plan)
	certs := ingress["certificates"].(map[string]interface{})
	if got, want := certs["provider"], "None"; got != want {
		t.Errorf("certificates.provider = %v, want %v", got, want)
	}
	if _, present := certs["bundledCertManager"]; present {
		t.Errorf("bundledCertManager present for an insecure install: %v", certs)
	}
	if got, want := ingress["protocol"], "http"; got != want {
		t.Errorf("ingress.protocol = %v, want %v", got, want)
	}
}

func TestResolve_Root_SelectsTheDomainCAKeyPairAndTheRootTrust(t *testing.T) {
	withDataHome(t)
	cacheRoot(t, "educates-ca", testpki.NewRootCA())
	cfg := localConfig("workshop.test", false)

	plan, err := localcerts.Resolve(cfg)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	want := translator.LocalCertificates{
		Provider: "BundledCertManager",
		CustomCA: &translator.LocalCustomCA{
			CAKeyPairSecretRef:     translator.SecretRef{Name: "workshop.test-ca-key-pair", Namespace: "educates-secrets"},
			CACertificateSecretRef: &translator.SecretRef{Name: "educates-ca", Namespace: "educates-secrets"},
		},
	}
	if !reflect.DeepEqual(plan.Certificates, want) {
		t.Errorf("plan certificates = %+v, want %+v", plan.Certificates, want)
	}

	ingress := translatedIngress(t, cfg, plan)
	for path, want := range map[string]interface{}{
		"certificates.provider":                                                     "BundledCertManager",
		"certificates.bundledCertManager.issuerType":                                "CustomCA",
		"certificates.bundledCertManager.customCA.caKeyPairSecretRef.name":          "workshop.test-ca-key-pair",
		"certificates.bundledCertManager.customCA.caKeyPairSecretRef.namespace":     "educates-secrets",
		"certificates.bundledCertManager.customCA.caCertificateSecretRef.name":      "educates-ca",
		"certificates.bundledCertManager.customCA.caCertificateSecretRef.namespace": "educates-secrets",
	} {
		if got := nested(t, ingress, path); got != want {
			t.Errorf("ingress.%s = %v, want %v", path, got, want)
		}
	}
	if _, present := ingress["protocol"]; present {
		t.Errorf("ingress.protocol set for a secure install: %v", ingress["protocol"])
	}
}

func TestResolve_NewDomain_MintsAConstrainedCAKeyPairOnCommit(t *testing.T) {
	dataHome := withDataHome(t)
	root := testpki.NewRootCA(testpki.WithValidity(time.Now().Add(-time.Hour), time.Now().AddDate(2, 0, 0)))
	cacheRoot(t, "educates-ca", root)
	before := cacheFiles(t, dataHome)

	plan, err := localcerts.Resolve(localConfig("workshop.test", false))
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if after := cacheFiles(t, dataHome); !reflect.DeepEqual(after, before) {
		t.Fatalf("Resolve wrote to the cache: %v", keys(after))
	}

	var r noteRecorder
	if err := plan.Commit(&r); err != nil {
		t.Fatalf("Commit: %v", err)
	}
	if len(r.notes) != 1 || !strings.Contains(r.notes[0], `"workshop.test-ca-key-pair"`) || !strings.Contains(r.notes[0], `"educates-ca"`) {
		t.Errorf("Commit printed %q, want one line naming the minted CA key pair and its root", r.notes)
	}

	minted := cachedCAKeyPair(t, "workshop.test-ca-key-pair", root)
	if got, want := minted.secret.Annotations, map[string]string{
		"training.educates.dev/domain":  "workshop.test",
		"training.educates.dev/root-ca": "educates-ca",
	}; !reflect.DeepEqual(got, want) {
		t.Errorf("annotations = %v, want %v", got, want)
	}

	ca := minted.chain[0]
	if err := ca.CheckSignatureFrom(root.Cert); err != nil {
		t.Errorf("CA is not signed by the root: %v", err)
	}
	if !ca.BasicConstraintsValid || !ca.IsCA || ca.MaxPathLen != 0 || !ca.MaxPathLenZero {
		t.Errorf("basic constraints: CA=%v maxPathLen=%d zero=%v, want a CA with path length 0", ca.IsCA, ca.MaxPathLen, ca.MaxPathLenZero)
	}
	if ca.KeyUsage&x509.KeyUsageCertSign == 0 {
		t.Error("CA cannot sign certificates")
	}
	if !ca.PermittedDNSDomainsCritical || !reflect.DeepEqual(ca.PermittedDNSDomains, []string{"workshop.test"}) {
		t.Errorf("name constraint permits %v (critical %v), want only workshop.test, critical", ca.PermittedDNSDomains, ca.PermittedDNSDomainsCritical)
	}
	var excluded []string
	for _, r := range ca.ExcludedIPRanges {
		excluded = append(excluded, r.String())
	}
	if want := []string{"0.0.0.0/0", "::/0"}; !reflect.DeepEqual(excluded, want) {
		t.Errorf("excluded IP ranges = %v, want %v", excluded, want)
	}
	if ca.Subject.CommonName != "workshop.test" {
		t.Errorf("subject common name = %q, want the domain", ca.Subject.CommonName)
	}
	if !ca.NotAfter.Equal(root.Cert.NotAfter) {
		t.Errorf("CA expires %v, want the root's expiry %v", ca.NotAfter, root.Cert.NotAfter)
	}
	if ca.NotBefore.After(time.Now()) {
		t.Errorf("CA is not valid yet: not before %v", ca.NotBefore)
	}

	// Clients accept what the CA signs for the domain and reject anything
	// else, and it cannot sign another CA.
	for _, name := range []string{"*.workshop.test", "workshop.test"} {
		leaf := minted.ca.NewLeaf(testpki.WithDNSNames(name))
		if err := verifies(leaf, minted, root, strings.Replace(name, "*", "portal", 1)); err != nil {
			t.Errorf("leaf for %s does not verify: %v", name, err)
		}
	}
	if err := verifies(minted.ca.NewLeaf(testpki.WithDNSNames("*.other.test")), minted, root, "portal.other.test"); err == nil {
		t.Error("leaf for *.other.test verifies; the name constraint is not enforced")
	}
	ipLeaf := minted.ca.NewLeaf(testpki.WithDNSNames("*.workshop.test"), testpki.WithIPAddresses(net.ParseIP("10.0.0.1")))
	if err := verifies(ipLeaf, minted, root, "portal.workshop.test"); err == nil {
		t.Error("leaf with an IP address verifies; IP ranges are not excluded")
	}
	sub := minted.ca.NewIntermediateCA()
	subLeaf := sub.NewLeaf(testpki.WithDNSNames("*.workshop.test"))
	pool := x509.NewCertPool()
	pool.AddCert(root.Cert)
	inter := x509.NewCertPool()
	inter.AddCert(ca)
	inter.AddCert(sub.Cert)
	if _, err := subLeaf.Cert.Verify(x509.VerifyOptions{Roots: pool, Intermediates: inter, DNSName: "portal.workshop.test"}); err == nil {
		t.Error("a CA signed by the minted CA verifies; path length 0 is not enforced")
	}
}

func TestResolve_CachedCAKeyPair_ReusedWithoutMinting(t *testing.T) {
	dataHome := withDataHome(t)
	cacheRoot(t, "educates-ca", testpki.NewRootCA())
	cfg := localConfig("workshop.test", false)
	resolveAndCommit(t, cfg)
	before := cacheFiles(t, dataHome)

	plan, notes := resolveAndCommit(t, cfg)
	if len(notes) != 0 {
		t.Errorf("second deploy printed %q, want nothing", notes)
	}
	if after := cacheFiles(t, dataHome); !reflect.DeepEqual(after, before) {
		t.Error("second deploy changed the cache")
	}
	if got := plan.Certificates.CustomCA.CAKeyPairSecretRef.Name; got != "workshop.test-ca-key-pair" {
		t.Errorf("plan selects %q, want workshop.test-ca-key-pair", got)
	}
}

func TestResolve_StaleCAKeyPair_Reminted(t *testing.T) {
	cases := map[string]struct {
		// stage replaces the cached root or CA key pair after a first
		// deploy and returns the root the CA key pair must come from.
		stage    func(t *testing.T, root *testpki.Certificate) (rootName string, newRoot *testpki.Certificate)
		wantNote string
	}{
		"root replaced under the same name": {
			stage: func(t *testing.T, _ *testpki.Certificate) (string, *testpki.Certificate) {
				removeCached(t, "educates-ca", "educates-ca-root-key-pair")
				replacement := testpki.NewRootCA()
				cacheRoot(t, "educates-ca", replacement)
				return "educates-ca", replacement
			},
			wantNote: "another root CA",
		},
		"root replaced by another root": {
			stage: func(t *testing.T, _ *testpki.Certificate) (string, *testpki.Certificate) {
				removeCached(t, "educates-ca", "educates-ca-root-key-pair")
				replacement := testpki.NewRootCA()
				cacheRoot(t, "mkcert-ca", replacement)
				return "mkcert-ca", replacement
			},
			wantNote: "another root CA",
		},
		"CA key pair expired": {
			stage: func(t *testing.T, root *testpki.Certificate) (string, *testpki.Certificate) {
				expired := root.NewIntermediateCA(
					testpki.WithCommonName("workshop.test"),
					testpki.WithPermittedDNSDomains("workshop.test"),
					testpki.WithMaxPathLen(0),
					testpki.WithValidity(time.Now().AddDate(0, -2, 0), time.Now().AddDate(0, -1, 0)),
				)
				writeCached(t, secrets.NewCAKeyPairSecret("workshop.test", "educates-ca", expired.CertPEM, expired.KeyPEM, root.CertPEM))
				return "educates-ca", root
			},
			wantNote: "expired",
		},
		"CA key pair unreadable": {
			stage: func(t *testing.T, root *testpki.Certificate) (string, *testpki.Certificate) {
				writeCached(t, &apiv1.Secret{
					ObjectMeta: metav1.ObjectMeta{Name: "workshop.test-ca-key-pair"},
					Type:       apiv1.SecretTypeTLS,
					Data:       map[string][]byte{"tls.crt": []byte("not a certificate"), "tls.key": []byte("not a key")},
				})
				return "educates-ca", root
			},
			wantNote: "not a valid CA key pair",
		},
		"CA key pair not limited to the domain": {
			stage: func(t *testing.T, root *testpki.Certificate) (string, *testpki.Certificate) {
				unconstrained := root.NewIntermediateCA(testpki.WithMaxPathLen(0))
				writeCached(t, secrets.NewCAKeyPairSecret("workshop.test", "educates-ca", unconstrained.CertPEM, unconstrained.KeyPEM, root.CertPEM))
				return "educates-ca", root
			},
			wantNote: "not limited to",
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			withDataHome(t)
			root := testpki.NewRootCA()
			cacheRoot(t, "educates-ca", root)
			cfg := localConfig("workshop.test", false)
			resolveAndCommit(t, cfg)

			rootName, wantRoot := tc.stage(t, root)

			plan, notes := resolveAndCommit(t, cfg)
			if len(notes) != 1 || !strings.Contains(notes[0], `"workshop.test-ca-key-pair"`) || !strings.Contains(notes[0], tc.wantNote) {
				t.Errorf("Commit printed %q, want one line re-minting the CA key pair because it is %s", notes, tc.wantNote)
			}
			minted := cachedCAKeyPair(t, "workshop.test-ca-key-pair", wantRoot)
			if err := minted.chain[0].CheckSignatureFrom(wantRoot.Cert); err != nil {
				t.Errorf("re-minted CA is not signed by the current root: %v", err)
			}
			if time.Now().After(minted.chain[0].NotAfter) {
				t.Error("re-minted CA has expired")
			}
			if got := minted.secret.Annotations["training.educates.dev/root-ca"]; got != rootName {
				t.Errorf("root-ca annotation = %q, want %q", got, rootName)
			}
			if got := plan.Certificates.CustomCA.CACertificateSecretRef; got == nil || got.Name != rootName {
				t.Errorf("caCertificateSecretRef = %+v, want %q", got, rootName)
			}
		})
	}
}

func TestResolve_ConstrainedRoot(t *testing.T) {
	t.Run("permitting the domain mints from it", func(t *testing.T) {
		withDataHome(t)
		root := testpki.NewRootCA(testpki.WithPermittedDNSDomains("test"))
		cacheRoot(t, "corporate-ca", root)

		resolveAndCommit(t, localConfig("workshop.test", false))

		minted := cachedCAKeyPair(t, "workshop.test-ca-key-pair", root)
		leaf := minted.ca.NewLeaf(testpki.WithDNSNames("*.workshop.test"))
		if err := verifies(leaf, minted, root, "portal.workshop.test"); err != nil {
			t.Errorf("leaf does not verify through the constrained root: %v", err)
		}
	})

	for name, root := range map[string]*testpki.Certificate{
		"permitting only another domain": testpki.NewRootCA(testpki.WithPermittedDNSDomains("other.test")),
		"excluding the domain":           testpki.NewRootCA(testpki.WithExcludedDNSDomains("workshop.test")),
	} {
		t.Run(name+" fails", func(t *testing.T) {
			dataHome := withDataHome(t)
			cacheRoot(t, "corporate-ca", root)
			before := cacheFiles(t, dataHome)

			_, err := localcerts.Resolve(localConfig("workshop.test", false))
			if err == nil {
				t.Fatal("Resolve minted a CA the root's name constraints exclude")
			}
			for _, want := range []string{`root CA "corporate-ca"`, `"workshop.test"`, "name constraints"} {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("error does not mention %q:\n%v", want, err)
				}
			}
			if after := cacheFiles(t, dataHome); !reflect.DeepEqual(after, before) {
				t.Error("a refused mint changed the cache")
			}
		})
	}
}

func TestResolve_NoRoot_ErrorsWithRemediation(t *testing.T) {
	cases := map[string]func(t *testing.T){
		"empty cache": func(t *testing.T) {},
		"CA certificate without a key": func(t *testing.T) {
			writeCached(t, secrets.NewCATrustSecret("corporate-ca", testpki.NewRootCA().CertPEM))
		},
		"CA key pair for the domain without its root": func(t *testing.T) {
			root := testpki.NewRootCA()
			ca := root.NewIntermediateCA(testpki.WithPermittedDNSDomains("workshop.test"), testpki.WithMaxPathLen(0))
			writeCached(t, secrets.NewCAKeyPairSecret("workshop.test", "educates-ca", ca.CertPEM, ca.KeyPEM, root.CertPEM))
		},
		"CA cached by an alpha release": func(t *testing.T) {
			ca := testpki.NewRootCA()
			writeCached(t, withDomainAnnotation(secrets.NewTLSSecret("workshop.test-ca", ca.CertPEM, ca.KeyPEM), "workshop.test"))
		},
		"TLS certificate for another domain": func(t *testing.T) {
			cacheTLS(t, "other-tls", wildcardLeaf(testpki.NewRootCA(), "other.test", "sub.workshop.test"))
		},
		"TLS certificate annotated with the domain but without its wildcard": func(t *testing.T) {
			leaf := testpki.NewRootCA().NewLeaf(testpki.WithDNSNames("workshop.test", "portal.workshop.test"))
			writeCached(t, withDomainAnnotation(secrets.NewTLSSecret("workshop.test-tls", leaf.CertPEM, leaf.KeyPEM), "workshop.test"))
		},
	}
	for name, stage := range cases {
		t.Run(name, func(t *testing.T) {
			withDataHome(t)
			stage(t)

			_, err := localcerts.Resolve(localConfig("workshop.test", false))
			if err == nil {
				t.Fatal("Resolve succeeded for a secure install with no root CA")
			}
			for _, want := range []string{
				`"workshop.test"`,
				"educates local secrets add ca <name>\n",
				`--cert "$(mkcert -CAROOT)/rootCA.pem" --key "$(mkcert -CAROOT)/rootCA-key.pem"`,
				`mkcert -cert-file workshop.test.pem -key-file workshop.test-key.pem "*.workshop.test"`,
				"educates local secrets add tls workshop.test-tls --cert workshop.test.pem --key workshop.test-key.pem",
				`educates local secrets add ca mkcert-ca --cert "$(mkcert -CAROOT)/rootCA.pem"` + "\n",
				"installs no\ncert-manager",
				"educates local config set ingress.insecure true",
			} {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("error does not mention %q:\n%v", want, err)
				}
			}
		})
	}
}

func TestResolve_MoreThanOneRoot_ErrorsNamingThem(t *testing.T) {
	withDataHome(t)
	cacheRoot(t, "educates-ca", testpki.NewRootCA())
	cacheRoot(t, "mkcert-ca", testpki.NewRootCA())

	_, err := localcerts.Resolve(localConfig("workshop.test", false))
	if err == nil {
		t.Fatal("Resolve chose between two root CAs")
	}
	for _, want := range []string{`"educates-ca"`, `"mkcert-ca"`, "one root CA"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error does not mention %q:\n%v", want, err)
		}
	}
}

func TestResolve_MoreThanOneRoot_ErrorsBeforeSelectingAWildcardCertificate(t *testing.T) {
	withDataHome(t)
	mkcert := testpki.NewRootCA()
	cacheRoot(t, "educates-ca", testpki.NewRootCA())
	cacheRoot(t, "mkcert-ca", mkcert)
	cacheTLS(t, "workshop-tls", wildcardLeaf(mkcert, "workshop.test"))

	_, err := localcerts.Resolve(localConfig("workshop.test", false))
	if err == nil {
		t.Fatal("Resolve served a wildcard certificate from a cache holding two root CAs")
	}
	for _, want := range []string{`"educates-ca"`, `"mkcert-ca"`, "one root CA"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error does not mention %q:\n%v", want, err)
		}
	}
}

func TestResolve_UnusableRoot_Errors(t *testing.T) {
	root := testpki.NewRootCA()
	cases := map[string]struct {
		stage func(t *testing.T)
		want  string
	}{
		"key does not match": {
			stage: func(t *testing.T) {
				writeCached(t, secrets.NewCATrustSecret("educates-ca", root.CertPEM))
				writeCached(t, secrets.NewCARootKeyPairSecret("educates-ca", root.CertPEM, testpki.NewRootCA().KeyPEM))
			},
			want: "does not match",
		},
		"expired": {
			stage: func(t *testing.T) {
				cacheRoot(t, "educates-ca", testpki.NewRootCA(testpki.WithValidity(time.Now().AddDate(-2, 0, 0), time.Now().AddDate(-1, 0, 0))))
			},
			want: "expired",
		},
		"CA trust Secret missing": {
			stage: func(t *testing.T) {
				writeCached(t, secrets.NewCARootKeyPairSecret("educates-ca", root.CertPEM, root.KeyPEM))
			},
			want: `CA trust Secret "educates-ca"`,
		},
		"CA trust Secret holds another certificate": {
			stage: func(t *testing.T) {
				writeCached(t, secrets.NewCATrustSecret("educates-ca", testpki.NewRootCA().CertPEM))
				writeCached(t, secrets.NewCARootKeyPairSecret("educates-ca", root.CertPEM, root.KeyPEM))
			},
			want: `CA trust Secret "educates-ca"`,
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			withDataHome(t)
			tc.stage(t)

			_, err := localcerts.Resolve(localConfig("workshop.test", false))
			if err == nil {
				t.Fatal("Resolve accepted an unusable root CA")
			}
			if !strings.Contains(err.Error(), "educates-ca") || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error does not name the root and mention %q:\n%v", tc.want, err)
			}
		})
	}
}

// cacheTLS caches leaf, with the chain it carries, as TLS certificate
// name, as `educates local secrets add tls NAME --cert --key` does.
func cacheTLS(t *testing.T, name string, leaf *testpki.Certificate, chain ...*testpki.Certificate) {
	t.Helper()
	certPEM := append([]byte{}, leaf.CertPEM...)
	for _, c := range chain {
		certPEM = append(certPEM, c.CertPEM...)
	}
	writeCached(t, secrets.NewTLSSecret(name, certPEM, leaf.KeyPEM))
}

// wildcardLeaf returns a server certificate signed by ca for the
// wildcard of each domain.
func wildcardLeaf(ca *testpki.Certificate, domains ...string) *testpki.Certificate {
	var names []string
	for _, d := range domains {
		names = append(names, "*."+d)
	}
	return ca.NewLeaf(testpki.WithDNSNames(names...))
}

func staticCertificate(tls, ca string) translator.LocalCertificates {
	static := &translator.LocalStaticCertificate{
		TLSSecretRef: translator.SecretRef{Name: tls, Namespace: "educates-secrets"},
	}
	if ca != "" {
		static.CACertificateSecretRef = &translator.SecretRef{Name: ca, Namespace: "educates-secrets"}
	}
	return translator.LocalCertificates{Provider: "StaticCertificate", StaticCertificate: static}
}

func TestResolve_OneTLSCertificate_SelectsStaticCertificateWithTheCAThatVerifiesIt(t *testing.T) {
	dataHome := withDataHome(t)
	mkcert := testpki.NewRootCA(testpki.WithCommonName("mkcert development CA"))
	cacheTLS(t, "workshop-tls", wildcardLeaf(mkcert, "workshop.test"))
	writeCached(t, secrets.NewCATrustSecret("corporate-ca", testpki.NewRootCA().CertPEM))
	writeCached(t, secrets.NewCATrustSecret("mkcert-ca", mkcert.CertPEM))
	before := cacheFiles(t, dataHome)
	cfg := localConfig("workshop.test", false)

	plan, err := localcerts.Resolve(cfg)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if want := staticCertificate("workshop-tls", "mkcert-ca"); !reflect.DeepEqual(plan.Certificates, want) {
		t.Errorf("plan certificates = %+v, want %+v", plan.Certificates, want)
	}
	if want := []string{"corporate-ca", "mkcert-ca", "workshop-tls"}; !reflect.DeepEqual(plan.SyncSecretNames, want) {
		t.Errorf("plan syncs %v, want %v", plan.SyncSecretNames, want)
	}
	if len(plan.Warnings) != 0 {
		t.Errorf("plan warns %q, want no warning", plan.Warnings)
	}

	ingress := translatedIngress(t, cfg, plan)
	for path, want := range map[string]interface{}{
		"certificates.provider":                                           "StaticCertificate",
		"certificates.staticCertificate.tlsSecretRef.name":                "workshop-tls",
		"certificates.staticCertificate.tlsSecretRef.namespace":           "educates-secrets",
		"certificates.staticCertificate.caCertificateSecretRef.name":      "mkcert-ca",
		"certificates.staticCertificate.caCertificateSecretRef.namespace": "educates-secrets",
	} {
		if got := nested(t, ingress, path); got != want {
			t.Errorf("ingress.%s = %v, want %v", path, got, want)
		}
	}

	var r noteRecorder
	if err := plan.Commit(&r); err != nil {
		t.Fatalf("Commit: %v", err)
	}
	if after := cacheFiles(t, dataHome); !reflect.DeepEqual(after, before) {
		t.Error("deploying with a TLS certificate changed the cache")
	}
}

func TestResolve_TLSCertificate_NoticeNamesItAndTheOtherDomainsItCovers(t *testing.T) {
	withDataHome(t)
	mkcert := testpki.NewRootCA()
	writeCached(t, secrets.NewCATrustSecret("mkcert-ca", mkcert.CertPEM))
	cacheTLS(t, "projects-tls", mkcert.NewLeaf(testpki.WithDNSNames(
		"*.workshop.test", "workshop.test", "*.Other.test", "*.third.test.", "portal.fourth.test")))
	cacheTLS(t, "single-tls", wildcardLeaf(mkcert, "single.test"))

	for domain, wantOthers := range map[string]string{
		"workshop.test": "other.test, third.test",
		"other.test":    "third.test, workshop.test",
	} {
		t.Run(domain, func(t *testing.T) {
			plan, err := localcerts.Resolve(localConfig(domain, false))
			if err != nil {
				t.Fatalf("Resolve: %v", err)
			}
			if want := staticCertificate("projects-tls", "mkcert-ca"); !reflect.DeepEqual(plan.Certificates, want) {
				t.Errorf("plan certificates = %+v, want %+v", plan.Certificates, want)
			}
			if len(plan.Notices) != 1 {
				t.Fatalf("plan notices = %q, want one", plan.Notices)
			}
			notice := plan.Notices[0]
			for _, want := range []string{
				`TLS certificate "projects-tls"`,
				fmt.Sprintf("%q", domain),
				"cert-manager will not be installed",
				"educates local secrets remove projects-tls",
				"other domains, which removing it affects too: " + wantOthers + ".",
			} {
				if !strings.Contains(notice, want) {
					t.Errorf("notice does not mention %q:\n%s", want, notice)
				}
			}
		})
	}

	t.Run("a certificate for one domain", func(t *testing.T) {
		plan, err := localcerts.Resolve(localConfig("single.test", false))
		if err != nil {
			t.Fatalf("Resolve: %v", err)
		}
		if len(plan.Notices) != 1 || strings.Contains(plan.Notices[0], "other domains") {
			t.Errorf("plan notices = %q, want one naming no other domain", plan.Notices)
		}
	})
}

func TestResolve_TLSCertificateBesideARoot_ServedWithoutMinting(t *testing.T) {
	dataHome := withDataHome(t)
	mkcert := testpki.NewRootCA()
	cacheRoot(t, "mkcert-ca", mkcert)
	cacheTLS(t, "workshop-tls", wildcardLeaf(mkcert, "workshop.test"))
	before := cacheFiles(t, dataHome)

	plan, notes := resolveAndCommit(t, localConfig("workshop.test", false))

	if want := staticCertificate("workshop-tls", "mkcert-ca"); !reflect.DeepEqual(plan.Certificates, want) {
		t.Errorf("plan certificates = %+v, want %+v", plan.Certificates, want)
	}
	if want := []string{"mkcert-ca", "workshop-tls"}; !reflect.DeepEqual(plan.SyncSecretNames, want) {
		t.Errorf("plan syncs %v, want %v", plan.SyncSecretNames, want)
	}
	if after := cacheFiles(t, dataHome); !reflect.DeepEqual(after, before) {
		t.Errorf("deploy changed the cache: %v", keys(after))
	}
	if len(notes) != 1 || !strings.Contains(notes[0], "cert-manager will not be installed") {
		t.Errorf("Commit printed %q, want only the StaticCertificate notice", notes)
	}
}

func TestResolve_MoreThanOneTLSCertificate_ErrorsNamingThem(t *testing.T) {
	withDataHome(t)
	mkcert := testpki.NewRootCA()
	cacheRoot(t, "mkcert-ca", mkcert)
	cacheTLS(t, "workshop-tls", wildcardLeaf(mkcert, "workshop.test"))
	cacheTLS(t, "projects-tls", wildcardLeaf(mkcert, "other.test", "workshop.test"))
	cacheTLS(t, "other-tls", wildcardLeaf(mkcert, "other.test"))
	cacheTLS(t, "expired-tls", mkcert.NewLeaf(testpki.WithDNSNames("*.workshop.test"),
		testpki.WithValidity(time.Now().AddDate(0, -2, 0), time.Now().AddDate(0, -1, 0))))

	_, err := localcerts.Resolve(localConfig("workshop.test", false))
	if err == nil {
		t.Fatal("Resolve chose between two TLS certificates for the domain")
	}
	for _, want := range []string{`"projects-tls", "workshop-tls"`, `"workshop.test"`, "educates local secrets remove NAME"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error does not mention %q:\n%v", want, err)
		}
	}
	for _, unwanted := range []string{"other-tls", "expired-tls"} {
		if strings.Contains(err.Error(), unwanted) {
			t.Errorf("error names %q, which does not count:\n%v", unwanted, err)
		}
	}
}

func TestResolve_ExpiredTLSCertificate_SkippedWithANotice(t *testing.T) {
	mkcert := testpki.NewRootCA()
	expired := mkcert.NewLeaf(testpki.WithDNSNames("*.workshop.test"),
		testpki.WithValidity(time.Now().AddDate(0, -2, 0), time.Now().AddDate(0, -1, 0)))
	expiredOn := expired.Cert.NotAfter.UTC().Format(time.DateOnly)
	wantNotice := `Skipped TLS certificate "old-tls" for ingress domain "workshop.test": it expired on ` + expiredOn + "."

	t.Run("beside a valid one", func(t *testing.T) {
		withDataHome(t)
		writeCached(t, secrets.NewCATrustSecret("mkcert-ca", mkcert.CertPEM))
		cacheTLS(t, "old-tls", expired)
		cacheTLS(t, "workshop-tls", wildcardLeaf(mkcert, "workshop.test"))

		plan, err := localcerts.Resolve(localConfig("workshop.test", false))
		if err != nil {
			t.Fatalf("Resolve: %v", err)
		}
		if want := staticCertificate("workshop-tls", "mkcert-ca"); !reflect.DeepEqual(plan.Certificates, want) {
			t.Errorf("plan certificates = %+v, want %+v", plan.Certificates, want)
		}
		if len(plan.Notices) != 2 || plan.Notices[0] != wantNotice || !strings.Contains(plan.Notices[1], `"workshop-tls"`) {
			t.Errorf("plan notices = %q, want the skipped certificate and then the one served", plan.Notices)
		}
	})

	t.Run("beside a root", func(t *testing.T) {
		withDataHome(t)
		cacheRoot(t, "educates-ca", testpki.NewRootCA())
		cacheTLS(t, "old-tls", expired)

		plan, err := localcerts.Resolve(localConfig("workshop.test", false))
		if err != nil {
			t.Fatalf("Resolve: %v", err)
		}
		if got := plan.Certificates.Provider; got != "BundledCertManager" {
			t.Errorf("plan provider = %q, want BundledCertManager", got)
		}
		if !reflect.DeepEqual(plan.Notices, []string{wantNotice}) {
			t.Errorf("plan notices = %q, want %q", plan.Notices, wantNotice)
		}
	})

	t.Run("alone", func(t *testing.T) {
		withDataHome(t)
		cacheTLS(t, "old-tls", expired)

		_, err := localcerts.Resolve(localConfig("workshop.test", false))
		if err == nil {
			t.Fatal("Resolve served an expired TLS certificate")
		}
		for _, want := range []string{`have expired: "old-tls"`, "educates local secrets add tls", "educates local secrets add ca <name>\n"} {
			if !strings.Contains(err.Error(), want) {
				t.Errorf("error does not mention %q:\n%v", want, err)
			}
		}
	})
}

func TestResolve_TLSCertificate_CATrust(t *testing.T) {
	mkcert := testpki.NewRootCA()
	intermediate := mkcert.NewIntermediateCA()
	otherDomainCA := mkcert.NewIntermediateCA(testpki.WithPermittedDNSDomains("other.test"))

	cases := map[string]struct {
		stage  func(t *testing.T)
		wantCA string
	}{
		"verified through the intermediates tls.crt carries": {
			stage: func(t *testing.T) {
				cacheTLS(t, "workshop-tls", wildcardLeaf(intermediate, "workshop.test"), intermediate)
				writeCached(t, secrets.NewCATrustSecret("mkcert-ca", mkcert.CertPEM))
			},
			wantCA: "mkcert-ca",
		},
		"first of two CA certificates that verify it": {
			stage: func(t *testing.T) {
				cacheTLS(t, "workshop-tls", wildcardLeaf(mkcert, "workshop.test"))
				writeCached(t, secrets.NewCATrustSecret("mkcert-ca", mkcert.CertPEM))
				writeCached(t, secrets.NewCATrustSecret("a-copy-ca", mkcert.CertPEM))
			},
			wantCA: "a-copy-ca",
		},
		"CA bundle holding the issuer": {
			stage: func(t *testing.T) {
				cacheTLS(t, "workshop-tls", wildcardLeaf(mkcert, "workshop.test"))
				bundle := append(append([]byte{}, testpki.NewRootCA().CertPEM...), mkcert.CertPEM...)
				writeCached(t, secrets.NewCATrustSecret("bundle-ca", bundle))
			},
			wantCA: "bundle-ca",
		},
		"no CA certificate cached": {
			stage: func(t *testing.T) {
				cacheTLS(t, "workshop-tls", wildcardLeaf(mkcert, "workshop.test"))
			},
		},
		"only an unrelated CA certificate": {
			stage: func(t *testing.T) {
				cacheTLS(t, "workshop-tls", wildcardLeaf(mkcert, "workshop.test"))
				writeCached(t, secrets.NewCATrustSecret("corporate-ca", testpki.NewRootCA().CertPEM))
			},
		},
		"an issuer whose name constraints exclude the domain": {
			stage: func(t *testing.T) {
				cacheTLS(t, "workshop-tls", wildcardLeaf(otherDomainCA, "workshop.test"), otherDomainCA)
				writeCached(t, secrets.NewCATrustSecret("mkcert-ca", mkcert.CertPEM))
			},
		},
		// Only what `local secrets list` shows as a CA certificate is CA
		// trust; a TLS certificate's ca.crt is not.
		"only a TLS certificate for another domain carrying the CA as ca.crt": {
			stage: func(t *testing.T) {
				cacheTLS(t, "workshop-tls", wildcardLeaf(mkcert, "workshop.test"))
				other := wildcardLeaf(mkcert, "other.test")
				otherTLS := secrets.NewTLSSecret("other-tls", other.CertPEM, other.KeyPEM)
				otherTLS.Data["ca.crt"] = mkcert.CertPEM
				writeCached(t, otherTLS)
			},
		},
		// A CA key pair is synced only when a deploy selects it, so it
		// cannot be the CA trust of a StaticCertificate.
		"only a CA key pair holding the CA": {
			stage: func(t *testing.T) {
				cacheTLS(t, "workshop-tls", wildcardLeaf(intermediate, "workshop.test"), intermediate)
				writeCached(t, secrets.NewCAKeyPairSecret("workshop.test", "mkcert-ca", intermediate.CertPEM, intermediate.KeyPEM, mkcert.CertPEM))
			},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			withDataHome(t)
			tc.stage(t)
			cfg := localConfig("workshop.test", false)

			plan, err := localcerts.Resolve(cfg)
			if err != nil {
				t.Fatalf("Resolve: %v", err)
			}
			if want := staticCertificate("workshop-tls", tc.wantCA); !reflect.DeepEqual(plan.Certificates, want) {
				t.Errorf("plan certificates = %+v, want %+v", plan.Certificates, want)
			}
			if tc.wantCA != "" {
				if len(plan.Warnings) != 0 {
					t.Errorf("plan warns %q, want no warning", plan.Warnings)
				}
				return
			}
			if len(plan.Warnings) != 1 || !strings.Contains(plan.Warnings[0], `"workshop-tls"`) ||
				!strings.Contains(plan.Warnings[0], `educates local secrets add ca mkcert-ca --cert "$(mkcert -CAROOT)/rootCA.pem"`) {
				t.Errorf("plan warns %q, want one naming the certificate and how to register its CA", plan.Warnings)
			}
			certs := nested(t, translatedIngress(t, cfg, plan), "certificates.staticCertificate").(map[string]interface{})
			if _, present := certs["caCertificateSecretRef"]; present {
				t.Errorf("caCertificateSecretRef emitted with no CA certificate that verifies the certificate: %v", certs)
			}
		})
	}
}

func TestResolve_TLSCertificateWithAMismatchedKey_Errors(t *testing.T) {
	withDataHome(t)
	mkcert := testpki.NewRootCA()
	cacheRoot(t, "mkcert-ca", mkcert)
	leaf := wildcardLeaf(mkcert, "workshop.test")
	writeCached(t, secrets.NewTLSSecret("workshop-tls", leaf.CertPEM, wildcardLeaf(mkcert, "workshop.test").KeyPEM))

	_, err := localcerts.Resolve(localConfig("workshop.test", false))
	if err == nil {
		t.Fatal("Resolve served a TLS certificate whose key does not match")
	}
	for _, want := range []string{`TLS certificate "workshop-tls"`, "private key", "educates local secrets remove workshop-tls"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error does not mention %q:\n%v", want, err)
		}
	}
}

// A v3 local secrets cache holds the wildcard certificate and the CA
// certificate that issued it, both annotated with the ingress domain.
const (
	v3CACertificateSecret = `apiVersion: v1
kind: Secret
metadata:
  name: workshop.test-ca
  annotations:
    training.educates.dev/domain: workshop.test
type: Opaque
data:
  ca.crt: %s
`
	v3TLSSecret = `apiVersion: v1
kind: Secret
metadata:
  name: workshop.test-tls
  annotations:
    training.educates.dev/domain: workshop.test
type: kubernetes.io/tls
data:
  tls.crt: %s
  tls.key: %s
`
)

func TestResolve_V3Cache_ServedWithStaticCertificateAsItIs(t *testing.T) {
	dataHome := withDataHome(t)
	mkcert := testpki.NewRootCA(testpki.WithCommonName("mkcert development CA"))
	leaf := wildcardLeaf(mkcert, "workshop.test")
	b64 := func(b []byte) string { return base64.StdEncoding.EncodeToString(b) }
	secretsDir := filepath.Join(dataHome, "secrets")
	if err := os.MkdirAll(secretsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, content := range map[string]string{
		"workshop.test-ca.yaml":  fmt.Sprintf(v3CACertificateSecret, b64(mkcert.CertPEM)),
		"workshop.test-tls.yaml": fmt.Sprintf(v3TLSSecret, b64(leaf.CertPEM), b64(leaf.KeyPEM)),
	} {
		if err := os.WriteFile(filepath.Join(secretsDir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	before := cacheFiles(t, dataHome)

	plan, notes := resolveAndCommit(t, localConfig("workshop.test", false))

	if want := staticCertificate("workshop.test-tls", "workshop.test-ca"); !reflect.DeepEqual(plan.Certificates, want) {
		t.Errorf("plan certificates = %+v, want %+v", plan.Certificates, want)
	}
	if want := []string{"workshop.test-ca", "workshop.test-tls"}; !reflect.DeepEqual(plan.SyncSecretNames, want) {
		t.Errorf("plan syncs %v, want %v", plan.SyncSecretNames, want)
	}
	if len(plan.Warnings) != 0 {
		t.Errorf("plan warns %q, want no warning", plan.Warnings)
	}
	if len(notes) != 1 || !strings.Contains(notes[0], "cert-manager will not be installed") {
		t.Errorf("Commit printed %q, want only the StaticCertificate notice", notes)
	}
	if after := cacheFiles(t, dataHome); !reflect.DeepEqual(after, before) {
		t.Errorf("deploying a v3 cache changed it: %v", keys(after))
	}
}

// stageMixedCache caches one Secret of every role: the root educates-ca,
// a CA key pair minted for other.test, a trust-only CA certificate, a TLS
// certificate, registry credentials, and a CA with its key in the shape
// alpha releases cached.
func stageMixedCache(t *testing.T) {
	t.Helper()
	root := testpki.NewRootCA()
	cacheRoot(t, "educates-ca", root)
	resolveAndCommit(t, localConfig("other.test", false))
	writeCached(t, secrets.NewCATrustSecret("corporate-ca", testpki.NewRootCA().CertPEM))
	leaf := root.NewLeaf(testpki.WithDNSNames("*.wildcard.test"))
	writeCached(t, secrets.NewTLSSecret("wildcard-tls", leaf.ChainPEM(), leaf.KeyPEM))
	writeCached(t, secrets.NewDockerRegistrySecret("registry", "registry.test", "user", "pass", ""))
	alpha := testpki.NewRootCA()
	writeCached(t, withDomainAnnotation(secrets.NewTLSSecret("workshop.test-ca", alpha.CertPEM, alpha.KeyPEM), "workshop.test"))
}

func TestResolve_SyncsOnlyTheSelectedCAKeyPair(t *testing.T) {
	for name, tc := range map[string]struct {
		insecure bool
		want     []string
	}{
		"secure": {
			want: []string{"corporate-ca", "educates-ca", "registry", "wildcard-tls", "workshop.test-ca-key-pair"},
		},
		"insecure": {
			insecure: true,
			want:     []string{"corporate-ca", "educates-ca", "registry", "wildcard-tls"},
		},
	} {
		t.Run(name, func(t *testing.T) {
			withDataHome(t)
			stageMixedCache(t)

			// The selected CA key pair is synced although only Commit
			// writes it to the cache.
			plan, err := localcerts.Resolve(localConfig("workshop.test", tc.insecure))
			if err != nil {
				t.Fatalf("Resolve: %v", err)
			}
			if !reflect.DeepEqual(plan.SyncSecretNames, tc.want) {
				t.Errorf("plan syncs %v, want %v", plan.SyncSecretNames, tc.want)
			}
		})
	}
}

func TestResolve_LeavesAnAbsentCacheAbsent(t *testing.T) {
	dataHome := withDataHome(t)

	if _, err := localcerts.Resolve(localConfig("workshop.test", true)); err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dataHome, "secrets")); !os.IsNotExist(err) {
		t.Errorf("Resolve created the secrets cache directory (stat error: %v)", err)
	}
}

func TestResolveSync_PushesOnlyTheCAKeyPairTheLocalConfigSelects(t *testing.T) {
	withoutCAKeyPairs := []string{"corporate-ca", "educates-ca", "registry", "wildcard-tls"}

	for name, tc := range map[string]struct {
		config    *v1alpha1.EducatesLocalConfig
		want      []string
		wantMints int
	}{
		"without a local config":               {want: withoutCAKeyPairs},
		"with a local config without a domain": {config: localConfig("", false), want: withoutCAKeyPairs},
		"with an insecure local config":        {config: localConfig("workshop.test", true), want: withoutCAKeyPairs},
		"with a secure local config of another domain": {
			config:    localConfig("test2.educates.test", false),
			want:      []string{"corporate-ca", "educates-ca", "registry", "test2.educates.test-ca-key-pair", "wildcard-tls"},
			wantMints: 1,
		},
		"with a secure local config": {
			config:    localConfig("workshop.test", false),
			want:      []string{"corporate-ca", "educates-ca", "registry", "wildcard-tls", "workshop.test-ca-key-pair"},
			wantMints: 1,
		},
	} {
		t.Run(name, func(t *testing.T) {
			withDataHome(t)
			stageMixedCache(t)

			plan, err := localcerts.ResolveSync(tc.config)
			if err != nil {
				t.Fatalf("ResolveSync: %v", err)
			}
			if !reflect.DeepEqual(plan.SyncSecretNames, tc.want) {
				t.Errorf("sync pushes %v, want %v", plan.SyncSecretNames, tc.want)
			}
			var r noteRecorder
			if err := plan.Commit(&r); err != nil {
				t.Fatalf("Commit: %v", err)
			}
			if len(r.notes) != tc.wantMints {
				t.Errorf("Commit printed %q, want %d mint lines", r.notes, tc.wantMints)
			}
			for _, name := range plan.SyncSecretNames {
				if _, err := secrets.LoadCachedSecret(name); err != nil {
					t.Errorf("sync pushes %q, which the cache does not hold after Commit: %v", name, err)
				}
			}
		})
	}
}

func TestResolveSync_UnresolvableCertificates_WarnsAndPushesSecretsWithoutCAKeys(t *testing.T) {
	// A secure ingress domain with neither a root CA nor a wildcard
	// certificate for it cached: a deploy fails, but a sync still pushes
	// what holds no CA key.
	withDataHome(t)
	writeCached(t, secrets.NewCATrustSecret("corporate-ca", testpki.NewRootCA().CertPEM))
	writeCached(t, secrets.NewDockerRegistrySecret("registry", "registry.test", "user", "pass", ""))
	alpha := testpki.NewRootCA()
	writeCached(t, withDomainAnnotation(secrets.NewTLSSecret("workshop.test-ca", alpha.CertPEM, alpha.KeyPEM), "workshop.test"))

	plan, err := localcerts.ResolveSync(localConfig("workshop.test", false))
	if err != nil {
		t.Fatalf("ResolveSync: %v", err)
	}
	if want := []string{"corporate-ca", "registry"}; !reflect.DeepEqual(plan.SyncSecretNames, want) {
		t.Errorf("sync pushes %v, want %v", plan.SyncSecretNames, want)
	}
	var r noteRecorder
	if err := plan.Commit(&r); err != nil {
		t.Fatalf("Commit: %v", err)
	}
	if len(r.notes) != 1 || !strings.HasPrefix(r.notes[0], "warning: ") ||
		!strings.Contains(r.notes[0], `no certificate for ingress domain "workshop.test"`) {
		t.Errorf("Commit printed %q, want one warning carrying why the certificates cannot be resolved", r.notes)
	}
}

func TestPlan_CommitAndReport_PrintNoticesThenWarnings(t *testing.T) {
	plan := &localcerts.Plan{
		Notices:  []string{"first notice", "second notice"},
		Warnings: []string{"a warning"},
	}
	want := []string{"first notice", "second notice", "warning: a warning"}

	var reported noteRecorder
	plan.Report(&reported)
	if !reflect.DeepEqual(reported.notes, want) {
		t.Errorf("Report printed %q, want %q", reported.notes, want)
	}

	var committed noteRecorder
	if err := plan.Commit(&committed); err != nil {
		t.Fatalf("Commit: %v", err)
	}
	if !reflect.DeepEqual(committed.notes, want) {
		t.Errorf("Commit printed %q, want %q", committed.notes, want)
	}
}

func keys(m map[string]string) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	return out
}
