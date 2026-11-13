// Package localcerts is the CLI's local certificate resolution step. From
// the local secrets cache and an EducatesLocalConfig's ingress.domain and
// ingress.insecure, it decides how a local install gets its ingress
// certificates and returns that decision as a Plan. Every command that
// renders, deploys or syncs a local install takes its certificate
// decisions from here and nowhere else.
//
// A secure install serves the one unexpired wildcard TLS certificate in
// the cache that covers the ingress domain, with the StaticCertificate
// provider and no cert-manager, and trusts the cached CA certificate its
// chain verifies against. Without such a certificate it issues its
// ingress certificate from the CA key pair <domain>-ca-key-pair: an
// intermediate CA limited to the ingress domain, minted from the one root
// CA in the cache. The root's key never leaves this machine, and the
// cluster trusts the root through its CA trust Secret.
package localcerts

import (
	"crypto"
	"crypto/x509"
	"fmt"
	"reflect"
	"slices"
	"sort"
	"strings"
	"time"

	apiv1 "k8s.io/api/core/v1"

	"github.com/educates/educates-training-platform/client-programs/pkg/config/translator"
	"github.com/educates/educates-training-platform/client-programs/pkg/config/v1alpha1"
	"github.com/educates/educates-training-platform/client-programs/pkg/secrets"
)

// Plan is the outcome of resolving the certificates of a local install.
type Plan struct {
	// Certificates is the certificate decision the translator emits as
	// EducatesClusterConfig ingress.certificates.
	Certificates translator.LocalCertificates

	// SyncSecretNames names the cached Secrets a deploy pushes into the
	// secrets.LocalSecretsSyncNamespace namespace, sorted. It includes a
	// CA key pair the plan mints, which is in the cache once the plan is
	// committed.
	SyncSecretNames []string

	// Notices tell the user about the decision.
	Notices []string

	// Warnings tell the user about a problem the decision works around.
	Warnings []string

	// mints are the CA key pairs the plan minted, which Commit writes to
	// the cache.
	mints []mintedCAKeyPair
}

// mintedCAKeyPair is a CA key pair minted for the plan and the line
// Commit prints when it writes it.
type mintedCAKeyPair struct {
	secret *apiv1.Secret
	note   string
}

// Reporter receives the lines a plan reports. The deploy progress
// reporter satisfies it.
type Reporter interface {
	Note(msg string)
}

// Resolve decides the certificates of a local install from cfg's
// ingress.domain and ingress.insecure and the local secrets cache. It
// only reads the cache: a CA key pair it mints is held in the plan until
// Commit. The nip.io defaulting of ingress.domain must already have run.
func Resolve(cfg *v1alpha1.EducatesLocalConfig) (*Plan, error) {
	names, err := secrets.ListCachedSecretNames()
	if err != nil {
		return nil, err
	}
	plan := &Plan{}
	if cfg.Ingress.Insecure {
		// Plain HTTP needs no CA.
		plan.Certificates = translator.LocalCertificates{Provider: translator.CertificatesProviderNone}
		plan.SyncSecretNames = syncSecretNames(names, "")
		return plan, nil
	}

	domain := cfg.Ingress.Domain
	if domain == "" {
		return nil, fmt.Errorf("ingress.domain is empty; cannot decide the ingress certificates")
	}

	// The cache holds at most one root CA. More is an error whatever the
	// cache would otherwise serve, so no install depends on which root
	// was meant.
	roots, err := secrets.CachedRootCAs()
	if err != nil {
		return nil, err
	}
	if len(roots) > 1 {
		return nil, fmt.Errorf(`the local secrets cache holds more than one root CA: %s.

It can hold only one root CA, from which deploys mint a CA for each
ingress domain. Remove the root CAs you do not use with:

  educates local secrets remove NAME`, secrets.QuoteNames(roots))
	}

	// A wildcard TLS certificate covering the domain is served as it is,
	// with no cert-manager.
	selected, expired, err := selectTLSCertificate(names, domain)
	if err != nil {
		return nil, err
	}
	for _, c := range expired {
		plan.Notices = append(plan.Notices, fmt.Sprintf("Skipped TLS certificate %q for ingress domain %q: it expired on %s.",
			c.name, domain, c.cert().NotAfter.UTC().Format(time.DateOnly)))
	}
	if selected != nil {
		plan.useStaticCertificate(selected, domain, names)
		return plan, nil
	}

	// Otherwise the root CA mints the CA key pair cert-manager issues the
	// ingress certificate from.
	if len(roots) == 0 {
		return nil, noCertificateError(domain, expired)
	}
	root, err := loadRootCA(roots[0], domain)
	if err != nil {
		return nil, err
	}
	keyPair, err := plan.caKeyPairFor(domain, root)
	if err != nil {
		return nil, err
	}
	plan.Certificates = translator.LocalCertificates{
		Provider: translator.CertificatesProviderBundledCertManager,
		CustomCA: &translator.LocalCustomCA{
			CAKeyPairSecretRef:     translator.SecretRef{Name: keyPair, Namespace: secrets.LocalSecretsSyncNamespace},
			CACertificateSecretRef: &translator.SecretRef{Name: root.name, Namespace: secrets.LocalSecretsSyncNamespace},
		},
	}
	plan.SyncSecretNames = syncSecretNames(names, keyPair)
	return plan, nil
}

