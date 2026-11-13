package cmd

import (
	"github.com/spf13/cobra"
	"k8s.io/kubectl/pkg/util/templates"

	"github.com/educates/educates-training-platform/client-programs/pkg/docker"
)

func (p *ProjectInfo) NewLocalResolverCmdGroup() *cobra.Command {
	var c = &cobra.Command{
		Use:   "resolver",
		Short: "Manage local DNS resolver",
		PersistentPreRunE: func(cmd *cobra.Command, _ []string) error {
			return docker.CheckDaemonRunning()
		},
	}

	// Use a command group as it allows us to dictate the order in which they
	// are displayed in the help message, as otherwise they are displayed in
	// sort order.

	commandGroups := templates.CommandGroups{
		{
			Message: "Available Commands:",
			Commands: []*cobra.Command{
				p.NewLocalResolverDeployCmd(),
				p.NewLocalResolverDeleteCmd(),
				p.NewLocalResolverUpdateCmd(),
				p.NewLocalResolverViewCmd(),
			},
		},
	}

	commandGroups.Add(c)

	templates.ActsAsRootCommand(c, []string{"--help"}, commandGroups...)

	return c
}
