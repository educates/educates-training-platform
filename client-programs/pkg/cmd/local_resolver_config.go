package cmd

import (
	"context"
	"fmt"
	"io"

	"github.com/educates/educates-training-platform/client-programs/pkg/config"
	"github.com/educates/educates-training-platform/client-programs/pkg/deployer/progress"
	"github.com/educates/educates-training-platform/client-programs/pkg/localclusters"
	"github.com/educates/educates-training-platform/client-programs/pkg/resolver"
	"github.com/educates/educates-training-platform/client-programs/pkg/utils"
)

// observedResolverConfig renders the configuration of the local DNS
// resolver, as localclusters.Inventory.ResolverConfig does, for the local
// clusters observed on the host now, with the local clusters of given
// served from the configs given for them. It also returns the configs of
// other clusters it leaves out, for the caller to warn about.
func observedResolverConfig(ctx context.Context, given ...localclusters.ResolverInput) ([]byte, []error, error) {
	inv, err := localClusterInventory(ctx)
	if err != nil {
		return nil, nil, err
	}
	return inv.ResolverConfig(given...)
}

// warnLeftOutConfigs writes to w a warning for each config of another
// local cluster that the local DNS resolver leaves out.
func warnLeftOutConfigs(w io.Writer, leftOut []error) {
	for _, err := range leftOut {
		fmt.Fprintf(w, "Warning: %v\n", err)
	}
}

// resolverContainer is how a refresh of the local DNS resolver reaches
// its container.
type resolverContainer struct {
	// exists reports whether the container exists.
	exists func() (bool, error)

	// update serves a configuration from the container and restarts it,
	// and reports whether the container mounts the include folder.
	update func(content []byte) (includeFolderMounted bool, err error)
}

// dockerResolverContainer is the local DNS resolver container on the
// Docker host.
var dockerResolverContainer = resolverContainer{
	exists: resolver.ResolverExists,
	update: func(content []byte) (bool, error) { return resolver.UpdateResolver(io.Discard, content) },
}

// refreshLocalResolver serves the configuration render returns from the
// local DNS resolver and restarts it, as refreshResolverContainer does
// for the container on the Docker host.
func refreshLocalResolver(rep progress.Reporter, render func() ([]byte, []error, error)) {
	refreshResolverContainer(rep, dockerResolverContainer, render)
}

// refreshResolverContainer serves the configuration render returns from
// the local DNS resolver container c and restarts it, as a step of rep,
// printing nothing else. When the container does not exist it does
// nothing, and renders nothing.
//
// It never fails. Local cluster create and delete refresh the resolver
// once the kind cluster exists, or is gone, so the step warns about each
// config render leaves out, and a resolver it can't look up, render or
// restart is a warning that says to run 'educates local resolver update'.
// A container without the include folder is updated, with a warning that
// says how to re-create it.
func refreshResolverContainer(rep progress.Reporter, c resolverContainer, render func() ([]byte, []error, error)) {
	exists, err := c.exists()
	if err == nil && !exists {
		return
	}
	step := rep.Start("refreshing local DNS resolver")
	if err == nil {
		var content []byte
		var leftOut []error
		content, leftOut, err = render()
		for _, e := range leftOut {
			step.Warn(e.Error())
		}
		if err == nil {
			var includeFolderMounted bool
			includeFolderMounted, err = c.update(content)
			if err == nil && !includeFolderMounted {
				step.Warn(resolverWithoutIncludeFolderWarning())
			}
		}
	}
	if err != nil {
		step.Warn(fmt.Sprintf("not refreshed: %v. Run 'educates local resolver update' once that is fixed", err))
		step.Done("skipped")
		return
	}
	step.Done("updated")
}

// resolverWithoutIncludeFolderWarning says that the local DNS resolver
// container, created by an earlier release, does not mount the include
// folder, so it serves none of the user's own entries, and how to
// re-create it.
func resolverWithoutIncludeFolderWarning() string {
	return fmt.Sprintf("the local DNS resolver container was created without the include folder %s, so it serves "+
		"none of your own entries there: run 'educates local resolver delete' then 'educates local resolver deploy' "+
		"to pick up dnsmasq.d/", config.DnsmasqIncludeDir(utils.GetEducatesHomeDir()))
}

// defaultResolverConfig loads the default local cluster's config as the
// local resolver commands take it, by the rules of a local cluster's
// config: from configPath when it is set, and otherwise from the data
// home, which must hold one. domain, when set,
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
	cfg, err := localclusters.LoadConfigFile(id, path)
	if err != nil {
		return localclusters.ResolverInput{}, err
	}
	return localclusters.ResolverInput{
		LoadedConfig:           localclusters.LoadedConfig{Identity: id, Config: cfg, Source: path},
		ResolverDomainOverride: domain,
	}, nil
}
