package cmd

import (
	"fmt"
	"os"

	"github.com/educates/educates-training-platform/client-programs/pkg/lookup"
	"github.com/pkg/errors"
	"github.com/spf13/cobra"
)

type LookupConfigOptions struct {
	KubeconfigOptions
	OutputPath string
}

const adminLookupKubeconfigExample = `
  # Fetch kubeconfig for lookup service remote access
  educates admin lookup kubeconfig

  # Fetch kubeconfig for lookup service remote access and save to a specific file
  educates admin lookup kubeconfig --output ./lookup-kubeconfig.yaml

  # Fetch kubeconfig for lookup service remote access for a specific cluster
  educates admin lookup kubeconfig --kubeconfig /path/to/kubeconfig --context my-cluster
`

func (p *ProjectInfo) NewAdminLookupKubeconfigCmd() *cobra.Command {
	var o LookupConfigOptions

	var c = &cobra.Command{
		Args:  cobra.NoArgs,
		Use:   "kubeconfig",
		Short: "Fetch kubeconfig for lookup service remote access",
		RunE: func(cmd *cobra.Command, _ []string) error {
			config := lookup.LookupConfig{
				Kubeconfig: o.Kubeconfig,
				Context: o.Context,
			}
			kubeconfig, err := lookup.NewLookupService().RemoteAccessKubeconfig(&config)
			if err != nil {
				return err
			}
			if o.OutputPath != "" {
				err = os.WriteFile(o.OutputPath, []byte(kubeconfig), 0644)

				if err != nil {
					return errors.Wrapf(err, "unable to write kubeconfig to %s", o.OutputPath)
				}
			} else {
				fmt.Print(kubeconfig)
			}

			return nil
		},
		Example: adminLookupKubeconfigExample,
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
	c.Flags().StringVarP(
		&o.OutputPath,
		"output",
		"o",
		"",
		"Path to write Kubeconfig file to",
	)

	return c
}