// ResolveSync resolves what `educates local secrets sync` pushes into the
// secrets.LocalSecretsSyncNamespace namespace for a local cluster whose
// config is cfg, nil when the cluster has none. For a config that has an
// ingress.domain, it is the plan Resolve makes for that config. Without
// a config, or for a config with no ingress.domain, whose install takes
// the nip.io name of its listen address and so serves plain HTTP, no CA
// key pair is selected and none is pushed. When Resolve cannot decide the certificates of the config,
// such as a secure domain with nothing usable cached, the sync still
// pushes every Secret that holds no CA key, and the plan warns why no CA
// key pair is pushed; a deploy of that config fails until it is fixed.
// It only reads the cache.
func ResolveSync(cfg *v1alpha1.EducatesLocalConfig) (*Plan, error) {
	names, err := secrets.ListCachedSecretNames()
	if err != nil {
		return nil, err
	}
	plan := &Plan{SyncSecretNames: syncSecretNames(names, "")}
	if cfg == nil || cfg.Ingress.Domain == "" {
		return plan, nil
	}
	resolved, err := Resolve(cfg)
	if err != nil {
		plan.Warnings = append(plan.Warnings, fmt.Sprintf(`the ingress certificates of the local config cannot be decided, so
no CA key pair is pushed and deploying the local config fails until this
is fixed: %v`, err))
		return plan, nil
	}
	return resolved, nil
}

// tlsCertificate is a wildcard TLS certificate in the local secrets
// cache: a kubernetes.io/tls Secret whose certificate is not a CA. It
// serves every domain it has a *.<domain> DNS SAN for; any domain
// annotation it carries is ignored.
type tlsCertificate struct {
	name   string
	secret *apiv1.Secret
	// chain is tls.crt: the certificate followed by any intermediates.
	chain []*x509.Certificate
}

func (c *tlsCertificate) cert() *x509.Certificate { return c.chain[0] }

// covers reports whether the certificate has a *.<domain> DNS SAN.
func (c *tlsCertificate) covers(domain string) bool {
	return slices.Contains(secrets.WildcardDomains(c.cert()), normalizeDomain(domain))
}

