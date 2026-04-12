package secrets

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path"
	"regexp"
	"sort"
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
	"sigs.k8s.io/yaml"
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
			if val, found := annotations[constants.EducatesTrainingLabelAnnotationDomain]; !found || val != domain {
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
			if val, found := annotations[constants.EducatesTrainingLabelAnnotationDomain]; !found || val != domain {
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

func List() (string, error) {
	secretsCacheDir := path.Join(utils.GetEducatesHomeDir(), "secrets")

	err := os.MkdirAll(secretsCacheDir, os.ModePerm)

	if err != nil {
		return "", errors.Wrapf(err, "unable to create secrets cache directory")
	}

	files, err := os.ReadDir(secretsCacheDir)

	if err != nil {
		return "", errors.Wrapf(err, "unable to read secrets cache directory")
	}

	var data [][]string
	for _, f := range files {
		if strings.HasSuffix(f.Name(), ".yaml") {
			name := strings.TrimSuffix(f.Name(), ".yaml")
			secretObj, err := decodeFileIntoSecret(f.Name())
			if err != nil {
				continue
			}

			annotations := secretObj.ObjectMeta.Annotations
			domain := annotations[constants.EducatesTrainingLabelAnnotationDomain]
			secretObjType := secretType(secretObj.Type)
			dataKeys := secretDataKeys(secretObj.Data, secretObj.StringData)
			data = append(data, []string{name, secretObjType, dataKeys, domain})
		}
	}
	return utils.PrintTable([]string{"NAME", "TYPE", "KEYS", "DOMAIN"}, data), nil
}

func AddTLSSecret(name, certFile, keyFile, ingressDomain string, asString bool) error {
	var err error
	var matched bool

	if matched, err = regexp.MatchString("^[a-z0-9]([.a-z0-9-]+)?[a-z0-9]$", name); err != nil {
		return errors.Wrapf(err, "regex match on secret name failed")
	}

	if !matched {
		return errors.New("invalid secret name")
	}

	var certificateFileData []byte
	var certificateKeyFileData []byte

	if certFile != "" {
		certificateFileData, err = os.ReadFile(certFile)

		if err != nil {
			return errors.Wrapf(err, "failed to read certificate file %s", certFile)
		}
	}

	if keyFile != "" {
		certificateKeyFileData, err = os.ReadFile(keyFile)

		if err != nil {
			return errors.Wrapf(err, "failed to read certificate key file %s", keyFile)
		}
	}

	secret := &apiv1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:        name,
			Annotations: map[string]string{},
		},
		Type: "kubernetes.io/tls",
	}
	if asString {
		secret.StringData = map[string]string{
			"tls.crt": string(certificateFileData),
			"tls.key": string(certificateKeyFileData),
		}
	} else {
		secret.Data = map[string][]byte{
			"tls.crt": certificateFileData,
			"tls.key": certificateKeyFileData,
		}
	}

	if ingressDomain != "" {
		secret.ObjectMeta.Annotations[constants.EducatesTrainingLabelAnnotationDomain] = ingressDomain
	}

	secretData, err := json.MarshalIndent(&secret, "", "    ")

	if err != nil {
		return errors.Wrap(err, "failed to generate secret data")
	}

	secretData, err = yaml.JSONToYAML(secretData)

	if err != nil {
		return errors.Wrap(err, "failed to generate YAML data")
	}

	secretsCacheDir := path.Join(utils.GetEducatesHomeDir(), "secrets")

	err = os.MkdirAll(secretsCacheDir, os.ModePerm)

	if err != nil {
		return errors.Wrapf(err, "unable to create secrets cache directory")
	}

	secretFilePath := path.Join(secretsCacheDir, name+".yaml")

	secretFile, err := os.OpenFile(secretFilePath, os.O_RDWR|os.O_CREATE|os.O_TRUNC, os.ModePerm)

	if err != nil {
		return errors.Wrapf(err, "unable to create secret file %s", secretFilePath)
	}

	if _, err := secretFile.Write(secretData); err != nil {
		return errors.Wrapf(err, "unable to write secret file %s", secretFilePath)
	}

	if err := secretFile.Close(); err != nil {
		return errors.Wrapf(err, "unable to close secret file %s", secretFilePath)
	}

	return nil
}

