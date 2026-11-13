package localclusters

import (
	"errors"
	"io/fs"
	"os"

	"github.com/educates/educates-training-platform/client-programs/pkg/config"
	"github.com/educates/educates-training-platform/client-programs/pkg/config/v1alpha1"
)

// ConfiguredListenAddress returns the listen address the config of the
// local cluster id in the data home declares, the host address the
// cluster publishes its ports 80 and 443 on: cluster.listenAddress of an
// EducatesLocalConfig, or target.cluster.listenAddress of an
// EducatesConfig. A named cluster's config follows the rules of
// LoadNamedConfig. The default cluster's EducatesLocalConfig gets its
// defaults, so one that declares no listen address gives 127.0.0.1. A
// missing config declares none and is no error.
func ConfiguredListenAddress(id Identity) (string, error) {
	if _, err := os.Stat(id.ConfigPath); errors.Is(err, fs.ErrNotExist) {
		return "", nil
	}
	var cfg v1alpha1.Config
	var err error
	if id.IsDefault() {
		cfg, err = config.Load(id.ConfigPath)
	} else {
		cfg, err = LoadNamedConfig(id.ConfigPath)
	}
	if err != nil {
		return "", err
	}
	switch c := cfg.(type) {
	case *v1alpha1.EducatesLocalConfig:
		return c.Cluster.ListenAddress, nil
	case *v1alpha1.EducatesConfig:
		if c.Target != nil {
			return c.Target.Cluster.ListenAddress, nil
		}
	}
	return "", nil
}
