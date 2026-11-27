package cmd

import (
	"fmt"
	"io"

	"github.com/spf13/cobra"

	"github.com/educates/educates-training-platform/client-programs/pkg/localclusters"
	"github.com/educates/educates-training-platform/client-programs/pkg/router"
	"github.com/educates/educates-training-platform/client-programs/pkg/utils"
)

var localRouterStatusExample = `
  # Show the router and where it sends each name:
  educates local router status
`

func (p *ProjectInfo) NewLocalRouterStatusCmd() *cobra.Command {
	var c = &cobra.Command{
		Args:  cobra.NoArgs,
		Use:   "status",
		Short: "Shows the router every local cluster is served through",
		Long: `Shows the router, the container educates-router that publishes ports 80
and 443 on the router address for every local cluster, and where it sends
each name. The router also publishes port 6443 there, and sends each
connection to the API server of the cluster whose control-plane node its
TLS server name names, such as educates-control-plane for the default
local cluster.

It shows the router address, from <data-home>/router.yaml, whether the
router runs, the address and the ports it publishes, and its image. When
the router is missing, has stopped, does not publish port 6443, or runs
another image or on another address than this CLI wants, it says so, and
that 'educates local router update' creates or re-creates it.

The table has a row for each name the router sends to a local cluster,
from the configs of every local cluster that exists, running or not: the
cluster's ingress domain, then its resolver.extraDomains. A name reaches
its cluster with every name under it, the most specific name winning. TLS
says how the router serves the name on port 443: passthrough sends the
connection to the cluster untouched, which presents its own certificate;
terminated ends TLS at the router, with a certificate from the local
secrets cache, and sends the requests on as plain HTTP, for a cluster
with ingress.externalTLSTermination; none serves the name on port 80 only,
for the extra domains of such a cluster, which the router's certificate
does not cover. STATE is whether the cluster runs: the router answers a
stopped cluster's names on port 80, and on 443 when TLS ends at the
router, with a 503 naming the command that starts it.`,
		Example: localRouterStatusExample,
		RunE: func(cmd *cobra.Command, _ []string) error {
			inventory, err := localClusterInventory(cmd.Context())
			if err != nil {
				return err
			}
			status, err := inventory.RouterStatus(router.Image)
			if err != nil {
				return err
			}
			printLocalRouterStatus(cmd.OutOrStdout(), cmd.ErrOrStderr(), status)
			return nil
		},
	}

	return c
}

// printLocalRouterStatus writes status to w: the router address, the
// router container and its image, what to run when the router does not
// serve the local clusters as it should, and the table of routes. It
// writes to errW a warning when the router address can't serve local
// clusters, and for each local cluster the router leaves out.
func printLocalRouterStatus(w, errW io.Writer, status localclusters.RouterStatus) {
	routerState := string(status.State)
	if status.State != localclusters.RouterMissing {
		routerState = fmt.Sprintf("%s (%s)", status.State, router.ContainerName)
	}
	if status.State == localclusters.RouterRunning {
		if status.PublishedAddress == "" {
			routerState += ", publishing neither port 80 nor port 443"
		} else {
			routerState += ", publishing " + localclusters.DescribePorts(status.PublishedPorts) + " on " + status.PublishedAddress
		}
	}
	fmt.Fprintln(w, utils.PrintKeyValuesTable([][]string{
		{"Router address", orDash(status.Address)},
		{"Router", routerState},
		{"Image", status.Image},
	}))
	fmt.Fprintln(w)
	if status.Advice != "" {
		fmt.Fprintln(w, status.Advice)
		fmt.Fprintln(w)
	}

	if len(status.Routes) == 0 {
		fmt.Fprintln(w, "The router routes no local cluster.")
	} else {
		table := make([][]string, 0, len(status.Routes))
		for _, r := range status.Routes {
			cluster := r.Cluster.Name
			if r.Cluster.IsDefault() {
				cluster = "(default)"
			}
			table = append(table, []string{orDash(r.Domain), cluster, string(r.TLS), string(r.ClusterState)})
		}
		fmt.Fprintln(w, utils.PrintTable([]string{"NAME", "CLUSTER", "TLS", "STATE"}, table))
	}

	if status.AddressError != nil {
		fmt.Fprintf(errW, "Warning: %v\n", status.AddressError)
	}
	for _, err := range status.LeftOut {
		fmt.Fprintf(errW, "Warning: %v\n", err)
	}
}