// selectTLSCertificate returns the one unexpired TLS certificate among
// the cached Secrets names that covers domain, or nil when there is
// none, and the TLS certificates for domain it skipped because they
// expired. More than one unexpired certificate for domain is an error
// naming them.
func selectTLSCertificate(names []string, domain string) (selected *tlsCertificate, expired []*tlsCertificate, err error) {
	var valid []*tlsCertificate
	now := time.Now()
	for _, name := range names {
		c := loadTLSCertificate(name)
		if c == nil || !c.covers(domain) {
			continue
		}
		if now.After(c.cert().NotAfter) {
			expired = append(expired, c)
			continue
		}
		valid = append(valid, c)
	}
	switch len(valid) {
	case 0:
		return nil, expired, nil
	case 1:
	default:
		names := make([]string, len(valid))
		for i, c := range valid {
			names[i] = c.name
		}
		return nil, nil, fmt.Errorf(`more than one TLS certificate in the local secrets cache covers ingress
domain %q: %s.

A deploy serves one wildcard certificate for the domain. Remove the ones
you do not want to use with:

  educates local secrets remove NAME`, domain, secrets.QuoteNames(names))
	}

	c := valid[0]
	if _, err := secrets.ParseKeyPair(secrets.SecretData(c.secret, "tls.crt"), secrets.SecretData(c.secret, "tls.key")); err != nil {
		return nil, nil, fmt.Errorf(`TLS certificate %q covers ingress domain %q but cannot be served: its
tls.key is not the private key of its certificate. Cache it again with
'educates local secrets add tls %s --cert FILE --key FILE', or remove it
with:

  educates local secrets remove %s`, c.name, domain, c.name, c.name)
	}
	return c, expired, nil
}

// loadTLSCertificate reads the cached Secret name as a TLS certificate,
// returning nil when it is not one or cannot be read.
func loadTLSCertificate(name string) *tlsCertificate {
	s, err := secrets.LoadCachedSecret(name)
	if err != nil {
		return nil
	}
	if role, _ := secrets.ClassifyCachedSecret(name, s); role != secrets.TLSCertificateRole {
		return nil
	}
	chain, err := secrets.ParseCertificatesPEM(secrets.SecretData(s, "tls.crt"))
	if err != nil {
		return nil
	}
	return &tlsCertificate{name: name, secret: s, chain: chain}
}

// useStaticCertificate makes the plan serve TLS certificate c for domain
// with the StaticCertificate provider, trusting the first cached CA
// certificate, by name, that c's chain verifies against. It notices the
// choice, the certificate to remove for cert-manager and the other
// domains c covers, and warns when no cached CA certificate verifies c.
func (p *Plan) useStaticCertificate(c *tlsCertificate, domain string, names []string) {
	static := &translator.LocalStaticCertificate{
		TLSSecretRef: translator.SecretRef{Name: c.name, Namespace: secrets.LocalSecretsSyncNamespace},
	}
	if ca := verifyingCACertificate(names, c); ca != "" {
		static.CACertificateSecretRef = &translator.SecretRef{Name: ca, Namespace: secrets.LocalSecretsSyncNamespace}
	} else {
		p.Warnings = append(p.Warnings, fmt.Sprintf(`no CA certificate in the local secrets cache verifies TLS certificate
%q, so the training portal and workshop sessions will not trust the
ingress. Register the CA that issued it, for mkcert its root CA, with:

  educates local secrets add ca mkcert-ca --cert "$(mkcert -CAROOT)/rootCA.pem"`, c.name))
	}
	p.Certificates = translator.LocalCertificates{
		Provider:          translator.CertificatesProviderStaticCertificate,
		StaticCertificate: static,
	}
	p.SyncSecretNames = syncSecretNames(names, "")

	notice := fmt.Sprintf(`Serving TLS certificate %q for ingress domain %q with the
StaticCertificate provider: cert-manager will not be installed. To have
cert-manager issue the ingress certificates from a root CA instead,
remove the certificate with:

  educates local secrets remove %s`, c.name, domain, c.name)
	var others []string
	for _, d := range secrets.WildcardDomains(c.cert()) {
		if d != normalizeDomain(domain) {
			others = append(others, d)
		}
	}
	if len(others) > 0 {
		notice += fmt.Sprintf("\n\nIt also serves these other domains, which removing it affects too: %s.", strings.Join(others, ", "))
	}
	p.Notices = append(p.Notices, notice)
}

