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

var secretNameRegex = regexp.MustCompile(`^[a-z0-9]([.a-z0-9-]+)?[a-z0-9]$`)

func validateSecretName(name string) error {
	if !secretNameRegex.MatchString(name) {
		return errors.New("invalid secret name")
	}
	return nil
}

// iterateSecretFiles calls fn for every .yaml file in secretsCacheDir.
// Iteration stops early if fn returns a non-nil sentinel value; any other
// non-nil error is returned to the caller.
func iterateSecretFiles(fn func(name string, secret *apiv1.Secret) error) error {
	files, err := os.ReadDir(secretsCacheDir)
	if err != nil {
		return errors.Wrapf(err, "unable to read secrets cache directory %q", secretsCacheDir)
	}

	for _, f := range files {
		if !strings.HasSuffix(f.Name(), ".yaml") {
			continue
		}
		name := strings.TrimSuffix(f.Name(), ".yaml")
		secretObj, err := decodeFileIntoSecret(f.Name())
		if err != nil {
			continue
		}
		if err := fn(name, secretObj); err != nil {
			return err
		}
	}
	return nil
}

// secretHasKey returns true when key is present and non-empty in either the
// binary Data map or the plain-text StringData map.
func secretHasKey(secret *apiv1.Secret, key string) bool {
	if v, ok := secret.Data[key]; ok && len(v) > 0 {
		return true
	}
	if v, ok := secret.StringData[key]; ok && len(v) > 0 {
		return true
	}
	return false
}

func writeSecretToCache(name string, secret *apiv1.Secret) error {
	secretData, err := json.MarshalIndent(secret, "", "    ")
	if err != nil {
		return errors.Wrap(err, "failed to generate secret data")
	}

	secretData, err = yaml.JSONToYAML(secretData)
	if err != nil {
		return errors.Wrap(err, "failed to generate YAML data")
	}

	if err := os.MkdirAll(secretsCacheDir, os.ModePerm); err != nil {
		return errors.Wrapf(err, "unable to create secrets cache directory")
	}

	secretFilePath := path.Join(secretsCacheDir, name+".yaml")

	secretFile, err := os.OpenFile(secretFilePath, os.O_RDWR|os.O_CREATE|os.O_TRUNC, os.ModePerm)
	if err != nil {
		return errors.Wrapf(err, "unable to create secret file %s", secretFilePath)
	}
	defer secretFile.Close()

	if _, err := secretFile.Write(secretData); err != nil {
		return errors.Wrapf(err, "unable to write secret file %s", secretFilePath)
	}

	return nil
}

func LocalCachedSecretForIngressDomain(domain string) string {
	var found string
	iterateSecretFiles(func(name string, secret *apiv1.Secret) error { //nolint:errcheck
		annotations := secret.ObjectMeta.Annotations

		// Domain name must match.
		if val := annotations[constants.EducatesTrainingLabelAnnotationDomain]; val != domain {
			return nil
		}

		// Type of secret needs to be kubernetes.io/tls.
		if secret.Type != apiv1.SecretTypeTLS {
			return nil
		}

		// Needs to contain tls.crt and tls.key data.
		if !secretHasKey(secret, "tls.crt") || !secretHasKey(secret, "tls.key") {
			return nil
		}

		found = name
		return errors.New("stop") // sentinel to stop iteration
	})
	return found
}

func LocalCachedSecretForCertificateAuthority(domain string) string {
	var found string
	iterateSecretFiles(func(name string, secret *apiv1.Secret) error { //nolint:errcheck
		annotations := secret.ObjectMeta.Annotations

		// Domain name must match.
		if val := annotations[constants.EducatesTrainingLabelAnnotationDomain]; val != domain {
			return nil
		}

		// Type of secret needs to be Opaque (or unset).
		if secret.Type != apiv1.SecretTypeOpaque && secret.Type != "" {
			return nil
		}

		// Needs to contain ca.crt data.
		if !secretHasKey(secret, "ca.crt") {
			return nil
		}

		found = name
		return errors.New("stop") // sentinel to stop iteration
	})
	return found
}

/**
 * SyncSecretsToCluster copies secrets from the local cache to the cluster.
 */
