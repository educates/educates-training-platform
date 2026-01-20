package cmd

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path"
	"regexp"

	"github.com/educates/educates-training-platform/client-programs/pkg/utils"
	"github.com/pkg/errors"
	"github.com/spf13/cobra"
	apiv1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/yaml"
)

const localSecretsAddDockerRegistryExample = `
  # Create a secret for use with Docker hub
  educates local secrets add docker-registry my-registry --docker-username my-username --docker-password my-password --docker-email my-email

  # Create a secret for use with GitHub Container Registry
  educates local secrets add docker-registry my-registry --docker-server https://ghcr.io --docker-username my-username --docker-password my-password --docker-email my-email
`

type LocalSecretsAddDockerRegistryOptions struct {
	Server   string
	Username string
	Password string
	Email    string
}

func (o *LocalSecretsAddDockerRegistryOptions) Run(name string) error {
	var err error
	var matched bool

	if matched, err = regexp.MatchString("^[a-z0-9]([.a-z0-9-]+)?[a-z0-9]$", name); err != nil {
		return errors.Wrapf(err, "regex match on secret name failed")
	}

	if !matched {
		return errors.New("invalid secret name")
	}

	authString := base64.StdEncoding.EncodeToString([]byte(fmt.Sprintf("%s:%s", o.Username, o.Password)))

	dockerConfig := map[string]interface{}{
		"auths": map[string]interface{}{
			o.Server: map[string]string{
				"username": o.Username,
				"password": o.Password,
				"email":    o.Email,
				"auth":     authString,
			},
		},
	}

	dockerConfigData, _ := json.Marshal(dockerConfig)

	secret := &apiv1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Type: "kubernetes.io/dockerconfigjson",
		Data: map[string][]byte{
			".dockerconfigjson": dockerConfigData,
		},
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

func (p *ProjectInfo) NewLocalSecretsAddDockerRegistryCmd() *cobra.Command {
	var o LocalSecretsAddDockerRegistryOptions

	var c = &cobra.Command{
		Args:  func(cmd *cobra.Command, args []string) error {
			if len(args) != 1 {
				return utils.CmdError(cmd, "name is required", "NAME")
			}
			return nil
		},
		Use:   "docker-registry NAME",
		Short: "Create a secret for use with a Docker registry",
		RunE:  func(_ *cobra.Command, args []string) error { return o.Run(args[0]) },
		Example: localSecretsAddDockerRegistryExample,
	}

	c.Flags().StringVar(
		&o.Server,
		"docker-server",
		"https://index.docker.io/v1/",
		"server location for docker registry",
	)
	c.Flags().StringVar(
		&o.Username,
		"docker-username",
		"",
		"username for docker registry authentication",
	)
	c.Flags().StringVar(
		&o.Password,
		"docker-password",
		"",
		"password for docker registry authentication",
	)
	c.Flags().StringVar(
		&o.Email,
		"docker-email",
		"",
		"email for docker registry",
	)

	c.MarkFlagsRequiredTogether("docker-username", "docker-password", "docker-email")

	return c
}
