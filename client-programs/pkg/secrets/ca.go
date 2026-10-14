package secrets

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"time"
)

// GenerateSelfSignedCA produces a fresh self-signed CA certificate +
// private key suitable for the EducatesLocalConfig laptop flow.
// cert-manager's CA-typed ClusterIssuer signs workshop certs from it.
//
// Choices:
//   - RSA 2048 — broad cert-manager + browser compatibility, fast on
//     a laptop, no transitive Go-toolchain concerns.
//   - 10-year validity — laptops live longer than 1 year, expiry
//     surprises are annoying, and the trust is scoped to one user's
//     keychain so the wide window is acceptable.
//   - CommonName + Organization carry "educates" so the cert is
//     visually identifiable in browser cert UIs.
//   - KeyUsageCertSign + IsCA + BasicConstraintsValid so cert-manager
//     can sign downstream leaf certs.
func GenerateSelfSignedCA(commonName string) (certPEM, keyPEM []byte, err error) {
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, nil, fmt.Errorf("generate RSA key: %w", err)
	}

	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return nil, nil, fmt.Errorf("generate serial: %w", err)
	}

	now := time.Now().UTC()
	tmpl := &x509.Certificate{
		SerialNumber: serial,
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

	derBytes, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &priv.PublicKey, priv)
	if err != nil {
		return nil, nil, fmt.Errorf("create certificate: %w", err)
	}

	certPEM = pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: derBytes})

	keyDER, err := x509.MarshalPKCS8PrivateKey(priv)
	if err != nil {
		return nil, nil, fmt.Errorf("marshal private key: %w", err)
	}
	keyPEM = pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER})

	return certPEM, keyPEM, nil
}

// IsCACertificatePEM reports whether the first certificate in certPEM is a
// CA certificate (basic constraints present with CA set). Used to tell a
// cached CA Secret apart from a cached wildcard TLS Secret, since both are
// kubernetes.io/tls Secrets carrying tls.crt and tls.key.
func IsCACertificatePEM(certPEM []byte) bool {
	block, _ := pem.Decode(certPEM)
	if block == nil || block.Type != "CERTIFICATE" {
		return false
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return false
	}
	return cert.BasicConstraintsValid && cert.IsCA
}
