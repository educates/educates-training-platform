package cmd

import (
	"github.com/spf13/cobra"
	"k8s.io/kubectl/pkg/util/templates"

	"github.com/educates/educates-training-platform/client-programs/pkg/docker"
)

// NewLocalRouterCmdGroup returns local router, the commands of the router
// every local cluster is served through. Like the other host services
// every local cluster shares, the router takes no --local-cluster.
func (p *ProjectInfo) NewLocalRouterCmdGroup() *cobra.Command {
	var c = &cobra.Command{
		Use:   "router",
		Short: "Manage the router every local cluster is served through",
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
				p.NewLocalRouterStatusCmd(),
				p.NewLocalRouterUpdateCmd(),
			},
		},
	}

	commandGroups.Add(c)

	templates.ActsAsRootCommand(c, []string{"--help"}, commandGroups...)

	return c
}
