package localcerts

import (
	"crypto"
	"crypto/x509"
	"fmt"
	"slices"
	"time"

	"github.com/educates/educates-training-platform/client-programs/pkg/config/translator"
	"github.com/educates/educates-training-platform/client-programs/pkg/localclusters"
	"github.com/educates/educates-training-platform/client-programs/pkg/secrets"
)

const (
	// RouterCertificateValidity is how long a certificate the CLI issues
	// for the router is valid.
	RouterCertificateValidity = 365 * 24 * time.Hour

	// RouterCertificateRenewal is how long before its expiry a
	// certificate the CLI issued for the router is issued again.
	RouterCertificateRenewal = 30 * 24 * time.Hour
)

// RouterCertificate returns the certificate the router ends TLS with for
// ingress domain domain, of a local cluster whose TLS ends at the router,
// and a line for each change it made, to tell the user. current is the
// certificate the router serves for the domain now, nil when it serves
// none.
//
// It serves the one unexpired wildcard TLS certificate the local secrets
// cache holds for the domain, as it is, as Resolve serves one to a cluster
// that ends TLS itself, once a cached CA certificate verifies it, since
// the cluster trusts that CA. Otherwise it serves a certificate for domain
// and *.<domain> issued from the domain's CA key pair, which it first
// mints from the root CA and writes to the cache, as a deploy does, when
// the cache holds none that can serve the domain: current, when that CA
// key pair issued it for the domain and it stays valid for more than
// RouterCertificateRenewal, or else a new one, valid for
// RouterCertificateValidity, which the cluster trusts through the root.
func RouterCertificate(domain string, current *localclusters.RouterCertificate) (localclusters.RouterCertificate, []string, error) {
	names, err := secrets.ListCachedSecretNames()
	if err != nil {
		return localclusters.RouterCertificate{}, nil, err
	}
	roots, err := cachedRootCA()
	if err != nil {
		return localclusters.RouterCertificate{}, nil, err
	}
	selected, expired, err := selectTLSCertificate(names, domain)
	if err != nil {
		return localclusters.RouterCertificate{}, nil, err
	}
	source, err := routerSourceFor(names, domain, selected, expired, roots)
	if err != nil {
		return localclusters.RouterCertificate{}, nil, err
	}
	if source.tls != nil {
		return localclusters.RouterCertificate{
			Domain:      domain,
			Certificate: secrets.SecretData(source.tls.secret, "tls.crt"),
			Key:         secrets.SecretData(source.tls.secret, "tls.key"),
		}, nil, nil
	}

	var notes noteLines
	plan := &Plan{}
	keyPair, err := plan.caKeyPairFor(domain, source.root)
	if err != nil {
		return localclusters.RouterCertificate{}, nil, err
	}
	if err := plan.Commit(&notes); err != nil {
		return localclusters.RouterCertificate{}, nil, err
	}
	ca, caKey, err := loadCAKeyPair(keyPair)
	if err != nil {
		return localclusters.RouterCertificate{}, nil, err
	}

	why := replaceRouterCertificate(current, ca, domain, time.Now())
	if why == "" {
		return *current, notes, nil
	}
	certPEM, keyPEM, err := secrets.IssueDomainCertificate(ca, caKey, domain, time.Now().Add(RouterCertificateValidity))
	if err != nil {
		return localclusters.RouterCertificate{}, nil, fmt.Errorf("issue the router's certificate for ingress domain %q from CA key pair %q: %w", domain, keyPair, err)
	}
	leaf, err := secrets.ParseCertificatePEM(certPEM)
	if err != nil {
		return localclusters.RouterCertificate{}, nil, err
	}
	note := fmt.Sprintf("Issued the router's certificate for ingress domain %q from CA key pair %q, valid until %s",
		domain, keyPair, leaf.NotAfter.UTC().Format(time.DateOnly))
	if current != nil {
		note += ", replacing the one it served, which " + why
	}
	notes = append(notes, note+".")
	return localclusters.RouterCertificate{
		Domain:      domain,
		Certificate: append(certPEM, secrets.EncodeCertificatePEM(ca)...),
		Key:         keyPEM,
	}, notes, nil
}

// replaceRouterCertificate says why the router's certificate current, nil
// for none, has to be replaced by one ca issues for domain, to follow
// "which", and returns "" when it does not: when ca issued it for domain
// and *.<domain>, with its key, and it stays valid for more than
// RouterCertificateRenewal after now.
func replaceRouterCertificate(current *localclusters.RouterCertificate, ca *x509.Certificate, domain string, now time.Time) string {
	if current == nil {
		return "is missing"
	}
	kp, err := secrets.ParseKeyPair(current.Certificate, current.Key)
	if err != nil {
		return "is not a valid certificate and key"
	}
	leaf := kp.Certificate()
	domain = normalizeDomain(domain)
	var names []string
	for _, name := range leaf.DNSNames {
		names = append(names, normalizeDomain(name))
	}
	switch {
	case !slices.Contains(names, domain) || !slices.Contains(names, "*."+domain):
		return "does not cover the domain"
	case leaf.CheckSignatureFrom(ca) != nil:
		return "the CA key pair did not issue"
	case now.After(leaf.NotAfter):
		return "expired on " + leaf.NotAfter.UTC().Format(time.DateOnly)
	case now.Add(RouterCertificateRenewal).After(leaf.NotAfter):
		return "expires on " + leaf.NotAfter.UTC().Format(time.DateOnly)
	}
	return ""
}

