package cmd

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/educates/educates-training-platform/client-programs/pkg/cluster"
	"github.com/educates/educates-training-platform/client-programs/pkg/localclusters"
)

var localConfigDeleteExample = `
  # Delete the config of the named local cluster test2, after deleting
  # the cluster:
  educates local cluster delete --local-cluster test2
  educates local config delete --local-cluster test2
`

func (p *ProjectInfo) NewLocalConfigDeleteCmd() *cobra.Command {
	c := &cobra.Command{
		Args:  cobra.NoArgs,
		Use:   "delete",
		Short: "Delete a named local cluster's config from <data-home>/clusters/<name>",
		Long: `Deletes the folder <data-home>/clusters/<name> of the named local
cluster --local-cluster or EDUCATES_LOCAL_CLUSTER selects, which holds its
config and its kind config file.

It refuses while the cluster's kind cluster, educates-<name>, exists:
delete the cluster first with 'educates local cluster delete', which
keeps the config. It always refuses for the default local cluster, whose
config is <data-home>/config.yaml.`,
		Example: localConfigDeleteExample,
		RunE: func(cmd *cobra.Command, _ []string) error {
			id, err := selectedLocalCluster(cmd)
			if err != nil {
				return err
			}
			return runLocalConfigDelete(cmd.OutOrStdout(), id, cluster.NewKindClusterConfig("", id).ClusterExists)
		},
	}
	addLocalClusterFlag(c)
	return c
}

// runLocalConfigDelete deletes the folder of the named local cluster id,
// which holds its config and its kind config file. clusterExists reports
// whether the cluster's kind cluster exists.
func runLocalConfigDelete(w io.Writer, id localclusters.Identity, clusterExists func() (bool, error)) error {
	if id.IsDefault() {
		return fmt.Errorf("local config delete deletes a named local cluster's config, and never the default local cluster's; "+
			"select a named local cluster with --%s or %s", localclusters.SelectionFlagName, localclusters.SelectionEnvVar)
	}
	folder := filepath.Dir(id.ConfigPath)
	if _, err := os.Stat(folder); os.IsNotExist(err) {
		return fmt.Errorf("local cluster %q has no config to delete at %s", id.Name, folder)
	} else if err != nil {
		return fmt.Errorf("check for the config of local cluster %q: %w", id.Name, err)
	}
	exists, err := clusterExists()
	if err != nil {
		return fmt.Errorf("check whether kind cluster %s exists: %w", id.KindName, err)
	}
	if exists {
		return fmt.Errorf("kind cluster %s of local cluster %q still exists; delete it first with "+
			"'educates local cluster delete%s', which keeps the config", id.KindName, id.Name, localClusterFlagArg(id))
	}
	if err := os.RemoveAll(folder); err != nil {
		return fmt.Errorf("delete the config of local cluster %q: %w", id.Name, err)
	}
	fmt.Fprintf(w, "deleted %s\n", folder)
	return nil
}
