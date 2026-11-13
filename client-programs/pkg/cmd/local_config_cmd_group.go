package cmd

import (
	"github.com/spf13/cobra"
	"k8s.io/kubectl/pkg/util/templates"

	"github.com/educates/educates-training-platform/client-programs/pkg/localclusters"
)

func (p *ProjectInfo) NewLocalConfigCmdGroup() *cobra.Command {
	var c = &cobra.Command{
		Use:   "config",
		Short: "Manage local configuration files",
		Long: "Manage local configuration files. This configuration will be used when creating a local cluster " +
			"using the 'educates local cluster create' command. By default it will use the nip.io wildcard domain " +
			"and Kind as the provider." + "\n" +
			"This configuration is saved in the Educates home directory." + "\n" +
			"--local-cluster <name>, or EDUCATES_LOCAL_CLUSTER, selects the named local cluster whose " +
			"configuration, in <data-home>/clusters/<name>/config.yaml, a command manages.",
	}

	// Use a command group as it allows us to dictate the order in which they
	// are displayed in the help message, as otherwise they are displayed in
	// sort order.

	commandGroups := templates.CommandGroups{
		{
			Message: "Available Commands:",
			Commands: []*cobra.Command{
				p.NewLocalConfigInitCmd(),
				p.NewLocalConfigGetCmd(),
				p.NewLocalConfigSetCmd(),
				p.NewLocalConfigEditCmd(),
				p.NewLocalConfigViewCmd(),
				p.NewLocalConfigExplainCmd(),
				p.NewLocalConfigDeleteCmd(),
			},
		},
	}

	commandGroups.Add(c)

	templates.ActsAsRootCommand(c, []string{"--help"}, commandGroups...)

	return c
}

// checkNamedConfigExists returns the error naming the ways to write a
// config when id is a named local cluster that has none, as
// localclusters.LoadConfig reports it. The config commands that read a
// config check it first, so a named cluster's missing config is reported
// the same way by each of them, including those that read the file
// without loading it.
func checkNamedConfigExists(id localclusters.Identity) error {
	if id.IsDefault() {
		return nil
	}
	return localclusters.CheckConfigExists(id)
}
