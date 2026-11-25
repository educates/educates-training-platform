package cmd

import (
	"fmt"
	"io"

	"github.com/spf13/cobra"

	"github.com/educates/educates-training-platform/client-programs/pkg/config"
	"github.com/educates/educates-training-platform/client-programs/pkg/localclusters"
	"github.com/educates/educates-training-platform/client-programs/pkg/utils"
)

// addIgnoredListenAddressFlag adds to c the --listen-address flag, hidden
// from its help, whose value goes to address. A local cluster has no
// address of its own, since every local cluster is served on the router
// address, so the flag is taken and ignored, with the warning
// warnIgnoredListenAddress gives.
func addIgnoredListenAddressFlag(c *cobra.Command, address *string) {
	c.Flags().StringVar(address, localclusters.ListenAddressFlagName, "",
		"ignored: every local cluster is served on the router address, which 'educates local router update --address <ip>' sets")
	_ = c.Flags().MarkHidden(localclusters.ListenAddressFlagName)
}

// warnIgnoredListenAddress tells w, when --listen-address was given the
// value address, that it is ignored, and where the address every local
// cluster is served on is set instead.
func warnIgnoredListenAddress(w io.Writer, address string) {
	if address == "" {
		return
	}
	fmt.Fprintf(w, "Warning: --%s %s is ignored: a local cluster has no address of its own, and every "+
		"local cluster is served through the router, on the router address in %s. Set it with "+
		"'educates local router update --address <ip>'.\n",
		localclusters.ListenAddressFlagName, address, config.LocalRouterPath(utils.GetEducatesHomeDir()))
}
