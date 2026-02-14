package cmd

import (
	"github.com/educates/educates-training-platform/client-programs/pkg/cluster"
	"github.com/educates/educates-training-platform/client-programs/pkg/constants"
	educatesResources "github.com/educates/educates-training-platform/client-programs/pkg/educates/resources"
	"github.com/educates/educates-training-platform/client-programs/pkg/utils"
	"github.com/pkg/errors"
	"github.com/spf13/cobra"
)

type ClusterPortalExportOptions struct {
	KubeconfigOptions
	Portal  string
	AsFiles string
}


const clusterPortalExportExample = `
# Export TrainingPortal and its workshops to stdout as YAML documents
educates cluster portal export

# Export a specific TrainingPortal and workshops to stdout
educates cluster portal export --portal=my-portal

# Export YAML documents as files in a directory
educates cluster portal export --portal=my-portal --as-files=./export
`

func (o *ClusterPortalExportOptions) Run() error {
	var err error

	if o.Portal == "" {
		o.Portal = constants.DefaultPortalName
	}

	clusterConfig, err := cluster.NewClusterConfigIfAvailable(o.Kubeconfig, o.Context)
	if err != nil {
		return err
	}

	dynamicClient, err := clusterConfig.GetDynamicClient()
	if err != nil {
		return errors.Wrap(err, "unable to create Kubernetes client")
	}

	manager := educatesResources.NewPortalManager(dynamicClient)

	documents, err := manager.GetTrainingPortalYAMLDocumentsForExport(&educatesResources.TrainingPortalExportConfig{
		Portal: o.Portal,
	})
	if err != nil {
		return err
	}

	if o.AsFiles != "" {
		return utils.WriteExportedDocuments(o.AsFiles, documents)
	}

	return utils.PrintExportedDocuments(documents)
}



func (p *ProjectInfo) NewClusterPortalExportCmd() *cobra.Command {
	var o ClusterPortalExportOptions

	var c = &cobra.Command{
		Args:    cobra.NoArgs,
		Use:     "export",
		Short:   "Export portal resources from Kubernetes",
		RunE:    func(_ *cobra.Command, _ []string) error { return o.Run() },
		Example: clusterPortalExportExample,
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
		&o.Portal,
		"portal",
		"p",
		constants.DefaultPortalName,
		"name to be used for training portal and workshop name prefixes",
	)
	c.Flags().StringVar(
		&o.AsFiles,
		"as-files",
		"",
		"write YAML resources as files in target directory instead of stdout",
	)

	return c
}
