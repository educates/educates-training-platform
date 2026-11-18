package cmd

import (
	"github.com/spf13/cobra"
	"k8s.io/kubectl/pkg/util/templates"
)

func (p *ProjectInfo) NewAdminPlatformCmdGroup() *cobra.Command {
	var c = &cobra.Command{
		Use:   "platform",
		Short: "Manage Educates installation",
	}

	// Use a command group as it allows us to dictate the order in which they
	// are displayed in the help message, as otherwise they are displayed in
	// sort order.

	commandGroups := templates.CommandGroups{
		{
			Message: "Available Commands:",
			Commands: []*cobra.Command{
				p.NewAdminPlatformDeployCmd(),
				p.NewAdminPlatformDeleteCmd(),
				p.NewAdminPlatformRenderCmd(),
				p.NewAdminPlatformExportCmd(),
				p.NewAdminPlatformHistoryCmd(),
			},
		},
	}

	commandGroups.Add(c)

	templates.ActsAsRootCommand(c, []string{"--help"}, commandGroups...)

	return c
}
