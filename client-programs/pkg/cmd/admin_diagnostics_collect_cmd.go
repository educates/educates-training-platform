package cmd

import (
	"os"
	"path/filepath"

	"github.com/educates/educates-training-platform/client-programs/pkg/cluster"
	"github.com/educates/educates-training-platform/client-programs/pkg/diagnostics"
	"github.com/mitchellh/go-homedir"
	"github.com/spf13/cobra"
)

var adminDiagnosticsCollectExample = `
  # Collect cluster diagnostics into the default archive:
  educates admin diagnostics collect

  # Collect into a specific path:
  educates admin diagnostics collect --dest ./educates-diagnostics.tar.gz
`

type AdminDiagnosticsCollectOptions struct {
	KubeconfigOptions
	Dest    string
	Verbose bool
}

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
		Args:    cobra.NoArgs,
		Use:     "collect",
		Short:   "Collect diagnostic information for an Educates cluster",
		Example: adminDiagnosticsCollectExample,
		RunE:    func(_ *cobra.Command, _ []string) error { return o.Run() },
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
	addLocalClusterContextFlag(c, &o.KubeconfigOptions)

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
