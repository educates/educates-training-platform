package cmd

import (
	"github.com/spf13/cobra"
	"k8s.io/kubectl/pkg/util/templates"
)

func (p *ProjectInfo) NewAdminDiagnosticsCmdGroup() *cobra.Command {
	var c = &cobra.Command{
		Use:   "diagnostics",
		Short: "Diagnostic commands for the local Kubernetes cluster",
	}

	// Use a command group as it allows us to dictate the order in which they
	// are displayed in the help message, as otherwise they are displayed in
	// sort order.

	commandGroups := templates.CommandGroups{
		{
			Message: "Available Commands:",
			Commands: []*cobra.Command{
				p.NewAdminDiagnosticsCollectCmd(),
				p.NewAdminDiagnosticsAnalyzeCmd(),
			},
		},
	}

	commandGroups.Add(c)

	templates.ActsAsRootCommand(c, []string{"--help"}, commandGroups...)

	return c
}