// loadCAKeyPair loads the CA key pair name from the local secrets cache,
// returning its CA certificate and private key.
func loadCAKeyPair(name string) (*x509.Certificate, crypto.Signer, error) {
	s, err := secrets.LoadCachedSecret(name)
	if err != nil {
		return nil, nil, err
	}
	kp, err := secrets.ParseKeyPair(secrets.SecretData(s, "tls.crt"), secrets.SecretData(s, "tls.key"))
	if err != nil {
		return nil, nil, fmt.Errorf("CA key pair %q: %w", name, err)
	}
	return kp.Certificate(), kp.Key, nil
}

// noteLines is a Reporter that keeps each note as a line.
type noteLines []string

func (n *noteLines) Note(msg string) { *n = append(*n, msg) }

// routerSource is where the router gets the certificate it ends TLS with
// for the ingress domain of a local cluster whose TLS ends at the router,
// and the CA the cluster trusts that certificate through.
type routerSource struct {
	// tls is the cached wildcard TLS certificate the router serves for
	// the domain as it is, nil when the router serves a certificate the
	// CLI issues from the domain's CA key pair.
	tls *tlsCertificate

	// root is the root CA the domain's CA key pair is minted from, set
	// when tls is nil.
	root *rootCA

	// caTrust is the cached CA trust Secret, by name, that the router's
	// certificate verifies against.
	caTrust string
}

// routerSourceFor decides where the router gets its certificate for
// domain, among the cached Secrets names, given selected and expired, the
// TLS certificates selectTLSCertificate selected and skipped for domain,
// and roots, the one root CA of the cache or none.
//
// A wildcard TLS certificate cached for the domain wins, as it does for a
// cluster that ends TLS itself, and the cluster trusts the cached CA
// certificate that verifies it: one no cached CA certificate verifies is
// refused, since the cluster could not trust the router. Otherwise the
// router serves a certificate issued from the domain's CA key pair, minted
// from the root CA, which the cluster trusts.
func routerSourceFor(names []string, domain string, selected *tlsCertificate, expired []*tlsCertificate, roots []string) (routerSource, error) {
	if selected != nil {
		ca := verifyingCACertificate(names, selected)
		if ca == "" {
			return routerSource{}, fmt.Errorf(`TLS certificate %q covers ingress domain %q, whose TLS ends at the
router, but no CA certificate in the local secrets cache verifies it, so
the cluster could not trust the router. Register the CA that issued it,
for mkcert its root CA, with:

  educates local secrets add ca mkcert-ca --cert "$(mkcert -CAROOT)/rootCA.pem"

or have the router serve a certificate issued from the root CA instead
by removing the TLS certificate with:

  educates local secrets remove %s`, selected.name, domain, selected.name)
		}
		return routerSource{tls: selected, caTrust: ca}, nil
	}
	if len(roots) == 0 {
		return routerSource{}, noCertificateError(domain, expired)
	}
	root, err := loadRootCA(roots[0], domain)
	if err != nil {
		return routerSource{}, err
	}
	return routerSource{root: root, caTrust: root.name}, nil
}

// trustTheRouter makes the plan that of a local cluster on domain whose
// TLS ends at the router, as source says, among the cached Secrets names:
// the None provider, trusting the CA trust Secret of the CA that verifies
// the router's certificate, and syncing no CA key pair, since the cluster
// issues no certificate. It notices a cached TLS certificate the router
// serves.
func (p *Plan) trustTheRouter(source routerSource, domain string, names []string) {
	p.Certificates = translator.LocalCertificates{
		Provider: translator.CertificatesProviderNone,
		None: &translator.LocalNoneCertificates{
			CACertificateSecretRef: &translator.SecretRef{Name: source.caTrust, Namespace: secrets.LocalSecretsSyncNamespace},
		},
	}
	p.SyncSecretNames = syncSecretNames(names, "")
	if source.tls != nil {
		p.Notices = append(p.Notices, fmt.Sprintf(`The router ends TLS for ingress domain %q with TLS certificate %q,
and the cluster trusts CA %q, which verifies it. To have the router serve a
certificate issued from the root CA instead, remove the TLS certificate
with:

  educates local secrets remove %s`, domain, source.tls.name, source.caTrust, source.tls.name))
	}
}
