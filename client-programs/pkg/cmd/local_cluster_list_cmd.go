package cmd

import (
	"fmt"
	"io"
	"strconv"

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
named one, whatever --local-cluster or EDUCATES_LOCAL_CLUSTER select.

The router address, the host address in <data-home>/router.yaml where the
router serves every local cluster, comes first. Each row then has the
cluster's state (running, stopped, or partly running when only some of its
nodes run), its number of Kubernetes nodes, running or not, the Kubernetes
version of its node image, its ingress domain, which resolves to the
router address, and its kube context. list reads no cluster's API server:
the state, nodes and version come from the node containers as Docker
reports them, so a stopped cluster shows them too. Run educates local
cluster status to see each node of a cluster, and educates local router
status to see where the router sends each name.

A kind cluster named educates-<name> is a local cluster only when the
data home holds its folder, clusters/<name>/. Other kind clusters are left
out.`,
		Example: localClusterListExample,
		RunE: func(cmd *cobra.Command, _ []string) error {
			inventory, err := localClusterInventory(cmd.Context())
			if err != nil {
				return err
			}
			printLocalClusterList(cmd.OutOrStdout(), cmd.ErrOrStderr(), inventory)
			return nil
		},
	}

	// list takes --local-cluster as every local cluster command does, and
	// lists every local cluster whichever it selects.
	addLocalClusterFlag(c)

	return c
}

// printLocalClusterList writes to w the router address, then the table
// of the local clusters of inventory, and to errW a warning when the
// router address does not load, and for each cluster whose config does
// not load.
func printLocalClusterList(w, errW io.Writer, inventory localclusters.Inventory) {
	rows := inventory.List()
	if len(rows) == 0 {
		fmt.Fprintln(w, "No local clusters found.")
		return
	}

	address, err := inventory.RouterAddress()
	fmt.Fprintf(w, "Router address: %s\n\n", orDash(address))
	if err != nil {
		fmt.Fprintf(errW, "Warning: %v\n", err)
	}

	table := make([][]string, 0, len(rows))
	for _, r := range rows {
		name := r.Identity.Name
		if r.Identity.IsDefault() {
			name = "(default)"
		}
		table = append(table, []string{
			name, string(r.State), strconv.Itoa(r.Nodes), orDash(r.KubernetesVersion),
			orDash(r.Domain), r.Identity.Context,
		})
	}
	fmt.Fprintln(w, utils.PrintTable(
		[]string{"NAME", "STATE", "NODES", "KUBERNETES", "DOMAIN", "CONTEXT"}, table))

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
