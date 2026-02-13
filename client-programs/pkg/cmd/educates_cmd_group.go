package cmd

import (
	"bytes"
	"fmt"
	"regexp"
	"strings"

	"github.com/spf13/cobra"
	"k8s.io/kubectl/pkg/util/templates"
)

var (
	usageOptionsSuffixPattern = regexp.MustCompile(`(?m)^(\s*.+?) \[options\]\s*$`)
	globalOptionsHintPattern  = regexp.MustCompile(`(?m)^Use ".* options" for a list of global command-line options \(applies to all commands\)\.\n?`)
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

	c.AddCommand(p.NewProjectVersionCmd())
	configureRootHelpTemplates(c, []string{"--help"}, commandGroups...)

	return c
}

// configureRootHelpTemplates preserves grouped command help output from cobra
// templates, while removing the synthetic [options] usage suffix and global
// options hint from displayed usage text.
func configureRootHelpTemplates(c *cobra.Command, filters []string, groups ...templates.CommandGroup) {
	templates.ActsAsRootCommand(c, filters, groups...)

	sanitizeCommandUsage(c)
}

func sanitizeCommandUsage(command *cobra.Command) {
	originalUsageFunc := command.UsageFunc()
	originalHelpFunc := command.HelpFunc()

	command.SetUsageFunc(func(cmd *cobra.Command) error {
		usageBuffer := bytes.NewBuffer(nil)

		originalErr := cmd.ErrOrStderr()
		cmd.SetErr(usageBuffer)
		defer cmd.SetErr(originalErr)

		if err := originalUsageFunc(cmd); err != nil {
			return err
		}

		_, err := fmt.Fprint(originalErr, sanitizeUsageOutput(usageBuffer.String()))
		return err
	})

	command.SetHelpFunc(func(cmd *cobra.Command, args []string) {
		helpBuffer := bytes.NewBuffer(nil)

		originalOut := cmd.OutOrStdout()
		cmd.SetOut(helpBuffer)
		defer cmd.SetOut(originalOut)

		originalHelpFunc(cmd, args)
		fmt.Fprint(originalOut, sanitizeUsageOutput(helpBuffer.String()))
	})

	for _, child := range command.Commands() {
		sanitizeCommandUsage(child)
	}
}

func sanitizeUsageOutput(output string) string {
	cleaned := usageOptionsSuffixPattern.ReplaceAllString(output, "$1")
	cleaned = globalOptionsHintPattern.ReplaceAllString(cleaned, "")

	for strings.Contains(cleaned, "\n\n\n") {
		cleaned = strings.ReplaceAll(cleaned, "\n\n\n", "\n\n")
	}

	return cleaned
}
