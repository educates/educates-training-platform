package cmd

import (
	"context"

	"github.com/pkg/errors"
	"github.com/spf13/cobra"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/dynamic"

	"github.com/educates/educates-training-platform/client-programs/pkg/cluster"
	"github.com/educates/educates-training-platform/client-programs/pkg/utils"
)

var clusterPortalExportExample = `
  # Export a training portal and its workshops as YAML to stdout:
  educates cluster portal export

  # Export a specific training portal:
  educates cluster portal export --portal my-portal

  # Export the YAML resources as files in a directory:
  educates cluster portal export --portal my-portal --as-files ./export
`

type ClusterPortalExportOptions struct {
	KubeconfigOptions
	Portal          string
	AsFiles         string
	Repository      string
	WorkshopVersion string
}

func (o *ClusterPortalExportOptions) Run() error {
	if o.Portal == "" {
		o.Portal = "educates-cli"
	}

	clusterConfig, err := cluster.NewClusterConfigIfAvailable(o.Kubeconfig, o.Context)
	if err != nil {
		return err
	}

	dynamicClient, err := clusterConfig.GetDynamicClient()
	if err != nil {
		return errors.Wrap(err, "unable to create Kubernetes client")
	}

	documents, err := exportTrainingPortalDocuments(dynamicClient, o.Portal, o.Repository, o.WorkshopVersion)
	if err != nil {
		return err
	}

	if o.AsFiles != "" {
		return utils.WriteExportedDocuments(o.AsFiles, documents)
	}

	return utils.PrintExportedDocuments(documents)
}

// exportTrainingPortalDocuments fetches the training portal and each
// workshop it references, sanitizes them for re-apply, and returns them as
// named YAML documents (trainingportal.yaml first, then one per workshop).
func exportTrainingPortalDocuments(client dynamic.Interface, portal, repository, workshopVersion string) ([]utils.ExportedYAMLDocument, error) {
	trainingPortalClient := client.Resource(trainingPortalResource)

	trainingPortal, err := trainingPortalClient.Get(context.TODO(), portal, metav1.GetOptions{})
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil, errors.Errorf("training portal %q does not exist", portal)
		}
		return nil, errors.Wrapf(err, "unable to fetch training portal %q in cluster", portal)
	}

	workshopNames, err := extractWorkshopNamesFromTrainingPortal(trainingPortal)
	if err != nil {
		return nil, err
	}

	documents := make([]utils.ExportedYAMLDocument, 0, len(workshopNames)+1)

	portalData, err := utils.RenderResourceAsYAMLDocument(utils.SanitizeTrainingPortalResourceForExport(utils.SanitizeResourceForExport(trainingPortal)))
	if err != nil {
		return nil, errors.Wrapf(err, "unable to generate YAML for training portal %q", trainingPortal.GetName())
	}
	documents = append(documents, utils.ExportedYAMLDocument{Name: "trainingportal.yaml", Data: portalData})

	workshopsClient := client.Resource(workshopResource)

	for _, name := range workshopNames {
		workshop, err := workshopsClient.Get(context.TODO(), name, metav1.GetOptions{})
		if err != nil {
			if k8serrors.IsNotFound(err) {
				return nil, errors.Errorf("workshop %q referenced by training portal %q does not exist", name, portal)
			}
			return nil, errors.Wrapf(err, "unable to fetch workshop %q in cluster", name)
		}

		workshopData, err := utils.RenderResourceAsYAMLDocument(utils.SanitizeWorkshopResourceForExport(utils.SanitizeResourceForExport(workshop), &utils.WorkshopResourceExportConfig{
			Repository:      repository,
			WorkshopVersion: workshopVersion,
		}))
		if err != nil {
			return nil, errors.Wrapf(err, "unable to generate YAML for workshop %q", name)
		}

		documents = append(documents, utils.ExportedYAMLDocument{Name: name + ".yaml", Data: workshopData})
	}

	return documents, nil
}

// extractWorkshopNamesFromTrainingPortal returns the de-duplicated workshop
// names referenced in a training portal's spec.workshops.
func extractWorkshopNamesFromTrainingPortal(trainingPortal *unstructured.Unstructured) ([]string, error) {
	workshops, _, err := unstructured.NestedSlice(trainingPortal.Object, "spec", "workshops")
	if err != nil {
		return nil, errors.Wrap(err, "unable to retrieve workshops from training portal")
	}

	names := []string{}
	seen := map[string]struct{}{}

	for _, item := range workshops {
		object, ok := item.(map[string]interface{})
		if !ok {
			return nil, errors.Errorf("invalid workshop reference in training portal %q", trainingPortal.GetName())
		}

		name, ok := object["name"].(string)
		if !ok || name == "" {
			return nil, errors.Errorf("invalid workshop reference in training portal %q", trainingPortal.GetName())
		}

		if _, exists := seen[name]; exists {
			continue
		}

		seen[name] = struct{}{}
		names = append(names, name)
	}

	return names, nil
}

func (p *ProjectInfo) NewClusterPortalExportCmd() *cobra.Command {
	var o ClusterPortalExportOptions

	var c = &cobra.Command{
		Args:    cobra.NoArgs,
		Use:     "export",
		Short:   "Export portal and its workshops from Kubernetes",
		Example: clusterPortalExportExample,
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
	c.Flags().StringVarP(
		&o.Portal,
		"portal",
		"p",
		"educates-cli",
		"name to be used for training portal and workshop name prefixes",
	)
	c.Flags().StringVar(
		&o.AsFiles,
		"as-files",
		"",
		"write YAML resources as files in the target directory instead of stdout",
	)
	c.Flags().StringVar(
		&o.Repository,
		"image-repository",
		"localhost:5001",
		"the address of the image repository",
	)
	c.Flags().StringVar(
		&o.WorkshopVersion,
		"workshop-version",
		"latest",
		"version of the workshop being exported",
	)

	return c
}
