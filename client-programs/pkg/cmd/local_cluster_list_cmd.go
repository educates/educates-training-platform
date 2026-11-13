package cmd

import (
	"fmt"
	"io"

	"github.com/spf13/cobra"

	"github.com/educates/educates-training-platform/client-programs/pkg/localclusters"
	"github.com/educates/educates-training-platform/client-programs/pkg/utils"
)

var localClusterListExample = `
  # List every local cluster, the default one and the named ones:
  educates local cluster list
`

func (p *ProjectInfo) NewLocalClusterListCmd() *cobra.Command {
	var c = &cobra.Command{
		Args:  cobra.NoArgs,
		Use:   "list",
		Short: "List the local Kubernetes clusters",
		Long: `Lists every local cluster: the default one, shown as (default), and every
named one, whatever --local-cluster or EDUCATES_LOCAL_CLUSTER select. Each
row has the cluster's state (running, stopped, or partly running when only
some of its nodes run), the host IP it publishes ports 80 and 443 on, its
ingress domain and its kube context.

A kind cluster named educates-<name> is a local cluster only when the
data home holds its folder, clusters/<name>/. Other kind clusters are left
out.`,
		Example: localClusterListExample,
		RunE: func(cmd *cobra.Command, _ []string) error {
			inventory, err := localClusterInventory(cmd.Context())
			if err != nil {
				return err
			}
			printLocalClusterList(cmd.OutOrStdout(), cmd.ErrOrStderr(), inventory.List())
			return nil
		},
	}

	c.Flags().String(localclusters.SelectionFlagName, "",
		"ignored: list shows every local cluster, whichever is selected")

	return c
}

// printLocalClusterList writes the table of local clusters rows to w, and
// to errW a warning for each cluster whose config does not load.
func printLocalClusterList(w, errW io.Writer, rows []localclusters.ListRow) {
	if len(rows) == 0 {
		fmt.Fprintln(w, "No local clusters found.")
		return
	}

	table := make([][]string, 0, len(rows))
	for _, r := range rows {
		name := r.Identity.Name
		if r.Identity.IsDefault() {
			name = "(default)"
		}
		table = append(table, []string{
			name, string(r.State), orDash(r.ListenAddress), orDash(r.Domain), r.Identity.Context,
		})
	}
	fmt.Fprintln(w, utils.PrintTable([]string{"NAME", "STATE", "LISTEN IP", "DOMAIN", "CONTEXT"}, table))

	for _, r := range rows {
		if r.ConfigError != nil {
			fmt.Fprintf(errW, "Warning: the config of %s does not load: %v\n", r.Identity.Description(), r.ConfigError)
		}
	}
}

// orDash returns s, or - when s is empty, for a table cell.
func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}
