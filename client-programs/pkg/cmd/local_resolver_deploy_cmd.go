package cmd

import (
	"context"
	"io"

	"github.com/spf13/cobra"

	"github.com/educates/educates-training-platform/client-programs/pkg/resolver"
)

var localResolverDeployExample = `
  # Deploy the local DNS resolver for every local cluster:
  educates local resolver deploy

  # Deploy using a specific config file for the default local cluster:
  educates local resolver deploy --config config.yaml

  # Override the default local cluster's ingress domain from the config:
  educates local resolver deploy --domain mydomain.test
`

// localResolverLong describes what the local DNS resolver serves, for the
// commands that deploy and update it.
const localResolverLong = `The local DNS resolver is one dnsmasq container, listening on
127.0.0.1:53, that serves the domains of every local cluster. It has no
settings of its own: each cluster contributes its names, its ingress
domain and resolver.extraDomains, which resolve to that cluster's
cluster.listenAddress when that is a specific IP address, and otherwise
to the host's IP address. A cluster with no ingress.domain is served the
nip.io name of its listen address, or for the default local cluster, of
the host's IP address.

The default local cluster is served from <data-home>/config.yaml, or
from the file --config names. A named local cluster is served from
<data-home>/clusters/<name>/config.yaml while its kind cluster exists.
Two local clusters claiming the same domain is an error, but a
subdomain of another cluster's domain, such as test2.educates.test
beside educates.test, resolves to its own cluster.

Names that resolve to anything other than a local cluster, such as a VM,
go in dnsmasq configuration files of your own, <data-home>/dnsmasq.d/*.conf,
which the resolver mounts and reads after every cluster's entries. The
command line tool creates the folder and never writes into it.

Creating or deleting a local cluster refreshes a running resolver.
'educates local resolver view' shows every name it answers for.`

type LocalResolverDeployOptions struct {
	Config      string
	LocalConfig bool
	Domain      string
}

// Run deploys the local DNS resolver serving every local cluster, and
// writes to errW a warning for each named local cluster's config it
// leaves out.
func (o *LocalResolverDeployOptions) Run(ctx context.Context, errW io.Writer) error {
	def, err := defaultResolverConfig(o.Config, o.Domain)
	if err != nil {
		return err
	}
	content, leftOut, err := observedResolverConfig(ctx, def)
	if err != nil {
		return err
	}
	warnLeftOutConfigs(errW, leftOut)
	return resolver.DeployResolver(content)
}

func (p *ProjectInfo) NewLocalResolverDeployCmd() *cobra.Command {
	var o LocalResolverDeployOptions

	c := &cobra.Command{
		Args:  cobra.NoArgs,
		Use:   "deploy",
		Short: "Deploys a local DNS resolver (macOS)",
		Long: "Deploys the local DNS resolver. When it already runs, it is left as it is: " +
			"'educates local resolver update' serves the current configs.\n\n" + localResolverLong,
		Example: localResolverDeployExample,
		RunE:    func(cmd *cobra.Command, _ []string) error { return o.Run(cmd.Context(), cmd.ErrOrStderr()) },
	}
	c.Flags().StringVarP(&o.Config, "config", "c", "", "path to a CLI config file (any kind) to serve the default local cluster from")
	c.Flags().BoolVar(&o.LocalConfig, "local-config", false, "serve the default local cluster from <data-home>/config.yaml (default when --config is not given)")
	c.Flags().StringVar(&o.Domain, "domain", "", "override the default local cluster's ingress.domain from the config")
	c.MarkFlagsMutuallyExclusive("config", "local-config")
	return c
}
