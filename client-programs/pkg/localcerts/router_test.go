package localcerts_test

import (
	"crypto/x509"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/educates/educates-training-platform/client-programs/internal/testpki"
	"github.com/educates/educates-training-platform/client-programs/pkg/config/translator"
	"github.com/educates/educates-training-platform/client-programs/pkg/config/v1alpha1"
	"github.com/educates/educates-training-platform/client-programs/pkg/localcerts"
	"github.com/educates/educates-training-platform/client-programs/pkg/localclusters"
	"github.com/educates/educates-training-platform/client-programs/pkg/secrets"
)

// routerConfig is the config of a local cluster on domain whose TLS ends
// at the router.
func routerConfig(domain string) *v1alpha1.EducatesLocalConfig {
	cfg := localConfig(domain, false)
	cfg.Ingress.ExternalTLSTermination = true
	return cfg
}

// trustingCA is the certificates decision of a cluster whose TLS ends at
// the router, trusting the CA trust Secret ca.
func trustingCA(ca string) translator.LocalCertificates {
	return translator.LocalCertificates{
		Provider: translator.CertificatesProviderNone,
		None: &translator.LocalNoneCertificates{
			CACertificateSecretRef: &translator.SecretRef{Name: ca, Namespace: "educates-secrets"},
		},
	}
}

func TestResolve_TLSEndedAtTheRouter_TrustsTheRootAndIssuesNoCertificate(t *testing.T) {
	dataHome := withDataHome(t)
	cacheRoot(t, "educates-ca", testpki.NewRootCA())
	before := cacheFiles(t, dataHome)
	cfg := routerConfig("workshop.test")

	plan, notes := resolveAndCommit(t, cfg)

	if want := trustingCA("educates-ca"); !reflect.DeepEqual(plan.Certificates, want) {
		t.Errorf("plan certificates = %+v, want %+v", plan.Certificates, want)
	}
	if want := []string{"educates-ca"}; !reflect.DeepEqual(plan.SyncSecretNames, want) {
		t.Errorf("plan syncs %v, want %v", plan.SyncSecretNames, want)
	}
	if after := cacheFiles(t, dataHome); !reflect.DeepEqual(after, before) {
		t.Errorf("deploy changed the cache: %v", keys(after))
	}
	if len(notes) != 0 {
		t.Errorf("Commit printed %q, want nothing", notes)
	}

	ingress := translatedIngress(t, cfg, plan)
	for path, want := range map[string]interface{}{
		"certificates.provider":                         "None",
		"certificates.none.caCertificateSecretRef.name": "educates-ca",
		"protocol": "https",
	} {
		if got := nested(t, ingress, path); got != want {
			t.Errorf("ingress.%s = %v, want %v", path, got, want)
		}
	}
}

func TestResolve_TLSEndedAtTheRouter_SyncsNoCAKeyPair(t *testing.T) {
	withDataHome(t)
	cacheRoot(t, "educates-ca", testpki.NewRootCA())
	// A deploy of the domain with the cluster ending TLS itself minted its
	// CA key pair.
	resolveAndCommit(t, localConfig("workshop.test", false))

	plan, err := localcerts.Resolve(routerConfig("workshop.test"))
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}

	if want := []string{"educates-ca"}; !reflect.DeepEqual(plan.SyncSecretNames, want) {
		t.Errorf("plan syncs %v, want only the root's CA trust Secret, %v", plan.SyncSecretNames, want)
	}
}

