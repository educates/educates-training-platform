package utils

import (
	"strings"

	"github.com/docker/docker/api/types/container"
)

func GetContainerName(container container.Summary) string {
	name := "unknown"

	if len(container.Names) > 0 {
		// Get the first name and strip the leading slash "/"
		name = strings.TrimPrefix(container.Names[0], "/")
	}

	return name
}