// verifyingCACertificate returns the name of the first cached CA
// certificate in names that c's chain, through the intermediates it
// carries, verifies against for a TLS server, or "" when none does. A CA
// certificate is a cached Secret secrets.ClassifyCachedSecret classifies
// as one, as `local secrets list` shows it: its ca.crt holds only CA
// certificates, and it holds no CA private key and no TLS certificate.
// The operator reads CA trust from its ca.crt.
func verifyingCACertificate(names []string, c *tlsCertificate) string {
	intermediates := x509.NewCertPool()
	for _, cert := range c.chain[1:] {
		intermediates.AddCert(cert)
	}
	for _, name := range names {
		s, err := secrets.LoadCachedSecret(name)
		if err != nil {
			continue
		}
		if role, _ := secrets.ClassifyCachedSecret(name, s); role != secrets.CACertificateRole {
			continue
		}
		cas := secrets.CATrustCertificates(s)
		roots := x509.NewCertPool()
		for _, ca := range cas {
			roots.AddCert(ca)
		}
		if _, err := c.cert().Verify(x509.VerifyOptions{
			Roots:         roots,
			Intermediates: intermediates,
			KeyUsages:     []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		}); err == nil {
			return name
		}
	}
	return ""
}

// normalizeDomain lowercases domain and drops a trailing dot, as
// secrets.WildcardDomains reports domains.
func normalizeDomain(domain string) string {
	return strings.ToLower(strings.TrimSuffix(domain, "."))
}

// rootCA is the root CA of the local secrets cache, read from its CA root
// key pair.
type rootCA struct {
	name    string
	cert    *x509.Certificate
	certPEM []byte
	key     crypto.Signer
}

// loadRootCA loads the root CA name of the cache, checking it can mint a
// CA for domain.
func loadRootCA(name, domain string) (*rootCA, error) {
	keyPairName := secrets.RootKeyPairName(name)
	unusable := func(format string, args ...any) error {
		return fmt.Errorf("root CA %q cannot be used: %s", name, fmt.Sprintf(format, args...))
	}
	keyPair, err := secrets.LoadCachedSecret(keyPairName)
	if err != nil {
		return nil, unusable("%v", err)
	}
	kp, err := secrets.ParseKeyPair(secrets.SecretData(keyPair, "tls.crt"), secrets.SecretData(keyPair, "tls.key"))
	if err != nil {
		return nil, unusable("its CA root key pair %q: %v", keyPairName, err)
	}
	cert := kp.Certificate()
	if !secrets.IsCACertificate(cert) {
		return nil, unusable("the certificate of its CA root key pair %q is not a CA certificate", keyPairName)
	}
	if now := time.Now(); now.After(cert.NotAfter) {
		return nil, unusable("it expired on %s; register a new root CA", cert.NotAfter.UTC().Format(time.DateOnly))
	}

	// The cluster trusts the root through its CA trust Secret, so the
	// Secret must hold the root's own certificate.
	trust, err := secrets.LoadCachedSecret(name)
	if err != nil {
		return nil, unusable("its CA trust Secret %q is missing from the local secrets cache", name)
	}
	trustCert, err := secrets.ParseCertificatePEM(secrets.SecretData(trust, "ca.crt"))
	if err != nil || !trustCert.Equal(cert) {
		return nil, unusable("its CA trust Secret %q does not hold the root certificate as ca.crt", name)
	}

	wildcard := "*." + domain
	if !secrets.PermitsDNSName(cert, wildcard) {
		return nil, fmt.Errorf(`root CA %q cannot mint a CA for ingress domain %q: its name
constraints do not permit %q (permitted DNS domains: %s; excluded DNS
domains: %s).

Set ingress.domain to a domain the root CA permits, or register a root CA
whose name constraints permit this domain.`,
			name, domain, wildcard, listOrNone(cert.PermittedDNSDomains), listOrNone(cert.ExcludedDNSDomains))
	}

	return &rootCA{name: name, cert: cert, certPEM: secrets.EncodeCertificatePEM(cert), key: kp.Key}, nil
}

