package cmd

import (
	"github.com/spf13/cobra"

	"github.com/educates/educates-training-platform/client-programs/pkg/workshops"
)

var (
	workshopNewExample = `
  # Create a new workshop using default hugo template (a directory will be created with my-workshop as name)
  educates workshop new my-workshop

  # Create a new workshop using default hugo template in /tmp/workshop
  educates workshop new my-workshop -d /tmp/workshop

  # Create a new workshop using default hugo template in current directory and overwrite existing files
  educates workshop new my-workshop -d . -y

  # Create a new workshop with custom name
  educates workshop new my-workshop --name "my-custom-workshop"

  # Create a new workshop with title and description
  educates workshop new my-workshop --title "Introduction to Kubernetes" --description "Learn the basics of Kubernetes"

  # Create a new workshop with language-specific educates base image. See docs for available images.
  educates workshop new my-workshop --image 'jdk21-environment:*'
  educates workshop new my-workshop --image 'conda-environment:*'

  # Create a new workshop with custom base image
  educates workshop new my-workshop --image ghcr.io/myorg/workshop-base:latest

  # Create a new workshop using the classic template
  educates workshop new my-workshop --template classic

  # Create a new workshop with kubernetes access enabled in the workshop
  educates workshop new my-workshop --with-kubernetes-access
`
)

func (p *ProjectInfo) NewWorkshopNewCmd() *cobra.Command {
	var o workshops.WorkshopNewOptions

	var c = &cobra.Command{
		Args:    cobra.ExactArgs(1),
		Use:     "new PATH",
		Short:   "Create workshop files from template",
		RunE:    func(_ *cobra.Command, args []string) error { return o.Run(args) },
		Example: workshopNewExample,
	}

	c.Flags().StringVarP(
		&o.Template,
		"template",
		"t",
		"hugo",
		"name of the workshop template to use (hugo, classic)",
	)
	c.Flags().StringVarP(
		&o.Name,
		"name",
		"n",
		"",
		"override name of the workshop (default: directory name)",
	)
	c.Flags().StringVar(
		&o.Title,
		"title",
		"",
		"short title describing the workshop",
	)
	c.Flags().StringVar(
		&o.Description,
		"description",
		"",
		"longer summary describing the workshop",
	)
	c.Flags().StringVar(
		&o.Image,
		"image",
		"",
		"name of the workshop base image to use",
	)
	c.Flags().StringVarP(
		&o.TargetDirectory,
		"directory",
		"d",
		"",
		"directory where the workshop will be created. By default a new directory with the workshop name will be created",
	)
	c.Flags().BoolVarP(
		&o.Overwrite,
		"overwrite",
		"y",
		false,
		"overwrite existing files in the target directory. If not provided, the user will be prompted to confirm the operation.",
	)
	c.Flags().BoolVarP(
		&o.WithKubernetesAccess,
		"with-kubernetes-access",
		"",
		false,
		"enable kubernetes access in the workshop",
	)
	c.Flags().BoolVarP(
		&o.AddGitHubAction,
		"add-github-action",
		"",
		false,
		"add GitHub action to the generated workshop",
	)

	return c
}