func TestResolve_TLSEndedAtTheRouter_WithATLSCertificateTrustsTheCAThatVerifiesIt(t *testing.T) {
	dataHome := withDataHome(t)
	cacheRoot(t, "educates-ca", testpki.NewRootCA())
	mkcert := testpki.NewRootCA(testpki.WithCommonName("mkcert development CA"))
	writeCached(t, secrets.NewCATrustSecret("mkcert-ca", mkcert.CertPEM))
	cacheTLS(t, "workshop-tls", wildcardLeaf(mkcert, "workshop.test"))
	before := cacheFiles(t, dataHome)

	plan, notes := resolveAndCommit(t, routerConfig("workshop.test"))

	if want := trustingCA("mkcert-ca"); !reflect.DeepEqual(plan.Certificates, want) {
		t.Errorf("plan certificates = %+v, want %+v", plan.Certificates, want)
	}
	if want := []string{"educates-ca", "mkcert-ca", "workshop-tls"}; !reflect.DeepEqual(plan.SyncSecretNames, want) {
		t.Errorf("plan syncs %v, want %v", plan.SyncSecretNames, want)
	}
	if after := cacheFiles(t, dataHome); !reflect.DeepEqual(after, before) {
		t.Errorf("deploy changed the cache: %v", keys(after))
	}
	if len(notes) != 1 || !strings.Contains(notes[0], `"workshop-tls"`) || !strings.Contains(notes[0], `"mkcert-ca"`) ||
		!strings.Contains(notes[0], "router") {
		t.Errorf("Commit printed %q, want a notice naming the router, the TLS certificate and its CA", notes)
	}
}

func TestResolve_TLSEndedAtTheRouter_RefusesATLSCertificateNoCachedCAVerifies(t *testing.T) {
	withDataHome(t)
	cacheRoot(t, "educates-ca", testpki.NewRootCA())
	cacheTLS(t, "workshop-tls", wildcardLeaf(testpki.NewRootCA(), "workshop.test"))

	_, err := localcerts.Resolve(routerConfig("workshop.test"))

	if err == nil {
		t.Fatal("Resolve gave the cluster a CA trust that does not verify the router's certificate")
	}
	for _, want := range []string{`"workshop-tls"`, `"workshop.test"`, "educates local secrets add ca", "educates local secrets remove workshop-tls"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error does not mention %q:\n%v", want, err)
		}
	}
}

func TestResolve_TLSEndedAtTheRouter_NoRootNoTLSCertificate_Errors(t *testing.T) {
	withDataHome(t)

	_, err := localcerts.Resolve(routerConfig("workshop.test"))

	if err == nil || !strings.Contains(err.Error(), "educates local secrets add ca") {
		t.Errorf("Resolve = %v, want an error saying how to cache a root CA", err)
	}
}

// routerLeaf checks that cert is a certificate the CLI issued for the
// router for domain, as a browser trusting root sees it: its chain holds
// the leaf and the CA it was issued from, its key matches the leaf, and
// the leaf verifies to root for domain and a name under it. It returns
// the leaf.
func routerLeaf(t *testing.T, cert localclusters.RouterCertificate, domain string, root *testpki.Certificate) *x509.Certificate {
	t.Helper()
	if cert.Domain != domain {
		t.Errorf("certificate domain = %q, want %q", cert.Domain, domain)
	}
	chain, err := secrets.ParseCertificatesPEM(cert.Certificate)
	if err != nil {
		t.Fatalf("certificate: %v", err)
	}
	if len(chain) != 2 {
		t.Fatalf("certificate holds %d certificates, want the leaf and its CA", len(chain))
	}
	key, err := secrets.ParsePrivateKeyPEM(cert.Key)
	if err != nil {
		t.Fatalf("key: %v", err)
	}
	if !secrets.KeyMatchesCertificate(chain[0], key) {
		t.Error("key does not match the certificate")
	}
	if want := []string{domain, "*." + domain}; !slices.Equal(chain[0].DNSNames, want) {
		t.Errorf("certificate names %q, want %q", chain[0].DNSNames, want)
	}
	// OpenSSL, unlike Go, takes a certificate whose subject is its
	// issuer's for a self-signed one, and refuses it.
	if chain[0].Subject.String() == chain[1].Subject.String() {
		t.Errorf("certificate subject %q is its CA's", chain[0].Subject)
	}
	roots := x509.NewCertPool()
	roots.AddCert(root.Cert)
	intermediates := x509.NewCertPool()
	intermediates.AddCert(chain[1])
	for _, name := range []string{domain, "portal." + domain} {
		if _, err := chain[0].Verify(x509.VerifyOptions{Roots: roots, Intermediates: intermediates, DNSName: name}); err != nil {
			t.Errorf("certificate does not verify to the root for %s: %v", name, err)
		}
	}
	return chain[0]
}

