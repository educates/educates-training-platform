package secrets

import (
	"context"
	"os"
	"path"
	"strings"

	"github.com/educates/educates-training-platform/client-programs/pkg/constants"
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

var secretsCacheDir = path.Join(utils.GetEducatesHomeDir(), "secrets")

const secretsNS = "educates-secrets"

func LocalCachedSecretForIngressDomain(domain string) string {
	files, err := os.ReadDir(secretsCacheDir)

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
	files, err := os.ReadDir(secretsCacheDir)

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

			// Type of secret needs to be Opaque.
			if secretObj.Type != "Opaque" && secretObj.Type != "" {
				continue
			}

			// Needs contain ca.crt data.
			if value, exists := secretObj.Data["ca.crt"]; !exists || len(value) == 0 {
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
	err := os.MkdirAll(secretsCacheDir, os.ModePerm)

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

		namespacesClient.Create(context.TODO(), &namespaceObj, metav1.CreateOptions{})
	}

	secretsClient := client.CoreV1().Secrets(secretsNS)

	files, err := os.ReadDir(secretsCacheDir)

	if err != nil {
		return errors.Wrapf(err, "unable to read secrets cache directory %q", secretsCacheDir)
	}

	for _, f := range files {
		if strings.HasSuffix(f.Name(), ".yaml") {
			name := strings.TrimSuffix(f.Name(), ".yaml")
			secretObj, err := decodeFileIntoSecret(f.Name())
			if err != nil {
				return err
			}

			secretObj.ObjectMeta.Namespace = ""

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

				_, err = secretsClient.Apply(context.TODO(), patch, metav1.ApplyOptions{FieldManager: constants.DefaultPortalName, Force: true})

				if err != nil {
					return errors.Wrapf(err, "unable to update secret in cluster %q", name)
				}
			}
		}
	}

	return nil
}

func decodeFileIntoSecret(fileName string) (*apiv1.Secret, error) {
	fullPath := path.Join(secretsCacheDir, fileName)

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
