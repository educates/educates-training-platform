package cmd

import (
	"encoding/json"
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

const localSecretsAddCaExample = `
  # Create a CA secret
  educates local secrets add ca my-ca

  # Create a CA secret with a custom domain
  educates local secrets add ca my-ca --domain my-domain.com

  # Create a CA secret with a custom certificate file
  educates local secrets add ca my-ca --cert /path/to/ca.crt
`

type LocalSecretsAddCaOptions struct {
	CertFile      string
	IngressDomain string
}

func (o *LocalSecretsAddCaOptions) Run(name string) error {
	var err error
	var matched bool

	if matched, err = regexp.MatchString("^[a-z0-9]([.a-z0-9-]+)?[a-z0-9]$", name); err != nil {
		return errors.Wrapf(err, "regex match on secret name failed")
	}

	if !matched {
		return errors.New("invalid secret name")
	}

	var certificateFileData []byte

	if o.CertFile != "" {
		certificateFileData, err = os.ReadFile(o.CertFile)

		if err != nil {
			return errors.Wrapf(err, "failed to read certificate file %s", o.CertFile)
		}
	}

	secret := &apiv1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:        name,
			Annotations: map[string]string{},
		},
		// Type: "kubernetes.io/tls",
		Data: map[string][]byte{
			"ca.crt": certificateFileData,
		},
	}

	if o.IngressDomain != "" {
		secret.ObjectMeta.Annotations["training.educates.dev/domain"] = o.IngressDomain
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

func (p *ProjectInfo) NewLocalSecretsAddCaCmd() *cobra.Command {
	var o LocalSecretsAddCaOptions

	var c = &cobra.Command{
		Args:  func(cmd *cobra.Command, args []string) error {
			if len(args) != 1 {
				return utils.CmdError(cmd, "name is required", "NAME")
			}
			return nil
		},
		Use:   "ca NAME",
		Short: "Create a CA secret",
		RunE:  func(_ *cobra.Command, args []string) error { return o.Run(args[0]) },
		Example: localSecretsAddCaExample,
	}

	c.Flags().StringVar(
		&o.CertFile,
		"cert",
		"",
		"path to PEM encoded CA certificate",
	)
	c.Flags().StringVar(
		&o.IngressDomain,
		"domain",
		"",
		"wildcard ingress domain matching certificate",
	)

	c.MarkFlagsRequiredTogether("cert")

	return c
}
