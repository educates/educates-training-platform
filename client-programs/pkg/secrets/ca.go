package secrets

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"fmt"
	"math/big"
	"net"
	"strings"
	"time"
)

// GenerateSelfSignedCA produces a fresh self-signed root CA certificate
// and private key for the EducatesLocalConfig laptop flow. The user
// trusts the root once; deploys mint a CA for each ingress domain from it
// with MintDomainCA, and cert-manager signs workshop certificates from
// that CA.
//
// Choices:
//   - RSA 2048: broad cert-manager and browser compatibility, fast on a
//     laptop.
//   - 10-year validity: laptops live longer than 1 year, expiry surprises
//     are annoying, and the trust is scoped to one user's keychain so the
//     wide window is acceptable.
//   - CommonName and Organization carry "educates" so the certificate is
//     visually identifiable in browser certificate UIs.
//   - No name constraints: the root's key never leaves the laptop, and
//     every CA minted from it is limited to its own domain.
func GenerateSelfSignedCA(commonName string) (certPEM, keyPEM []byte, err error) {
	now := time.Now().UTC()
	tmpl := &x509.Certificate{
		Subject: pkix.Name{
			CommonName:   commonName,
			Organization: []string{"Educates"},
		},
		NotBefore:             now.Add(-1 * time.Hour),
		NotAfter:              now.AddDate(10, 0, 0),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
		BasicConstraintsValid: true,
		IsCA:                  true,
	}
	return issueCertificate(tmpl, nil, nil)
}

// MintDomainCA mints, from root and its private key rootKey, an
// intermediate CA limited to ingress domain domain, and returns its
// certificate and a new private key as PEM.
//
// The CA may sign only end-entity certificates (path length 0), and a
// critical name constraint permits DNS names in domain and its
// subdomains and excludes every IPv4 and IPv6 address, so a key taken
// from a cluster cannot sign for any other site. Its subject is the
// domain under the root's organization, and it is valid until the root
// expires. Like the generated root it uses an RSA 2048 key.
func MintDomainCA(root *x509.Certificate, rootKey crypto.Signer, domain string) (certPEM, keyPEM []byte, err error) {
	notBefore := time.Now().UTC().Add(-1 * time.Hour)
	if notBefore.Before(root.NotBefore) {
		notBefore = root.NotBefore
	}
	tmpl := &x509.Certificate{
		Subject: pkix.Name{
			CommonName:   domain,
			Organization: root.Subject.Organization,
		},
		NotBefore:                   notBefore,
		NotAfter:                    root.NotAfter,
		KeyUsage:                    x509.KeyUsageCertSign | x509.KeyUsageCRLSign | x509.KeyUsageDigitalSignature,
		BasicConstraintsValid:       true,
		IsCA:                        true,
		MaxPathLen:                  0,
		MaxPathLenZero:              true,
		PermittedDNSDomainsCritical: true,
		PermittedDNSDomains:         []string{domain},
		ExcludedIPRanges: []*net.IPNet{
			{IP: net.IPv4zero.To4(), Mask: net.CIDRMask(0, 32)},
			{IP: net.IPv6zero, Mask: net.CIDRMask(0, 128)},
		},
	}
	return issueCertificate(tmpl, root, rootKey)
}

// IssueDomainCertificate issues, from CA certificate ca and its private
// key caKey, a TLS server certificate for ingress domain domain and every
// name one label under it, *.<domain>, valid from now until notAfter or
// until ca expires, whichever comes first. It returns the certificate and
// a new private key as PEM. Its subject is the domain under the CA's
// organization, and like the CAs it uses an RSA 2048 key.
func IssueDomainCertificate(ca *x509.Certificate, caKey crypto.Signer, domain string, notAfter time.Time) (certPEM, keyPEM []byte, err error) {
	notBefore := time.Now().UTC().Add(-1 * time.Hour)
	if notBefore.Before(ca.NotBefore) {
		notBefore = ca.NotBefore
	}
	if notAfter.After(ca.NotAfter) {
		notAfter = ca.NotAfter
	}
	tmpl := &x509.Certificate{
		Subject: pkix.Name{
			CommonName:   domain,
			Organization: ca.Subject.Organization,
		},
		DNSNames:    []string{domain, "*." + domain},
		NotBefore:   notBefore,
		NotAfter:    notAfter,
		KeyUsage:    x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	return issueCertificate(tmpl, ca, caKey)
}

// issueCertificate issues a certificate from tmpl for a new RSA 2048
// key, with a random 128-bit serial number, signed by parent with
// parentKey, or self-signed when parent is nil. It returns the
// certificate and the new private key as PEM.
func issueCertificate(tmpl, parent *x509.Certificate, parentKey crypto.Signer) (certPEM, keyPEM []byte, err error) {
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, nil, fmt.Errorf("generate RSA key: %w", err)
	}
	tmpl.SerialNumber, err = rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return nil, nil, fmt.Errorf("generate serial: %w", err)
	}
	if parent == nil {
		parent, parentKey = tmpl, priv
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, parent, &priv.PublicKey, parentKey)
	if err != nil {
		return nil, nil, fmt.Errorf("create certificate: %w", err)
	}
	keyPEM, err = EncodePrivateKeyPEM(priv)
	if err != nil {
		return nil, nil, err
	}
	return encodeCertificateDER(der), keyPEM, nil
}

// PermitsDNSName reports whether the DNS name constraints of CA
// certificate ca allow it to sign a certificate for name. Following RFC
// 5280, a constraint matches the name it states and every subdomain of
// it, or only subdomains when it starts with a dot; an excluded match
// wins, and a CA with no permitted DNS domains permits every name it does
// not exclude.
func PermitsDNSName(ca *x509.Certificate, name string) bool {
	for _, constraint := range ca.ExcludedDNSDomains {
		if matchesDNSConstraint(name, constraint) {
			return false
		}
	}
	if len(ca.PermittedDNSDomains) == 0 {
		return true
	}
	for _, constraint := range ca.PermittedDNSDomains {
		if matchesDNSConstraint(name, constraint) {
			return true
		}
	}
	return false
}

func matchesDNSConstraint(name, constraint string) bool {
	name, constraint = strings.ToLower(name), strings.ToLower(constraint)
	switch {
	case constraint == "":
		return true
	case strings.HasPrefix(constraint, "."):
		return strings.HasSuffix(name, constraint)
	default:
		return name == constraint || strings.HasSuffix(name, "."+constraint)
	}
}