// wantValidFor fails t unless cert is valid until about validity from now.
func wantValidFor(t *testing.T, cert *x509.Certificate, validity time.Duration) {
	t.Helper()
	if want := time.Now().Add(validity); cert.NotAfter.Before(want.Add(-time.Hour)) || cert.NotAfter.After(want.Add(time.Hour)) {
		t.Errorf("certificate valid until %s, want about %s", cert.NotAfter, want)
	}
}

func TestRouterCertificate_IssuedForTheDomainFromItsCAMintedFromTheRoot(t *testing.T) {
	withDataHome(t)
	root := testpki.NewRootCA(testpki.WithValidity(time.Now().Add(-time.Hour), time.Now().AddDate(5, 0, 0)))
	cacheRoot(t, "educates-ca", root)

	cert, notes, err := localcerts.RouterCertificate("workshop.test", nil)
	if err != nil {
		t.Fatalf("RouterCertificate: %v", err)
	}

	minted := cachedCAKeyPair(t, "workshop.test-ca-key-pair", root)
	leaf := routerLeaf(t, cert, "workshop.test", root)
	if err := leaf.CheckSignatureFrom(minted.chain[0]); err != nil {
		t.Errorf("certificate not issued by the domain's CA key pair: %v", err)
	}
	wantValidFor(t, leaf, 365*24*time.Hour)
	if len(notes) != 2 || !strings.Contains(notes[0], `Minted CA key pair "workshop.test-ca-key-pair"`) ||
		!strings.Contains(notes[1], "router") || !strings.Contains(notes[1], `"workshop.test"`) {
		t.Errorf("notes = %q, want the CA key pair minted, then the router's certificate issued", notes)
	}
}

func TestRouterCertificate_IssuedFromACachedCAKeyPairWithoutMintingOne(t *testing.T) {
	dataHome := withDataHome(t)
	root := testpki.NewRootCA()
	cacheRoot(t, "educates-ca", root)
	resolveAndCommit(t, localConfig("workshop.test", false))
	before := cacheFiles(t, dataHome)

	cert, notes, err := localcerts.RouterCertificate("workshop.test", nil)
	if err != nil {
		t.Fatalf("RouterCertificate: %v", err)
	}

	leaf := routerLeaf(t, cert, "workshop.test", root)
	if err := leaf.CheckSignatureFrom(cachedCAKeyPair(t, "workshop.test-ca-key-pair", root).chain[0]); err != nil {
		t.Errorf("certificate not issued by the cached CA key pair: %v", err)
	}
	if after := cacheFiles(t, dataHome); !reflect.DeepEqual(after, before) {
		t.Errorf("RouterCertificate changed the cache: %v", keys(after))
	}
	if len(notes) != 1 {
		t.Errorf("notes = %q, want only the router's certificate issued", notes)
	}
}

func TestRouterCertificate_KeptUntilWithin30DaysOfItsExpiry(t *testing.T) {
	withDataHome(t)
	root := testpki.NewRootCA(testpki.WithValidity(time.Now().Add(-time.Hour), time.Now().AddDate(5, 0, 0)))
	cacheRoot(t, "educates-ca", root)
	if _, _, err := localcerts.RouterCertificate("workshop.test", nil); err != nil {
		t.Fatalf("RouterCertificate: %v", err)
	}
	ca := cachedCAKeyPair(t, "workshop.test-ca-key-pair", root)

	for _, tc := range []struct {
		name    string
		expires time.Duration
		kept    bool
	}{
		{name: "expiring in 40 days", expires: 40 * 24 * time.Hour, kept: true},
		{name: "expiring in 20 days", expires: 20 * 24 * time.Hour},
		{name: "expired", expires: -24 * time.Hour},
	} {
		t.Run(tc.name, func(t *testing.T) {
			leaf := ca.ca.NewLeaf(testpki.WithDNSNames("workshop.test", "*.workshop.test"),
				testpki.WithValidity(time.Now().AddDate(-1, 0, 0), time.Now().Add(tc.expires)))
			current := localclusters.RouterCertificate{
				Domain:      "workshop.test",
				Certificate: append(append([]byte{}, leaf.CertPEM...), secrets.EncodeCertificatePEM(ca.chain[0])...),
				Key:         leaf.KeyPEM,
			}

			cert, notes, err := localcerts.RouterCertificate("workshop.test", &current)
			if err != nil {
				t.Fatalf("RouterCertificate: %v", err)
			}

			if tc.kept {
				if !reflect.DeepEqual(cert, current) || len(notes) != 0 {
					t.Errorf("RouterCertificate replaced a certificate valid for 40 more days, noting %q", notes)
				}
				return
			}
			wantValidFor(t, routerLeaf(t, cert, "workshop.test", root), 365*24*time.Hour)
			if len(notes) != 1 || !strings.Contains(notes[0], leaf.Cert.NotAfter.UTC().Format(time.DateOnly)) {
				t.Errorf("notes = %q, want one saying the certificate it replaced expires on %s", notes,
					leaf.Cert.NotAfter.UTC().Format(time.DateOnly))
			}
		})
	}
}

