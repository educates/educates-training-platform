package cmd

import (
	"github.com/spf13/cobra"
	"k8s.io/kubectl/pkg/util/templates"
)

/*
Create Cobra command group for commands related to extension packages.
*/
func (p *ProjectInfo) NewPackageCmdGroup() *cobra.Command {
	var c = &cobra.Command{
		Use:     "package",
		Aliases: []string{"packages"},
		Short:   "Tools for working on extension packages",
	}

	// Use a command group as it allows us to dictate the order in which they
	// are displayed in the help message, as otherwise they are displayed in
	// sort order.

	commandGroups := templates.CommandGroups{
		{
			Message: "Available Commands:",
			Commands: []*cobra.Command{
				p.NewPackagePublishCmd(),
			},
		},
	}

	commandGroups.Add(c)

	templates.ActsAsRootCommand(c, []string{"--help"}, commandGroups...)

	return c
}
