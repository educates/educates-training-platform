package utils

import (
	"path"

	"github.com/adrg/xdg"
	"github.com/educates/educates-training-platform/client-programs/pkg/constants"
)

func GetEducatesHomeDir() string {
	return path.Join(xdg.DataHome, constants.EducatesHomeDirName)
}
