package cmd

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"

	"github.com/spf13/cobra"

	"github.com/educates/educates-training-platform/client-programs/pkg/config"
	"github.com/educates/educates-training-platform/client-programs/pkg/localclusters"
	"github.com/educates/educates-training-platform/client-programs/pkg/utils"
)

var localResolverViewExample = `
  # Show every name the local DNS resolver answers for:
  educates local resolver view
`

func (p *ProjectInfo) NewLocalResolverViewCmd() *cobra.Command {
	var c = &cobra.Command{
		Args:  cobra.NoArgs,
		Use:   "view",
		Short: "Shows what the local DNS resolver serves",
		Long: `Shows what the local DNS resolver serves, from the configs it is rendered
from: the default local cluster's config in the data home, and the config
of every named local cluster whose kind cluster exists.

The first table has a row for each local cluster, with the address its
names resolve to, its listen address, and its config. The second has a
row for each name, with its cluster and where it comes from: the ingress
domain, the nip.io name of the listen address, resolver.extraDomains, or
an address= line of a *.conf file of your own in <data-home>/dnsmasq.d.
Its notes say when a name is covered by a broader name of the same
cluster, when it wins over another cluster's broader name, when one of
your files names a domain a cluster also serves, and, on macOS, when no
/etc/resolver file forwards it to the resolver.

Configs the resolver leaves out are listed after the tables.`,
		Example: localResolverViewExample,
		RunE: func(cmd *cobra.Command, _ []string) error {
			inventory, err := localClusterInventory(cmd.Context())
			if err != nil {
				return err
			}
			includes, err := readResolverIncludeFiles(config.DnsmasqIncludeDir(utils.GetEducatesHomeDir()))
			if err != nil {
				return err
			}
			view, err := inventory.ResolverView(includes, etcResolverFileExists())
			if err != nil {
				return err
			}
			printLocalResolverView(cmd.OutOrStdout(), cmd.ErrOrStderr(), view)
			return nil
		},
	}

	return c
}

// readResolverIncludeFiles reads the files of the include folder dir that
// the local DNS resolver's configuration reads, as
// localclusters.ReadsIncludeFile says. A missing folder holds none.
func readResolverIncludeFiles(dir string) ([]localclusters.IncludeFile, error) {
	entries, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read the local DNS resolver include folder: %w", err)
	}
	var files []localclusters.IncludeFile
	for _, e := range entries {
		if !e.Type().IsRegular() || !localclusters.ReadsIncludeFile(e.Name()) {
			continue
		}
		content, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			return nil, fmt.Errorf("read the local DNS resolver include file: %w", err)
		}
		files = append(files, localclusters.IncludeFile{Name: e.Name(), Content: content})
	}
	return files, nil
}

// etcResolverFileExists returns, on macOS, a check for a file in
// /etc/resolver for a domain, which forwards the domain's names to the
// local DNS resolver. Elsewhere there is no such folder, and it returns
// nil.
func etcResolverFileExists() func(domain string) bool {
	if runtime.GOOS != "darwin" {
		return nil
	}
	return func(domain string) bool {
		info, err := os.Stat(filepath.Join("/etc/resolver", domain))
		return err == nil && !info.IsDir()
	}
}

// printLocalResolverView writes the tables of the local DNS resolver's
// clusters and names to w, and to errW a warning for each config the
// resolver leaves out.
func printLocalResolverView(w, errW io.Writer, view localclusters.ResolverView) {
	clusters := make([][]string, 0, len(view.Clusters))
	for _, r := range view.Clusters {
		clusters = append(clusters, []string{r.Cluster, r.Address, r.Config})
	}
	fmt.Fprintln(w, utils.PrintTable([]string{"CLUSTER", "ADDRESS", "CONFIG"}, clusters))
	fmt.Fprintln(w)

	names := make([][]string, 0, len(view.Names))
	for _, r := range view.Names {
		names = append(names, []string{r.Name, r.Cluster, r.From, orDash(r.Note)})
	}
	fmt.Fprintln(w, utils.PrintTable([]string{"NAME", "CLUSTER", "FROM", "NOTE"}, names))

	warnLeftOutConfigs(errW, view.LeftOut)
}
