package secrets

import (
	"context"
	"os"
	"path"
	"strings"

	"github.com/educates/educates-training-platform/client-programs/pkg/utils"
	"github.com/pkg/errors"
	apiv1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	applycorev1 "k8s.io/client-go/applyconfigurations/core/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/kubectl/pkg/scheme"
)

// secretsCacheDir resolves the on-disk secrets cache directory at call
// time rather than at package-init, so $EDUCATES_CLI_DATA_HOME (and
// tests using t.Setenv) takes effect.
func secretsCacheDir() string {
	return path.Join(utils.GetEducatesHomeDir(), "secrets")
}

const secretsNS = "educates-secrets"

func LocalCachedSecretForIngressDomain(domain string) string {
	files, err := os.ReadDir(secretsCacheDir())

	if err != nil {
		return ""
	}

	for _, f := range files {
		if strings.HasSuffix(f.Name(), ".yaml") {
			name := strings.TrimSuffix(f.Name(), ".yaml")
			secretObj, err := decodeFileIntoSecret(f.Name())
			if err != nil {
				continue
			}

			annotations := secretObj.ObjectMeta.Annotations

			// Domain name must match.
			if val, found := annotations["training.educates.dev/domain"]; !found || val != domain {
				continue
			}

			// Type of secret needs to be kubernetes.io/tls.
			if secretObj.Type != "kubernetes.io/tls" {
				continue
			}

			// Needs contain tls.crt and tls.key data.
			if value, exists := secretObj.Data["tls.crt"]; !exists || len(value) == 0 {
				continue
			}

			if value, exists := secretObj.Data["tls.key"]; !exists || len(value) == 0 {
				continue
			}

			return name
		}
	}

	return ""
}

func LocalCachedSecretForCertificateAuthority(domain string) string {
	files, err := os.ReadDir(secretsCacheDir())

	if err != nil {
		return ""
	}

	for _, f := range files {
		if strings.HasSuffix(f.Name(), ".yaml") {
			name := strings.TrimSuffix(f.Name(), ".yaml")
			secretObj, err := decodeFileIntoSecret(f.Name())
			if err != nil {
				continue
			}

			annotations := secretObj.ObjectMeta.Annotations

			// Domain name must match.
			if val, found := annotations["training.educates.dev/domain"]; !found || val != domain {
				continue
			}

			// v4 CustomCA flow: cert-manager signs workshop certs from
			// this CA, so the Secret must carry both the CA cert and
			// its private key. v3's Opaque+ca.crt shape (cert only,
			// for trust distribution) is no longer supported — users
			// who want a non-signing CA reference switch to the
			// EducatesConfig escape hatch.
			if secretObj.Type != apiv1.SecretTypeTLS {
				continue
			}
			if v, ok := secretObj.Data["tls.crt"]; !ok || len(v) == 0 {
				continue
			}
			if v, ok := secretObj.Data["tls.key"]; !ok || len(v) == 0 {
				continue
			}

			return name
		}
	}

	return ""
}

/**
 * SyncSecretsToCluster copies secrets from the local cache to the cluster.
 */
func SyncLocalCachedSecretsToCluster(client *kubernetes.Clientset) error {
	err := os.MkdirAll(secretsCacheDir(), os.ModePerm)

	if err != nil {
		return errors.Wrapf(err, "unable to create secrets cache directory")
	}

	namespacesClient := client.CoreV1().Namespaces()

	_, err = namespacesClient.Get(context.TODO(), secretsNS, metav1.GetOptions{})

	if k8serrors.IsNotFound(err) {
		namespaceObj := apiv1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: secretsNS,
			},
		}

		// Surface RBAC / admission / transient errors here rather than
		// proceeding to write Secrets into a namespace that doesn't
		// exist — the subsequent secret writes would fail with a
		// confusing 'namespaces "educates-secrets" not found' that
		// masks the real cause (typically: kubeconfig user lacks
		// 'create namespaces').
		if _, err := namespacesClient.Create(context.TODO(), &namespaceObj, metav1.CreateOptions{}); err != nil && !k8serrors.IsAlreadyExists(err) {
			return errors.Wrapf(err, "unable to create namespace %q for local secrets sync", secretsNS)
		}
	} else if err != nil {
		return errors.Wrapf(err, "unable to check namespace %q for local secrets sync", secretsNS)
	}

	secretsClient := client.CoreV1().Secrets(secretsNS)

	files, err := os.ReadDir(secretsCacheDir())

	if err != nil {
		return errors.Wrapf(err, "unable to read secrets cache directory %q", secretsCacheDir())
	}

	for _, f := range files {
		if strings.HasSuffix(f.Name(), ".yaml") {
			name := strings.TrimSuffix(f.Name(), ".yaml")
			secretObj, err := decodeFileIntoSecret(f.Name())
			if err != nil {
				return err
			}

			secretObj.ObjectMeta.Namespace = ""

			// A CA Secret cached before NewCASecret wrote ca.crt holds
			// the CA only under tls.crt. Components read ca.crt, so
			// add it on the way into the cluster. Only CA certificates
			// qualify: a cached wildcard TLS Secret has the same shape
			// and must not have its leaf certificate presented as a CA.
			if secretObj.Type == apiv1.SecretTypeTLS && IsCACertificatePEM(secretObj.Data["tls.crt"]) {
				EnsureCACertificateKey(secretObj)
			}

			_, err = secretsClient.Get(context.TODO(), name, metav1.GetOptions{})

			// Create the secret if it doesn't exist.
			if err != nil {
				if !k8serrors.IsNotFound(err) {
					return errors.Wrap(err, "unable to read secrets from cluster")
				} else {
					_, err = secretsClient.Create(context.TODO(), secretObj, metav1.CreateOptions{})

					if err != nil {
						return errors.Wrapf(err, "unable to copy secret to cluster %q", name)
					}
				}
				// Update the secret if it does exist.
			} else {
				var patch *applycorev1.SecretApplyConfiguration

				if len(secretObj.StringData) != 0 {
					patch = applycorev1.Secret(name, secretsNS).WithType(secretObj.Type).WithStringData(secretObj.StringData)
				} else {
					patch = applycorev1.Secret(name, secretsNS).WithType(secretObj.Type).WithData(secretObj.Data)
				}

				_, err = secretsClient.Apply(context.TODO(), patch, metav1.ApplyOptions{FieldManager: "educates-cli", Force: true})

				if err != nil {
					return errors.Wrapf(err, "unable to update secret in cluster %q", name)
				}
			}
		}
	}

	return nil
}

func decodeFileIntoSecret(fileName string) (*apiv1.Secret, error) {
	fullPath := path.Join(secretsCacheDir(), fileName)

	yamlData, err := os.ReadFile(fullPath)
	if err != nil {
		return nil, errors.Wrapf(err, "unable to read secret file %q", fullPath)
	}

	decoder := serializer.NewCodecFactory(scheme.Scheme).UniversalDecoder()
	secretObj := &apiv1.Secret{}
	err = runtime.DecodeInto(decoder, yamlData, secretObj)
	if err != nil {
		return nil, errors.Wrapf(err, "unable to read secret file %q", fullPath)
	}
	return secretObj, nil
}
