package cmd

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/educates/educates-training-platform/client-programs/pkg/deployer/progress"
	"github.com/educates/educates-training-platform/client-programs/pkg/localclusters"
	"github.com/educates/educates-training-platform/client-programs/pkg/registry"
)

var (
	localMirrorDeleteExample = `
  # Delete a local image registry mirror
  educates local mirror delete mymirror
`
)

type LocalMirrorDeleteOptions struct {
	MirrorName string
}

func (o *LocalMirrorDeleteOptions) Run() error {
	mirrorConfig := &registry.MirrorConfig{
		Mirror: o.MirrorName,
	}

	return stepOnStdout(false, "delete registry mirror "+o.MirrorName, "deleted", func(s progress.Step) error {
		// Every local cluster shares the mirror, so it is unlinked from
		// every node of every one of them.
		inventory, err := localClusterInventory(context.Background())
		if err != nil {
			return err
		}
		if err := registry.DeleteMirrorAndUnlinkFromCluster(mirrorConfig, inventory.RegistryNodes(), s); err != nil {
			return err
		}
		// Starting or creating a cluster whose config declares the mirror
		// creates it again.
		if declaring := inventory.ClustersDeclaringMirror(o.MirrorName); len(declaring) > 0 {
			s.Warn(mirrorDeclaredWarning(o.MirrorName, declaring))
		}
		return nil
	})
}

// mirrorDeclaredWarning is the warning local mirror delete gives about
// the registry mirror mirror when the configs of the local clusters ids
// declare it, since the next start or create of each of them creates the
// mirror again.
func mirrorDeclaredWarning(mirror string, ids []localclusters.Identity) string {
	if len(ids) == 1 {
		return fmt.Sprintf("the config of %s declares registry mirror %s, so the next 'educates local cluster start' "+
			"of that cluster, or 'educates local cluster create' once it is deleted, creates the mirror again. "+
			"Remove the mirror from its cluster.registryMirrors to keep it deleted",
			localclusters.DescribeClusters(ids), mirror)
	}
	return fmt.Sprintf("the configs of %s declare registry mirror %s, so the next 'educates local cluster start' "+
		"of each of those clusters, or 'educates local cluster create' once it is deleted, creates the mirror again. "+
		"Remove the mirror from their cluster.registryMirrors to keep it deleted",
		localclusters.DescribeClusters(ids), mirror)
}

func (p *ProjectInfo) NewLocalMirrorDeleteCmd() *cobra.Command {
	var o LocalMirrorDeleteOptions

	var c = &cobra.Command{
		Args:  exactArgs(1, "mirror name is required", "NAME"),
		Use:   "delete NAME",
		Short: "Deletes the local image registry mirror",
		Long: `Deletes the registry mirror's container, which every local cluster shares,
and unlinks the mirror from every node of every running local cluster.

A local cluster whose config declares the mirror in cluster.registryMirrors
creates it again on its next start, or on its next create once deleted, so
the command warns, naming those clusters.`,
		RunE:    func(_ *cobra.Command, args []string) error { o.MirrorName = args[0]; return o.Run() },
		Example: localMirrorDeleteExample,
	}

	return c
}
