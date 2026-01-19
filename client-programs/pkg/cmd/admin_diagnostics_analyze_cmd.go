package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

type AdminDiagnosticsAnalyzeOptions struct {
	File string
	Dir  string
}

const adminDiagnosticsAnalyzeExample = `
  # Analyze diagnostic information for current Educates cluster in current directory
  educates admin diagnostics analyze --file ./diagnostics.tar.gz
`

func (o *AdminDiagnosticsAnalyzeOptions) Run() error {
	// clusterConfig := cluster.NewClusterConfig(o.Kubeconfig, "")

	// diagnostics := diagnostics.NewClusterDiagnostics(clusterConfig, o.Dest)

	// if err := diagnostics.Run(); err != nil {
	// 	return err
	// }

	return fmt.Errorf("Not implemented yet")
}

func (p *ProjectInfo) NewAdminDiagnosticsAnalyzeCmd() *cobra.Command {
	var o AdminDiagnosticsAnalyzeOptions

	var c = &cobra.Command{
		Args:  cobra.NoArgs,
		Use:   "analyze",
		Short: "Analyze diagnostic information for an Educates cluster",
		RunE:  func(_ *cobra.Command, _ []string) error { return o.Run() },
		Example: adminDiagnosticsAnalyzeExample,
	}

	c.Flags().StringVar(
		&o.File,
		"file",
		getDefaultFilename(),
		"Path to the diagnostics file is located",
	)

	c.Flags().StringVar(
		&o.Dir,
		"dir",
		"",
		"Path to the directory where the diagnostics files are located",
	)

	//c.MarkFlagRequired("file")

	return c
}
