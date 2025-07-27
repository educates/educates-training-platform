package cmd

import (
	"github.com/educates/educates-training-platform/client-programs/pkg/workshops"
	"github.com/spf13/cobra"
)

var (
	workshopExportExample = `
  # Export workshop definition from current directory (workshop definition is expected within the path defined by workshop-file flag) to stdout
  educates workshop export

  # Export workshop definition from specific directory to stdout
  educates workshop export lab-k8s-fundamentals

  # Export workshop definition using specific image repository
  educates workshop export --image-repository ghcr.io/myorg

  # Export workshop definition using specific version
  educates workshop export --workshop-version v1.0.0

  # Export workshop definition for custom workshop file path
  educates workshop export --workshop-file custom-workshop.yaml
  educates workshop export $HOME/workshops/labs-educates-showcase --workshop-file lab-session-workshop.yaml
`
)

func (p *ProjectInfo) NewWorkshopExportCmd() *cobra.Command {
	var o workshops.FilesExportOptions

	var c = &cobra.Command{
		Args:    cobra.MaximumNArgs(1),
		Use:     "export [PATH]",
		Short:   "Export workshop resource definition for distribution to stdout",
		RunE:    func(cmd *cobra.Command, args []string) error { return o.Run(args) },
		Example: workshopExportExample,
	}

	c.Flags().StringVar(
		&o.Repository,
		"image-repository",
		"localhost:5001",
		"the address of the image repository",
	)
	c.Flags().StringVar(
		&o.WorkshopFile,
		"workshop-file",
		"resources/workshop.yaml",
		"location of the workshop definition file",
	)

	c.Flags().StringVar(
		&o.WorkshopVersion,
		"workshop-version",
		"latest",
		"version of the workshop being published",
	)

	c.Flags().StringArrayVar(
		&o.DataValuesFlags.EnvFromStrings,
		"data-values-env",
		nil,
		"Extract data values (as strings) from prefixed env vars (format: PREFIX for PREFIX_all__key1=str) (can be specified multiple times)",
	)
	c.Flags().StringArrayVar(
		&o.DataValuesFlags.EnvFromYAML,
		"data-values-env-yaml",
		nil,
		"Extract data values (parsed as YAML) from prefixed env vars (format: PREFIX for PREFIX_all__key1=true) (can be specified multiple times)",
	)

	c.Flags().StringArrayVar(
		&o.DataValuesFlags.KVsFromStrings,
		"data-value",
		nil,
		"Set specific data value to given value, as string (format: all.key1.subkey=123) (can be specified multiple times)",
	)
	c.Flags().StringArrayVar(
		&o.DataValuesFlags.KVsFromYAML,
		"data-value-yaml",
		nil,
		"Set specific data value to given value, parsed as YAML (format: all.key1.subkey=true) (can be specified multiple times)",
	)
	c.Flags().StringArrayVar(
		&o.DataValuesFlags.KVsFromFiles,
		"data-value-file",
		nil,
		"Set specific data value to contents of a file (format: [@lib1:]all.key1.subkey={file path, HTTP URL, or '-' (i.e. stdin)}) (can be specified multiple times)",
	)
	c.Flags().StringArrayVar(
		&o.DataValuesFlags.FromFiles,
		"data-values-file",
		nil,
		"Set multiple data values via plain YAML files (format: [@lib1:]{file path, HTTP URL, or '-' (i.e. stdin)}) (can be specified multiple times)",
	)

	return c
}