// noCertificateError is the error for a secure install whose cache has
// neither a usable wildcard TLS certificate for domain nor a root CA,
// naming the commands that fix it. expired are the TLS certificates for
// domain that were skipped because they expired.
func noCertificateError(domain string, expired []*tlsCertificate) error {
	var skipped string
	if len(expired) > 0 {
		names := make([]string, len(expired))
		for i, c := range expired {
			names[i] = c.name
		}
		skipped = fmt.Sprintf("\n\nThe cached TLS certificates for the domain have expired: %s.", secrets.QuoteNames(names))
	}
	return fmt.Errorf(`no certificate for ingress domain %q in the local secrets cache: it holds
no root CA and no unexpired wildcard TLS certificate for %q.%s

A secure install is the default (ingress.insecure is false): Educates
serves workshops over HTTPS, either with certificates from a CA it mints
for the ingress domain from a root CA cached on this machine, or with a
wildcard TLS certificate you cache. A root CA's private key never leaves
this machine, and you trust its certificate once for every local ingress
domain.

To generate a root CA:

  educates local secrets add ca <name>

To register an existing root CA with its private key, such as mkcert's:

  educates local secrets add ca <name> --cert "$(mkcert -CAROOT)/rootCA.pem" --key "$(mkcert -CAROOT)/rootCA-key.pem"

To serve a mkcert wildcard certificate instead, which installs no
cert-manager, cache it and mkcert's root CA certificate:

  mkcert -cert-file %s.pem -key-file %s-key.pem "*.%s"
  educates local secrets add tls %s-tls --cert %s.pem --key %s-key.pem
  educates local secrets add ca mkcert-ca --cert "$(mkcert -CAROOT)/rootCA.pem"

To serve plain HTTP with no CA instead:

  educates local config set ingress.insecure true`,
		domain, "*."+domain, skipped, domain, domain, domain, domain, domain, domain)
}

// caKeyPairFor returns the name of the CA key pair for domain. When the
// cache has no CA key pair for domain that root minted and that is still
// valid, it mints one into the plan for Commit to write.
func (p *Plan) caKeyPairFor(domain string, root *rootCA) (string, error) {
	name := secrets.CAKeyPairName(domain)
	note := fmt.Sprintf("Minted CA key pair %q for ingress domain %q from root CA %q.", name, domain, root.name)
	if cached, err := secrets.LoadCachedSecret(name); err == nil {
		state := checkCachedCAKeyPair(cached, domain, root)
		if state == caKeyPairUsable {
			return name, nil
		}
		note = fmt.Sprintf("Minted CA key pair %q for ingress domain %q from root CA %q, replacing the cached one, which %s.", name, domain, root.name, state)
	}

	certPEM, keyPEM, err := secrets.MintDomainCA(root.cert, root.key, domain)
	if err != nil {
		return "", fmt.Errorf("mint CA key pair %q from root CA %q: %w", name, root.name, err)
	}
	p.mints = append(p.mints, mintedCAKeyPair{
		secret: secrets.NewCAKeyPairSecret(domain, root.name, certPEM, keyPEM, root.certPEM),
		note:   note,
	})
	return name, nil
}

// cachedCAKeyPairState is whether a cached CA key pair can serve an
// ingress domain under the root CA, and if not, why. Its String is the
// reason, phrased to follow "the cached one, which".
type cachedCAKeyPairState int

const (
	caKeyPairUsable cachedCAKeyPairState = iota
	caKeyPairInvalid
	caKeyPairNotLimitedToDomain
	caKeyPairFromAnotherRoot
	caKeyPairExpired
)