func SyncLocalCachedSecretsToCluster(client *kubernetes.Clientset) error {
	if err := os.MkdirAll(secretsCacheDir, os.ModePerm); err != nil {
		return errors.Wrapf(err, "unable to create secrets cache directory")
	}

	namespacesClient := client.CoreV1().Namespaces()

	_, err := namespacesClient.Get(context.TODO(), constants.EducatesSecretsNamespace, metav1.GetOptions{})
	if k8serrors.IsNotFound(err) {
		namespaceObj := apiv1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: constants.EducatesSecretsNamespace,
			},
		}
		namespacesClient.Create(context.TODO(), &namespaceObj, metav1.CreateOptions{})
	}

	secretsClient := client.CoreV1().Secrets(constants.EducatesSecretsNamespace)

	return iterateSecretFiles(func(name string, secretObj *apiv1.Secret) error {
		secretObj.ObjectMeta.Namespace = ""

		_, err := secretsClient.Get(context.TODO(), name, metav1.GetOptions{})

		// Create the secret if it doesn't exist.
		if err != nil {
			if !k8serrors.IsNotFound(err) {
				return errors.Wrap(err, "unable to read secrets from cluster")
			}
			_, err = secretsClient.Create(context.TODO(), secretObj, metav1.CreateOptions{})
			if err != nil {
				return errors.Wrapf(err, "unable to copy secret to cluster %q", name)
			}
			return nil
		}

		// Update the secret if it does exist.
		var patch *applycorev1.SecretApplyConfiguration
		if len(secretObj.StringData) != 0 {
			patch = applycorev1.Secret(name, constants.EducatesSecretsNamespace).WithType(secretObj.Type).WithStringData(secretObj.StringData)
		} else {
			patch = applycorev1.Secret(name, constants.EducatesSecretsNamespace).WithType(secretObj.Type).WithData(secretObj.Data)
		}

		_, err = secretsClient.Apply(context.TODO(), patch, metav1.ApplyOptions{FieldManager: constants.DefaultPortalName, Force: true})
		if err != nil {
			return errors.Wrapf(err, "unable to update secret in cluster %q", name)
		}
		return nil
	})
}

func List() (string, error) {
	if err := os.MkdirAll(secretsCacheDir, os.ModePerm); err != nil {
		return "", errors.Wrapf(err, "unable to create secrets cache directory")
	}

	var data [][]string
	iterateSecretFiles(func(name string, secretObj *apiv1.Secret) error { //nolint:errcheck
		annotations := secretObj.ObjectMeta.Annotations
		domain := annotations[constants.EducatesTrainingLabelAnnotationDomain]
		secretObjType := secretType(secretObj.Type)
		dataKeys := secretDataKeys(secretObj.Data, secretObj.StringData)
		data = append(data, []string{name, secretObjType, dataKeys, domain})
		return nil
	})

	return utils.PrintTable([]string{"NAME", "TYPE", "KEYS", "DOMAIN"}, data), nil
}

func AddTLSSecret(name, certFile, keyFile, ingressDomain string, asString bool) error {
	if err := validateSecretName(name); err != nil {
		return err
	}

	var certificateFileData, certificateKeyFileData []byte
	var err error

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
		Type: apiv1.SecretTypeTLS,
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

	return writeSecretToCache(name, secret)
}

func AddCASecret(name, certFile, ingressDomain string, asString bool) error {
	if err := validateSecretName(name); err != nil {
		return err
	}

	var certificateFileData []byte
	var err error

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

	return writeSecretToCache(name, secret)
}

func AddRegistrySecret(name, server, username, password, email string, asString bool) error {
	if err := validateSecretName(name); err != nil {
		return err
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

	dockerConfigData, err := json.Marshal(dockerConfig)
	if err != nil {
		return errors.Wrap(err, "failed to generate docker config data")
	}

	secret := &apiv1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Type: apiv1.SecretTypeDockerConfigJson,
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

	return writeSecretToCache(name, secret)
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
	keys := make([]string, 0, len(d)+len(s))
	for k := range d {
		keys = append(keys, k)
	}
	for k := range s {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return strings.Join(keys, ", ")
}

func secretType(t apiv1.SecretType) string {
	switch t {
	case apiv1.SecretTypeTLS:
		return "TLS"
	case apiv1.SecretTypeDockerConfigJson:
		return "Registry"
	case apiv1.SecretTypeOpaque, "":
		return "Opaque"
	case apiv1.SecretTypeServiceAccountToken:
		return "Service Account Token"
	default:
		return string(t)
	}
}
