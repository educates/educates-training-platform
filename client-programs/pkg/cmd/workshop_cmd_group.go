package cmd

import (
	"github.com/spf13/cobra"
	"k8s.io/kubectl/pkg/util/templates"
)

/*
Create Cobra command group for commands related to workshops.
*/
func (p *ProjectInfo) NewWorkshopCmdGroup() *cobra.Command {
	var c = &cobra.Command{
		Use:     "workshop",
		Aliases: []string{"workshops"},
		Short:   "Tools for working on local workshop files",
	}

	// Use a command group as it allows us to dictate the order in which they
	// are displayed in the help message, as otherwise they are displayed in
	// sort order.

	commandGroups := templates.CommandGroups{
		{
			Message: "Available Commands:",
			Commands: []*cobra.Command{
				p.NewWorkshopNewCmd(),
				p.NewWorkshopPublishCmd(),
				p.NewWorkshopExportCmd(),
				p.NewWorkshopRenderCmd(),
			},
		},
	}

	commandGroups.Add(c)

	templates.ActsAsRootCommand(c, []string{"--help"}, commandGroups...)

	return c
}
