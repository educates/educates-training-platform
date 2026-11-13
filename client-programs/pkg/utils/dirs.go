package utils

import (
	"os"
	"path"

	"github.com/adrg/xdg"
)

// EducatesCLIDataHomeEnv overrides the on-disk home for all Educates CLI
// state. When set and non-empty, it takes precedence over the default
// xdg.DataHome/educates location. Useful for CI and tests.
//
// A second data home is not a way to run several local clusters side by
// side: it still creates the default kind cluster, binds the same host
// ports and manages the same registry, resolver and mirror containers as
// the first. Named local clusters, which share one data home, run side by
// side instead.
const EducatesCLIDataHomeEnv = "EDUCATES_CLI_DATA_HOME"

// GetEducatesHomeDir returns the on-disk home for all Educates CLI state
// (the default local cluster's config.yaml and kind config file,
// clusters/ for named local clusters, secrets/, dnsmasq.conf, compose/,
// workshops/).
//
// Resolution order:
//  1. $EDUCATES_CLI_DATA_HOME if set and non-empty.
//  2. $XDG_DATA_HOME/educates/ (default).
func GetEducatesHomeDir() string {
	if v := os.Getenv(EducatesCLIDataHomeEnv); v != "" {
		return v
	}
	return path.Join(xdg.DataHome, "educates")
}
