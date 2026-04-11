package renderer

import (
	"os"
	"path"

	"github.com/educates/educates-training-platform/client-programs/pkg/utils"
	"github.com/pkg/errors"
)

func GenerateAccessToken(refresh bool) (string, error) {
	configFileDir := utils.GetEducatesHomeDir()
	accessTokenFile := path.Join(configFileDir, "live-reload-token.dat")

	err := os.MkdirAll(configFileDir, os.ModePerm)

	if err != nil {
		return "", errors.Wrapf(err, "unable to create config directory")
	}

	var accessToken string

	if refresh {
		accessToken = utils.RandomPassword(32)

		err := os.WriteFile(accessTokenFile, []byte(accessToken), 0644)

		if err != nil {
			return "", err
		}
	} else {
		if _, err := os.Stat(accessTokenFile); err == nil {
			accessTokenBytes, err := os.ReadFile(accessTokenFile)

			if err != nil {
				return "", err
			}

			accessToken = string(accessTokenBytes)
		} else if os.IsNotExist(err) {
			accessToken = utils.RandomPassword(32)

			err = os.WriteFile(accessTokenFile, []byte(accessToken), 0644)

			if err != nil {
				return "", err
			}
		} else {
			return "", err
		}
	}

	return accessToken, nil
}
