package secrets

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path"
	"regexp"
	"sort"
	"strings"

	"github.com/pkg/errors"
	apiv1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/yaml"
)

// secretNamePattern is the allowed shape of a local secret name: a
// DNS-label-ish string. Used by ValidateSecretName.
var secretNamePattern = regexp.MustCompile(`^[a-z0-9]([.a-z0-9-]+)?[a-z0-9]$`)

// ValidateSecretName reports whether name is a valid local secret name,
// returning a descriptive error (naming the offending value and the
// allowed pattern) when it is not. Commands call this from their cobra
// Args validator so an invalid name is rejected before any work runs.
func ValidateSecretName(name string) error {
	if !secretNamePattern.MatchString(name) {
		return errors.Errorf("invalid secret name %q (must match %s)", name, secretNamePattern.String())
	}
	return nil
}

// CacheDir returns the on-disk secrets cache directory, creating it if it
// does not yet exist. The path is resolved at call time so
// $EDUCATES_CLI_DATA_HOME (and tests using t.Setenv) take effect.
func CacheDir() (string, error) {
	dir := secretsCacheDir()
	if err := os.MkdirAll(dir, os.ModePerm); err != nil {
		return "", errors.Wrapf(err, "unable to create secrets cache directory")
	}
	return dir, nil
}

// WriteCachedSecret marshals secret to YAML and writes it to the cache as
// <name>.yaml, replacing any existing file of that name.
func WriteCachedSecret(secret *apiv1.Secret) error {
	dir, err := CacheDir()
	if err != nil {
		return err
	}

	secretData, err := json.MarshalIndent(secret, "", "    ")
	if err != nil {
		return errors.Wrap(err, "failed to generate secret data")
	}

	secretData, err = yaml.JSONToYAML(secretData)
	if err != nil {
		return errors.Wrap(err, "failed to generate YAML data")
	}

	secretFilePath := path.Join(dir, secret.ObjectMeta.Name+".yaml")
	if err := os.WriteFile(secretFilePath, secretData, os.ModePerm); err != nil {
		return errors.Wrapf(err, "unable to write secret file %q", secretFilePath)
	}

	return nil
}

// LoadCachedSecret reads and decodes the cached secret named name. It
// returns a clear "no secret named" error when the secret is not cached,
// so callers can surface that directly.
func LoadCachedSecret(name string) (*apiv1.Secret, error) {
	secretFilePath := path.Join(secretsCacheDir(), name+".yaml")

	if _, err := os.Stat(secretFilePath); err != nil {
		if os.IsNotExist(err) {
			return nil, errors.Errorf("no secret named %q in the local secrets cache", name)
		}
		return nil, errors.Wrapf(err, "unable to read secret file %q", secretFilePath)
	}

	return decodeFileIntoSecret(name + ".yaml")
}

// ListCachedSecretNames returns the names (without the .yaml suffix) of all
// secrets in the cache, sorted. The cache directory is created if needed.
func ListCachedSecretNames() ([]string, error) {
	dir, err := CacheDir()
	if err != nil {
		return nil, err
	}

	files, err := os.ReadDir(dir)
	if err != nil {
		return nil, errors.Wrapf(err, "unable to read secrets cache directory")
	}

	var names []string
	for _, f := range files {
		if strings.HasSuffix(f.Name(), ".yaml") {
			names = append(names, strings.TrimSuffix(f.Name(), ".yaml"))
		}
	}
	sort.Strings(names)

	return names, nil
}

// RemoveCachedSecret deletes the cached secret named name. Removing a
// secret that is not cached is not an error.
func RemoveCachedSecret(name string) error {
	secretFilePath := path.Join(secretsCacheDir(), name+".yaml")
	if err := os.Remove(secretFilePath); err != nil && !os.IsNotExist(err) {
		return errors.Wrapf(err, "unable to remove secret file %q", secretFilePath)
	}
	return nil
}

// NewTLSSecret builds a kubernetes.io/tls Secret from PEM-encoded cert and
// key bytes. When ingressDomain is non-empty it is recorded in the
// training.educates.dev/domain annotation so the lookup helpers can match
// the secret to a wildcard ingress domain.
func NewTLSSecret(name string, certPEM, keyPEM []byte, ingressDomain string) *apiv1.Secret {
	return newCertSecret(name, certPEM, keyPEM, ingressDomain)
}

// NewCASecret builds the Secret for a signing CA. In the v4 CustomCA flow
// cert-manager signs workshop certificates from this CA, so it carries both
// tls.crt and tls.key and is structurally a kubernetes.io/tls secret.
//
// It also carries the CA certificate under ca.crt. cert-manager reads the
// signing material from tls.crt and tls.key, but everything that only needs
// to trust the CA (the platform component start scripts, the workshop
// session and docker-in-docker CA injection in session-manager, the
// training portal) reads ca.crt, the key that means "trust anchor"
// regardless of how a Secret was built. For a self-signed CA the two
// certificate keys hold the same bytes, which is the shape cert-manager
// itself produces for a CA it issues from a SelfSigned issuer.
func NewCASecret(name string, certPEM, keyPEM []byte, ingressDomain string) *apiv1.Secret {
	secret := newCertSecret(name, certPEM, keyPEM, ingressDomain)
	secret.Data["ca.crt"] = certPEM
	return secret
}

// EnsureCACertificateKey adds a ca.crt entry, copied from tls.crt, to a CA
// Secret that lacks one. CA Secrets cached before ca.crt was written by
// NewCASecret, or built by hand as kubernetes.io/tls, hold the CA only
// under tls.crt; consumers read ca.crt. Returns true when the Secret was
// changed. A Secret with no tls.crt is left alone.
func EnsureCACertificateKey(secret *apiv1.Secret) bool {
	if secret == nil || secret.Data == nil {
		return false
	}
	if v, ok := secret.Data["ca.crt"]; ok && len(v) > 0 {
		return false
	}
	crt, ok := secret.Data["tls.crt"]
	if !ok || len(crt) == 0 {
		return false
	}
	secret.Data["ca.crt"] = crt
	return true
}

func newCertSecret(name string, certPEM, keyPEM []byte, ingressDomain string) *apiv1.Secret {
	secret := &apiv1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:        name,
			Annotations: map[string]string{},
		},
		Type: apiv1.SecretTypeTLS,
		Data: map[string][]byte{
			"tls.crt": certPEM,
			"tls.key": keyPEM,
		},
	}

	if ingressDomain != "" {
		secret.ObjectMeta.Annotations["training.educates.dev/domain"] = ingressDomain
	}

	return secret
}

// NewDockerRegistrySecret builds a kubernetes.io/dockerconfigjson Secret
// holding credentials for a single registry server.
func NewDockerRegistrySecret(name, server, username, password, email string) *apiv1.Secret {
	authString := base64.StdEncoding.EncodeToString([]byte(fmt.Sprintf("%s:%s", username, password)))

	dockerConfig := map[string]interface{}{
		"auths": map[string]interface{}{
			server: map[string]string{
				"username": username,
				"password": password,
				"email":    email,
				"auth":     authString,
			},
		},
	}

	dockerConfigData, _ := json.Marshal(dockerConfig)

	return &apiv1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Type: "kubernetes.io/dockerconfigjson",
		Data: map[string][]byte{
			".dockerconfigjson": dockerConfigData,
		},
	}
}
