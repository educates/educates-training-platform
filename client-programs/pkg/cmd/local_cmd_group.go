package cmd

import (
	"github.com/spf13/cobra"
	"k8s.io/kubectl/pkg/util/templates"
)

func (p *ProjectInfo) NewLocalCmdGroup() *cobra.Command {
	var c = &cobra.Command{
		Use:   "local",
		Short: "Tools for working with Educates on your local computer",
	}

	// Use a command group as it allows us to dictate the order in which they
	// are displayed in the help message, as otherwise they are displayed in
	// sort order.

	commandGroups := templates.CommandGroups{
		{
			Message: "Available Commands:",
			Commands: []*cobra.Command{
				p.NewLocalClusterCmdGroup(),
				p.NewLocalConfigCmdGroup(),
				p.NewLocalSecretsCmdGroup(),
				p.NewLocalRegistryCmdGroup(),
				p.NewLocalMirrorCmdGroup(),
				p.NewLocalResolverCmdGroup(),
			},
		},
	}

	commandGroups.Add(c)

	templates.ActsAsRootCommand(c, []string{"--help"}, commandGroups...)

	return c
}
