package cmd

import (
	"context"

	"github.com/educates/educates-training-platform/client-programs/pkg/cluster"
	"github.com/educates/educates-training-platform/client-programs/pkg/config"
	"github.com/educates/educates-training-platform/client-programs/pkg/config/hostinfo"
	"github.com/educates/educates-training-platform/client-programs/pkg/deployer/progress"
	"github.com/educates/educates-training-platform/client-programs/pkg/localclusters"
	"github.com/educates/educates-training-platform/client-programs/pkg/resolver"
	"github.com/educates/educates-training-platform/client-programs/pkg/utils"
)

// localResolverConfig renders the configuration of the local DNS resolver,
// which serves the domains of every local cluster of the data home, with
// the local clusters of given served from the configs given for them. It
// fails when two clusters claim the same domain.
func localResolverConfig(ctx context.Context, given ...localclusters.ClusterConfig) ([]byte, error) {
	observed, err := cluster.ObserveNodeContainers(ctx)
	if err != nil {
		return nil, err
	}
	// Without a host IP address, the default local cluster is served at
	// the loopback address and gets no nip.io domain.
	hostIP, err := hostinfo.DetectHostIP()
	if err != nil {
		hostIP = ""
	}
	return localclusters.RenderResolverConfig(localclusters.ResolverInputs{
		DataHome: utils.GetEducatesHomeDir(),
		HostIP:   hostIP,
		Observed: observed,
		Given:    given,
	})
}

// refreshLocalResolver serves the configuration render returns from the
// local DNS resolver and restarts it, as a step of rep, printing nothing
// else. When the resolver container does not exist it does nothing, and
// renders nothing.
func refreshLocalResolver(rep progress.Reporter, render func() ([]byte, error)) error {
	exists, err := resolver.ResolverExists()
	if err != nil || !exists {
		return err
	}
	return runStep(rep, "refreshing local DNS resolver", "updated", func(progress.Step) error {
		content, err := render()
		if err != nil {
			return err
		}
		return resolver.RefreshResolver(content)
	})
}

// defaultResolverConfig loads the default local cluster's config as the
// local resolver commands take it: from configPath when it is set, and
// otherwise from the data home, which must hold one. domain, when set,
// is served in place of the config's ingress domain.
func defaultResolverConfig(configPath, domain string) (localclusters.ClusterConfig, error) {
	id := defaultLocalCluster()
	path := configPath
	if path == "" {
		path = id.ConfigPath
		if err := config.EnsureLocalConfigFile(utils.GetEducatesHomeDir()); err != nil {
			return localclusters.ClusterConfig{}, err
		}
	}
	cfg, err := config.Load(path)
	if err != nil {
		return localclusters.ClusterConfig{}, err
	}
	return localclusters.ClusterConfig{ID: id, Config: cfg, Source: path, Domain: domain}, nil
}
