package cmd

import (
	"strings"

	"github.com/spf13/cobra"
	"k8s.io/kubectl/pkg/util/templates"
)

/*
Create root Cobra command group for Educates CLI .
*/
func (p *ProjectInfo) NewEducatesCmdGroup() *cobra.Command {
	c := &cobra.Command{
		Use:   "educates",
		Short: "Tools for managing Educates",
	}

	// Use a command group as it allows us to dictate the order in which they
	// are displayed in the help message, as otherwise they are displayed in
	// sort order.

	overrideCommandName := func(c *cobra.Command, name string) *cobra.Command {
		c.Use = strings.Replace(c.Use, c.Name(), name, 1)
		return c
	}

	commandGroups := templates.CommandGroups{
		{
			Message: "Content Creation Commands (Aliases):",
			Commands: []*cobra.Command{
				overrideCommandName(p.NewWorkshopNewCmd(), "new-workshop"),
				overrideCommandName(p.NewWorkshopPublishCmd(), "publish-workshop"),
				overrideCommandName(p.NewWorkshopExportCmd(), "export-workshop"),
				overrideCommandName(p.NewWorkshopRenderCmd(), "render-workshop"),
			},
		},
		{
			Message: "Content Management Commands (Aliases):",
			Commands: []*cobra.Command{
				overrideCommandName(p.NewClusterWorkshopDeployCmd(), "deploy-workshop"),
				overrideCommandName(p.NewClusterWorkshopListCmd(), "list-workshops"),
				overrideCommandName(p.NewClusterWorkshopServeCmd(), "serve-workshop"),
				overrideCommandName(p.NewClusterWorkshopUpdateCmd(), "update-workshop"),
				overrideCommandName(p.NewClusterWorkshopDeleteCmd(), "delete-workshop"),

				overrideCommandName(p.NewClusterPortalOpenCmd(), "browse-workshops"),
				overrideCommandName(p.NewClusterPortalPasswordCmd(), "view-credentials"),

				overrideCommandName(p.NewClusterPortalCreateCmd(), "create-portal"),
				overrideCommandName(p.NewClusterPortalListCmd(), "list-portals"),
				overrideCommandName(p.NewClusterPortalDeleteCmd(), "delete-portal"),

				overrideCommandName(p.NewClusterSessionListCmd(), "list-sessions"),
				overrideCommandName(p.NewClusterSessionStatusCmd(), "session-status"),
				overrideCommandName(p.NewClusterSessionExtendCmd(), "extend-session"),
				overrideCommandName(p.NewClusterSessionTerminateCmd(), "delete-session"),
			},
		},
		{
			Message: "Management Commands (Aliases):",
			Commands: []*cobra.Command{
				overrideCommandName(p.NewLocalClusterCreateCmd(), "create-cluster"),
				overrideCommandName(p.NewLocalClusterDeleteCmd(), "delete-cluster"),
				overrideCommandName(p.NewAdminPlatformDeployCmd(), "deploy-platform"),
				overrideCommandName(p.NewAdminPlatformDeleteCmd(), "delete-platform"),
			},
		},
		{
			Message: "Command Groups:",
			Commands: []*cobra.Command{
				p.NewLocalCmdGroup(),
				p.NewAdminCmdGroup(),
				p.NewProjectCmdGroup(),
				p.NewWorkshopCmdGroup(),
				p.NewTemplateCmdGroup(),
				p.NewClusterCmdGroup(),
				p.NewDockerCmdGroup(),
				p.NewTunnelCmdGroup(),
			},
		},
	}

	commandGroups.Add(c)

	templates.ActsAsRootCommand(c, []string{"--help"}, commandGroups...)

	c.AddCommand(p.NewProjectVersionCmd())

	return c
}
