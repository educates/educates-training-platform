package cmd

import (
	"context"
	"io"

	"github.com/educates/educates-training-platform/client-programs/pkg/config"
	"github.com/educates/educates-training-platform/client-programs/pkg/deployer/progress"
	"github.com/educates/educates-training-platform/client-programs/pkg/localclusters"
	"github.com/educates/educates-training-platform/client-programs/pkg/resolver"
	"github.com/educates/educates-training-platform/client-programs/pkg/utils"
)

// localResolverConfig renders the configuration of the local DNS resolver,
// which serves the domains of every local cluster of the inventory inv,
// with the local clusters of given served from the configs given for
// them. It fails when two clusters claim the same domain.
func localResolverConfig(inv localclusters.Inventory, given ...localclusters.ResolverInput) ([]byte, error) {
	return inv.ResolverConfig(given...)
}

// observedResolverConfig is localResolverConfig for the local clusters
// observed on the host now.
func observedResolverConfig(ctx context.Context, given ...localclusters.ResolverInput) ([]byte, error) {
	inv, err := localClusterInventory(ctx)
	if err != nil {
		return nil, err
	}
	return localResolverConfig(inv, given...)
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
		return resolver.UpdateResolver(io.Discard, content)
	})
}

// defaultResolverConfig loads the default local cluster's config as the
// local resolver commands take it: from configPath when it is set, and
// otherwise from the data home, which must hold one. domain, when set,
// is served in place of the config's ingress domain.
func defaultResolverConfig(configPath, domain string) (localclusters.ResolverInput, error) {
	id := defaultLocalCluster()
	path := configPath
	if path == "" {
		path = id.ConfigPath
		if err := config.EnsureLocalConfigFile(utils.GetEducatesHomeDir()); err != nil {
			return localclusters.ResolverInput{}, err
		}
	}
	cfg, err := config.Load(path)
	if err != nil {
		return localclusters.ResolverInput{}, err
	}
	return localclusters.ResolverInput{
		LoadedConfig:           localclusters.LoadedConfig{Identity: id, Config: cfg, Source: path},
		ResolverDomainOverride: domain,
	}, nil
}
