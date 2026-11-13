package localclusters

import (
	"errors"
	"io/fs"
)

// ConfiguredListenAddress returns the listen address the config of the
// local cluster id in the data home declares, the host address the
// cluster publishes its ports 80 and 443 on, as LoadedConfig.ListenAddress
// gives it from the config LoadConfig loads. The default cluster's
// EducatesLocalConfig gets its defaults, so one that declares no listen
// address gives 127.0.0.1. A missing config declares none and is no
// error.
func ConfiguredListenAddress(id Identity) (string, error) {
	loaded, err := LoadConfig(id)
	if errors.Is(err, fs.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return loaded.ListenAddress(), nil
}
