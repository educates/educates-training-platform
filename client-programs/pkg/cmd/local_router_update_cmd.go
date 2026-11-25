package cmd

import (
	"context"
	"fmt"
	"io"

	"github.com/spf13/cobra"

	"github.com/educates/educates-training-platform/client-programs/pkg/config"
	"github.com/educates/educates-training-platform/client-programs/pkg/deployer/progress"
	"github.com/educates/educates-training-platform/client-programs/pkg/localclusters"
	"github.com/educates/educates-training-platform/client-programs/pkg/utils"
)

var localRouterUpdateExample = `
  # Serve the local clusters' current configs from the router:
  educates local router update

  # Move the router to the alias 10.10.10.1, added to a network interface:
  educates local router update --address 10.10.10.1
`

// LocalRouterUpdateOptions are the options of local router update.
type LocalRouterUpdateOptions struct {
	// Address is the router address to set in router.yaml first, empty to
	// keep the one it declares.
	Address string
}

func (p *ProjectInfo) NewLocalRouterUpdateCmd() *cobra.Command {
	var o LocalRouterUpdateOptions

	c := &cobra.Command{
		Args:  cobra.NoArgs,
		Use:   "update",
		Short: "Updates the router every local cluster is served through",
		Long: `Renders the router's configuration again, from the configs of every local
cluster, and reloads the router, which keeps open connections, such as
workshop terminals, on the previous configuration until they close.

The router is re-created when it runs another image than the one this CLI
pins, publishes ports 80 and 443 on another address than the router
address, or has stopped, which drops every connection through it. It is
created when it is missing while a local cluster exists. Creating it
refuses to run on Docker Desktop 4.97 or later while "Allow privileged
port forwarding to external machines" is off, in Settings > Advanced.

With --address, update first sets the router address in
<data-home>/router.yaml, keeping the file's comments. The address must be
an IP address of a network interface of this host, and neither a loopback
nor the unspecified address. The local DNS resolver, when deployed, then
points every local cluster's names at it, and the router is re-created on
it, leaving the clusters running. A cluster whose domain is the nip.io name
of the router address takes the new name, and is deployed again for its
portals to answer on it.`,
		Example: localRouterUpdateExample,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return o.Run(cmd.Context(), cmd.OutOrStdout(), cmd.ErrOrStderr())
		},
	}
	c.Flags().StringVar(&o.Address, "address", "", "set the router address in router.yaml to this IP address of a network interface of this host first")
	return c
}

// Run updates the router of the data home's local clusters, writing what
// it does to w, and the warning about a Docker Desktop setting it can't
// read to errW.
func (o *LocalRouterUpdateOptions) Run(ctx context.Context, w, errW io.Writer) error {
	inventory, err := localClusterInventory(ctx)
	if err != nil {
		return err
	}
	return dockerRouterChange(utils.GetEducatesHomeDir(), errW).update(ctx, w, progress.New(w, 0, isStdoutTTY(w)), inventory,
		o.Address, func(rep progress.Reporter) {
			refreshLocalResolver(rep, func() ([]byte, []error, error) { return observedResolverConfig(ctx) })
		})
}

// update is local router update, given the inventory of the local
// clusters, writing what it does to w and its steps to rep.
//
// With address set, it first sets the router address in router.yaml, once
// the router address rules accept it, and when that changes the address,
// refreshes the local DNS resolver with refreshResolver, so every local
// cluster's names resolve to the new address. It then creates the router
// when it is missing while a local cluster exists, re-creates it as
// ensureRouter decides, and otherwise renders its configuration again and
// reloads it. Unlike refreshRouter, a reload that fails is an error.
// Last, it names each cluster whose domain changed with the router
// address, which is deployed again for its portals to answer on it.
func (rc routerChange) update(ctx context.Context, w io.Writer, rep progress.Reporter, inventory localclusters.Inventory,
	address string, refreshResolver func(progress.Reporter)) error {
	var before []localclusters.ListRow
	if address != "" {
		before = inventory.List()
		previous, err := inventory.SetRouterAddress(address)
		if err != nil {
			return err
		}
		current, err := inventory.RouterAddress()
		if err != nil {
			return err
		}
		path := config.LocalRouterPath(rc.dataHome)
		switch previous {
		case current:
			fmt.Fprintf(w, "The router address in %s is %s already.\n", path, current)
			before = nil
		case "":
			fmt.Fprintf(w, "Wrote %s, with the router address %s.\n", path, current)
			refreshResolver(rep)
		default:
			fmt.Fprintf(w, "Set the router address in %s to %s, in place of %s.\n", path, current, previous)
			refreshResolver(rep)
		}
	}

	if err := rc.updateRouter(ctx, w, rep, inventory); err != nil {
		return err
	}

	if before != nil {
		writeRedeployNotes(w, before, inventory.List())
	}
	return nil
}

// updateRouter creates, re-creates or reloads the router as update says,
// as steps of rep. With no router and no local cluster, it says so on w,
// and creates nothing.
func (rc routerChange) updateRouter(ctx context.Context, w io.Writer, rep progress.Reporter, inventory localclusters.Inventory) error {
	if _, exists := inventory.Router(); !exists && len(inventory.Clusters()) == 0 {
		fmt.Fprintln(w, "There is no local cluster, so the router is not created: 'educates local cluster create' "+
			"creates it with the first local cluster.")
		return nil
	}
	changed, err := rc.ensureRouter(ctx, rep, inventory)
	if err != nil || changed {
		return err
	}
	var notes []string
	defer func() { noteAll(rep, notes) }()
	return runStep(rep, "reloading router", "reloaded", func(s progress.Step) error {
		notes, err = rc.serveRoutes(ctx, s, inventory, true)
		return err
	})
}

// writeRedeployNotes writes to w a note for each local cluster whose
// domain changed from its row of before to its row of after, the list
// rows of the local clusters before and after the router address changed:
// a domain that is the nip.io name of the router address follows it, and
// the cluster's portals answer on the new domain once it is deployed
// again.
func writeRedeployNotes(w io.Writer, before, after []localclusters.ListRow) {
	domains := map[string]string{}
	for _, r := range before {
		domains[r.Identity.KindName] = r.Domain
	}
	for _, r := range after {
		previous, listed := domains[r.Identity.KindName]
		if !listed || previous == "" || r.Domain == "" || previous == r.Domain {
			continue
		}
		selection := ""
		if !r.Identity.IsDefault() {
			selection = " --" + localclusters.SelectionFlagName + " " + r.Identity.Name
		}
		fmt.Fprintf(w, "Note: the domain of %s is now %s, in place of %s. Deploy it again for its portals to answer on it:\n\n"+
			"  educates admin platform deploy --local-config%s\n\n", r.Identity.Description(), r.Domain, previous, selection)
	}
}
