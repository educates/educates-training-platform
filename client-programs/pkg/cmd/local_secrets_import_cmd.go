package cmd

import (
	"encoding/json"
	"os"
	"path"
	"regexp"
	"syscall"

	"github.com/educates/educates-training-platform/client-programs/pkg/utils"
	"github.com/pkg/errors"
	"github.com/spf13/cobra"
	apiv1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/kubectl/pkg/scheme"
	"sigs.k8s.io/yaml"
)

const localSecretsImportExample = `
  # Import secrets from a file
  educates local secrets import --file /path/to/secrets.yaml
`

type LocalSecretsImportOptions struct {
	File string
}

func (o *LocalSecretsImportOptions) Run() error {
	secretsCacheDir := path.Join(utils.GetEducatesHomeDir(), "secrets")

	err := os.MkdirAll(secretsCacheDir, os.ModePerm)

	if err != nil {
		return errors.Wrapf(err, "unable to create secrets cache directory")
	}

	data, err := os.ReadFile(o.File)

	if err != nil {
		return errors.Wrapf(err, "unable to read secrets file %q", o.File)
	}

	regex := regexp.MustCompile("\n?---\n?")

	for i, yamlData := range regex.Split(string(data), -1) {
		decoder := serializer.NewCodecFactory(scheme.Scheme).UniversalDecoder()
		secretObj := &apiv1.Secret{}
		err = runtime.DecodeInto(decoder, []byte(yamlData), secretObj)

		if err != nil {
			return errors.Wrapf(err, "unable to decode secret %q", i)
		}

		// Make sure that the namespace is cleared.

		secretObj.ObjectMeta.Namespace = ""

		// See if temporary file for secret already exists in secrets cache and
		// if it does remove it.

		name := secretObj.ObjectMeta.Name + ".yaml"
		secretFilePath := path.Join(secretsCacheDir, name)
		secretFilePathTmp := secretFilePath + ".tmp"

		err = os.Remove(secretFilePathTmp)

		if err != nil {
			e, ok := err.(*os.PathError)
			if !ok && e.Err != syscall.ENOENT {
				return errors.Wrapf(e, "unable to remove temporary secret file %q", secretFilePathTmp)
			}
		}

		// Now write the temporary secret file.

		secretData, err := json.MarshalIndent(&secretObj, "", "    ")

		if err != nil {
			return errors.Wrap(err, "failed to generate secret data")
		}

		secretData, err = yaml.JSONToYAML(secretData)

		if err != nil {
			return errors.Wrap(err, "failed to generate YAML data")
		}

		secretFile, err := os.OpenFile(secretFilePathTmp, os.O_RDWR|os.O_CREATE|os.O_TRUNC, os.ModePerm)

		if err != nil {
			return errors.Wrapf(err, "unable to create temporary secret file %q", secretFilePathTmp)
		}

		if _, err = secretFile.Write(secretData); err != nil {
			return errors.Wrapf(err, "unable to write temporary secret file %q", secretFilePathTmp)
		}

		if err = secretFile.Close(); err != nil {
			return errors.Wrapf(err, "unable to close temporary secret file %q", secretFilePathTmp)
		}

		// Now move tempoary file to real file name so replace it.

		if err = os.Rename(secretFilePathTmp, secretFilePath); err != nil {
			return errors.Wrapf(err, "unable to update secret file %q", secretFilePath)
		}
	}

	return nil
}

func (p *ProjectInfo) NewLocalSecretsImportCmd() *cobra.Command {
	var o LocalSecretsImportOptions

	var c = &cobra.Command{
		Args:  cobra.ArbitraryArgs,
		Use:   "import",
		Short: "Import secrets to the cache",
		RunE:  func(_ *cobra.Command, _ []string) error { return o.Run() },
		Example: localSecretsImportExample,
	}

	c.Flags().StringVarP(
		&o.File,
		"file",
		"f",
		"",
		"path to file of secrets to import",
	)

	c.MarkFlagRequired("file")

	return c
}