func TestRouterCertificate_ReissuedWhenTheDomainCADidNotIssueIt(t *testing.T) {
	withDataHome(t)
	root := testpki.NewRootCA()
	cacheRoot(t, "educates-ca", root)
	other := testpki.NewRootCA().NewIntermediateCA(testpki.WithPermittedDNSDomains("workshop.test"))
	leaf := other.NewLeaf(testpki.WithDNSNames("workshop.test", "*.workshop.test"))
	current := localclusters.RouterCertificate{Domain: "workshop.test", Certificate: leaf.ChainPEM(), Key: leaf.KeyPEM}

	cert, _, err := localcerts.RouterCertificate("workshop.test", &current)
	if err != nil {
		t.Fatalf("RouterCertificate: %v", err)
	}

	routerLeaf(t, cert, "workshop.test", root)
}

func TestRouterCertificate_ACachedTLSCertificateServedAsItIs(t *testing.T) {
	dataHome := withDataHome(t)
	cacheRoot(t, "educates-ca", testpki.NewRootCA())
	mkcert := testpki.NewRootCA()
	writeCached(t, secrets.NewCATrustSecret("mkcert-ca", mkcert.CertPEM))
	wildcard := wildcardLeaf(mkcert, "workshop.test")
	cacheTLS(t, "workshop-tls", wildcard)
	before := cacheFiles(t, dataHome)

	cert, notes, err := localcerts.RouterCertificate("workshop.test", nil)
	if err != nil {
		t.Fatalf("RouterCertificate: %v", err)
	}

	want := localclusters.RouterCertificate{Domain: "workshop.test", Certificate: wildcard.CertPEM, Key: wildcard.KeyPEM}
	if !reflect.DeepEqual(cert, want) {
		t.Errorf("RouterCertificate did not serve the cached TLS certificate as it is")
	}
	if after := cacheFiles(t, dataHome); !reflect.DeepEqual(after, before) {
		t.Errorf("RouterCertificate changed the cache: %v", keys(after))
	}
	if len(notes) != 0 {
		t.Errorf("notes = %q, want none", notes)
	}
}

func TestRouterCertificate_RefusesATLSCertificateNoCachedCAVerifies(t *testing.T) {
	withDataHome(t)
	cacheRoot(t, "educates-ca", testpki.NewRootCA())
	cacheTLS(t, "workshop-tls", wildcardLeaf(testpki.NewRootCA(), "workshop.test"))

	if _, _, err := localcerts.RouterCertificate("workshop.test", nil); err == nil || !strings.Contains(err.Error(), `"workshop-tls"`) {
		t.Errorf("RouterCertificate = %v, want an error naming the TLS certificate no cached CA verifies", err)
	}
}

func TestRouterCertificate_NoRootNoTLSCertificate_Errors(t *testing.T) {
	withDataHome(t)

	if _, _, err := localcerts.RouterCertificate("workshop.test", nil); err == nil || !strings.Contains(err.Error(), "educates local secrets add ca") {
		t.Errorf("RouterCertificate = %v, want an error saying how to cache a root CA", err)
	}
}
