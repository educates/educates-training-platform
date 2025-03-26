package cmd

import (
	"github.com/spf13/cobra"
	"k8s.io/kubectl/pkg/util/templates"
)

func (p *ProjectInfo) NewLocalResolverCmdGroup() *cobra.Command {
	var c = &cobra.Command{
		Use:   "resolver",
		Short: "Manage local DNS resolver",
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
			},
		},
	}

	commandGroups.Add(c)

	templates.ActsAsRootCommand(c, []string{"--help"}, commandGroups...)

	return c
}
