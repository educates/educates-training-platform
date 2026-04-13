package cmd

import (
	"os"
	"path/filepath"

	"github.com/educates/educates-training-platform/client-programs/pkg/cluster"
	"github.com/educates/educates-training-platform/client-programs/pkg/diagnostics"
	"github.com/mitchellh/go-homedir"
	"github.com/spf13/cobra"
)

type AdminDiagnosticsCollectOptions struct {
	KubeconfigOptions
	Dest    string
	Verbose bool
}

const adminDiagnosticsCollectExample = `
  # Collect diagnostic information for current Educates cluster in current directory
  educates admin diagnostics collect

  # Collect diagnostic information ffor current Educates cluster in current directory with verbose output
  educates admin diagnostics collect --verbose

  # Collect diagnostic information for an Educates cluster and save to a specific directory
  educates admin diagnostics collect --dest ./diagnostics

  # Collect diagnostic information for a specific Educates Cluster in current directory
  educates admin diagnostics collect --kubeconfig /path/to/kubeconfig --context my-cluster
`

func (o *AdminDiagnosticsCollectOptions) Run() error {
	clusterConfig := cluster.NewClusterConfig(o.Kubeconfig, o.Context)

	diagnostics := diagnostics.NewClusterDiagnostics(clusterConfig, o.Dest, o.Verbose)

	if err := diagnostics.Run(); err != nil {
		return err
	}

	return nil
}

func (p *ProjectInfo) NewAdminDiagnosticsCollectCmd() *cobra.Command {
	var o AdminDiagnosticsCollectOptions

	var c = &cobra.Command{
		Args:  cobra.NoArgs,
		Use:   "collect",
		Short: "Collect diagnostic information for an Educates cluster",
		RunE:  func(_ *cobra.Command, _ []string) error { return o.Run() },
		Example: adminDiagnosticsCollectExample,
	}

	c.Flags().StringVar(
		&o.Kubeconfig,
		"kubeconfig",
		"",
		"kubeconfig file to use instead of $KUBECONFIG or $HOME/.kube/config",
	)

	c.Flags().StringVar(
		&o.Context,
		"context",
		"",
		"Context to use from Kubeconfig",
	)

	c.Flags().StringVar(
		&o.Dest,
		"dest",
		getDefaultFilename(),
		"Path to the directory where the diagnostics files will be generated",
	)

	c.Flags().BoolVar(
		&o.Verbose,
		"verbose",
		false,
		"print verbose output",
	)
	// c.MarkFlagRequired("dest")

	return c
}

func getDefaultFilename() string {
	dir, err := os.Getwd()
	if err != nil {
		dir, err = homedir.Dir()
		if err != nil {
			dir = os.TempDir()
		}
	}
	return filepath.Join(dir, "educates-diagnostics.tar.gz")
}