func (s cachedCAKeyPairState) String() string {
	switch s {
	case caKeyPairUsable:
		return "can serve the domain"
	case caKeyPairNotLimitedToDomain:
		return "is not limited to the domain"
	case caKeyPairFromAnotherRoot:
		return "was minted from another root CA"
	case caKeyPairExpired:
		return "has expired"
	default:
		return "is not a valid CA key pair"
	}
}

// checkCachedCAKeyPair returns whether the cached CA key pair s can
// serve domain under root.
func checkCachedCAKeyPair(s *apiv1.Secret, domain string, root *rootCA) cachedCAKeyPairState {
	if s.Type != apiv1.SecretTypeTLS {
		return caKeyPairInvalid
	}
	kp, err := secrets.ParseKeyPair(secrets.SecretData(s, "tls.crt"), secrets.SecretData(s, "tls.key"))
	if err != nil || !secrets.IsCACertificate(kp.Certificate()) {
		return caKeyPairInvalid
	}
	ca := kp.Certificate()
	if !reflect.DeepEqual(ca.PermittedDNSDomains, []string{domain}) {
		return caKeyPairNotLimitedToDomain
	}
	caCrt, err := secrets.ParseCertificatePEM(secrets.SecretData(s, "ca.crt"))
	if s.Annotations[secrets.RootCAAnnotation] != root.name ||
		err != nil || !caCrt.Equal(root.cert) ||
		ca.CheckSignatureFrom(root.cert) != nil {
		return caKeyPairFromAnotherRoot
	}
	if time.Now().After(ca.NotAfter) {
		return caKeyPairExpired
	}
	return caKeyPairUsable
}

// syncSecretNames returns the cached Secrets a sync pushes, sorted: every
// cached Secret except those holding a CA's private key, plus selected,
// the CA key pair the plan selected, when not empty.
func syncSecretNames(names []string, selected string) []string {
	var out []string
	for _, name := range names {
		if name != selected && !holdsCAKey(name) {
			out = append(out, name)
		}
	}
	if selected != "" {
		out = append(out, selected)
		sort.Strings(out)
	}
	return out
}

// holdsCAKey reports whether the cached Secret name holds a CA's private
// key: a CA root key pair, a CA key pair, or an invalid Secret holding a
// CA with its key, as alpha releases cached one. A Secret that cannot be
// read counts only when its name alone classifies it as a CA root key
// pair or a CA key pair; otherwise the sync reports it.
func holdsCAKey(name string) bool {
	if role, ok := secrets.ClassifyCachedSecretName(name); ok {
		return role.HoldsCAKey()
	}
	s, err := secrets.LoadCachedSecret(name)
	if err != nil {
		return false
	}
	role, _ := secrets.ClassifyCachedSecret(name, s)
	return role.HoldsCAKey()
}

func listOrNone(values []string) string {
	if len(values) == 0 {
		return "none"
	}
	return strings.Join(values, ", ")
}

// Report prints the plan's notices and then its warnings to r, without
// changing anything.
func (p *Plan) Report(r Reporter) {
	for _, n := range p.Notices {
		r.Note(n)
	}
	for _, w := range p.Warnings {
		r.Note("warning: " + w)
	}
}

// Commit carries out what the plan changes in the local cache, writing
// each CA key pair it minted and printing a line for each to r, and then
// reports the plan to r. Deploy and sync paths call it once before
// pushing to a cluster; render only calls Report, so rendering leaves the
// cache untouched.
func (p *Plan) Commit(r Reporter) error {
	for _, m := range p.mints {
		if err := secrets.WriteCachedSecret(m.secret); err != nil {
			return err
		}
		r.Note(m.note)
	}
	p.mints = nil
	p.Report(r)
	return nil
}

// TranslatorOptions returns the translator input the plan decided.
func (p *Plan) TranslatorOptions() translator.Options {
	return translator.Options{LocalCertificates: p.Certificates}
}