func AddCASecret(name, certFile, ingressDomain string, asString bool) error {
	var err error
	var matched bool

	if matched, err = regexp.MatchString("^[a-z0-9]([.a-z0-9-]+)?[a-z0-9]$", name); err != nil {
		return errors.Wrapf(err, "regex match on secret name failed")
	}

	if !matched {
		return errors.New("invalid secret name")
	}

	var certificateFileData []byte

	if certFile != "" {
		certificateFileData, err = os.ReadFile(certFile)

		if err != nil {
			return errors.Wrapf(err, "failed to read certificate file %s", certFile)
		}
	}

	secret := &apiv1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:        name,
			Annotations: map[string]string{},
		},
		// Type: "kubernetes.io/tls",
	}
	if asString {
		secret.StringData = map[string]string{
			"ca.crt": string(certificateFileData),
		}
	} else {
		secret.Data = map[string][]byte{
			"ca.crt": certificateFileData,
		}
	}

	if ingressDomain != "" {
		secret.ObjectMeta.Annotations[constants.EducatesTrainingLabelAnnotationDomain] = ingressDomain
	}

	secretData, err := json.MarshalIndent(&secret, "", "    ")

	if err != nil {
		return errors.Wrap(err, "failed to generate secret data")
	}

	secretData, err = yaml.JSONToYAML(secretData)

	if err != nil {
		return errors.Wrap(err, "failed to generate YAML data")
	}

	secretsCacheDir := path.Join(utils.GetEducatesHomeDir(), "secrets")

	err = os.MkdirAll(secretsCacheDir, os.ModePerm)

	if err != nil {
		return errors.Wrapf(err, "unable to create secrets cache directory")
	}

	secretFilePath := path.Join(secretsCacheDir, name+".yaml")

	secretFile, err := os.OpenFile(secretFilePath, os.O_RDWR|os.O_CREATE|os.O_TRUNC, os.ModePerm)

	if err != nil {
		return errors.Wrapf(err, "unable to create secret file %s", secretFilePath)
	}

	if _, err := secretFile.Write(secretData); err != nil {
		return errors.Wrapf(err, "unable to write secret file %s", secretFilePath)
	}

	if err := secretFile.Close(); err != nil {
		return errors.Wrapf(err, "unable to close secret file %s", secretFilePath)
	}

	return nil
}

func AddRegistrySecret(name, server, username, password, email string, asString bool) error {
	var err error
	var matched bool

	if matched, err = regexp.MatchString("^[a-z0-9]([.a-z0-9-]+)?[a-z0-9]$", name); err != nil {
		return errors.Wrapf(err, "regex match on secret name failed")
	}

	if !matched {
		return errors.New("invalid secret name")
	}

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

	secret := &apiv1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Type: apiv1.SecretTypeDockerConfigJson, // Use the built-in constant
	}

	if asString {
		secret.StringData = map[string]string{
			".dockerconfigjson": string(dockerConfigData),
		}
	} else {
		secret.Data = map[string][]byte{
			".dockerconfigjson": dockerConfigData,
		}
	}

	secretData, err := json.MarshalIndent(&secret, "", "    ")

	if err != nil {
		return errors.Wrap(err, "failed to generate secret data")
	}

	secretData, err = yaml.JSONToYAML(secretData)

	if err != nil {
		return errors.Wrap(err, "failed to generate YAML data")
	}

	secretsCacheDir := path.Join(utils.GetEducatesHomeDir(), "secrets")

	err = os.MkdirAll(secretsCacheDir, os.ModePerm)

	if err != nil {
		return errors.Wrapf(err, "unable to create secrets cache directory")
	}

	secretFilePath := path.Join(secretsCacheDir, name+".yaml")

	secretFile, err := os.OpenFile(secretFilePath, os.O_RDWR|os.O_CREATE|os.O_TRUNC, os.ModePerm)

	if err != nil {
		return errors.Wrapf(err, "unable to create secret file %s", secretFilePath)
	}

	if _, err = secretFile.Write(secretData); err != nil {
		return errors.Wrapf(err, "unable to write secret file %s", secretFilePath)
	}

	if err := secretFile.Close(); err != nil {
		return errors.Wrapf(err, "unable to close secret file %s", secretFilePath)
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

func secretDataKeys(d map[string][]byte, s map[string]string) string {
	// 1. Collect all keys into a single slice
	// Pre-allocate memory for efficiency
	keys := make([]string, 0, len(d)+len(s))

	for k := range d {
		keys = append(keys, k)
	}
	for k := range s {
		keys = append(keys, k)
	}

	// 2. Sort the keys alphabetically
	// This makes the output deterministic and easier to read/test
	sort.Strings(keys)

	// 3. Join them with a comma and space
	return strings.Join(keys, ", ")
}

func secretType(t apiv1.SecretType) string {
	switch t {
	case apiv1.SecretTypeTLS:
		return "TLS"
	case apiv1.SecretTypeOpaque:
		return "Opaque"
	// You can easily add more cases as needed
	case apiv1.SecretTypeServiceAccountToken:
		return "Service Account Token"
	default:
		// Defaulting to Opaque is standard K8s behavior
		return "Opaque"
	}
}
