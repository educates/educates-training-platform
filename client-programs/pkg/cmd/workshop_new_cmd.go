package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"

	"github.com/pkg/errors"
	"github.com/spf13/cobra"

	"github.com/educates/educates-training-platform/client-programs/pkg/educates/local/workshops"
	"github.com/educates/educates-training-platform/client-programs/pkg/utils"
)

const (
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

  # Create a new workshop with github action enabled in the workshop
  educates workshop new my-workshop --with-github-action

  # Create a new workshop with virtual cluster enabled in the workshop
  educates workshop new my-workshop --with-virtual-cluster

  # Create a new workshop with docker, registry and console enabled in the workshop
  educates workshop new my-workshop --with-docker --with-registry --with-console

  # Create a new workshop with editor and terminal disabled in the workshop
  educates workshop new my-workshop --with-editor=false --with-terminal=false

  # Create a new workshop with workshop instructions disabled in the workshop
  educates workshop new my-workshop --with-workshop-instructions=false
`
)

type WorkshopNewOptions struct {
	Template              string
	Name                  string
	Title                 string
	Description           string
	Image                 string
	TargetDirectory       string
	Overwrite             bool
	WithKubernetesAccess  bool
	WithGitHubAction      bool
	WithVirtualCluster    bool
	WithDockerDaemon      bool
	WithImageRegistry     bool
	WithKubernetesConsole bool
	WithEditor            bool
	WithTerminal          bool
}

func (p *ProjectInfo) NewWorkshopNewCmd() *cobra.Command {
	var o WorkshopNewOptions

	var c = &cobra.Command{
		Args:  func(cmd *cobra.Command, args []string) error {
			if len(args) < 1 {
				return utils.CmdError(cmd, "path is required", "PATH")
			}
			if len(args) > 1 {
				return utils.CmdError(cmd, "too many arguments", "PATH")
			}
			return nil
		},
		Use:   "new PATH",
		Short: "Create workshop files from template",
		RunE: func(_ *cobra.Command, args []string) error {
			var err error

			// Validate workshop name
			name := o.Name
			if name == "" {
				name = args[0]
			}
			if match, _ := regexp.MatchString("^[a-z0-9-]+$", name); !match {
				return errors.Errorf("invalid workshop name %q", name)
			}

			// Get workshop dir
			workshopDir := filepath.Clean(args[0])
			if o.TargetDirectory != "" {
				workshopDir = filepath.Join(o.TargetDirectory, args[0])
			}

			if workshopDir, err = filepath.Abs(workshopDir); err != nil {
				return errors.Wrapf(err, "could not convert path name %q to absolute path", workshopDir)
			}

			// Check if target directory already exist and prompt the user to confirm that they want to overwrite the files in it
			if _, err = os.Stat(workshopDir); err == nil {
				ok := o.Overwrite
				if !o.Overwrite {
					ok = utils.YesNoPrompt([]string{
						fmt.Sprintf("WARNING: The directory %q already exists.", workshopDir),
						"All files will be created in it, overwriting existing files.",
						"Do you still want to use this directory?",
					}, true)
				}
				if !ok {
					return nil // Operation cancelled
				}
			}

			manager := workshops.NewWorkshopManager()
			err = manager.NewWorkshop(workshopDir, &workshops.WorkshopNewConfig{
				Template: o.Template,
				Name: name,
				Title: o.Title,
				Description: o.Description,
				Image: o.Image,
				TargetDirectory: o.TargetDirectory,
				Overwrite: o.Overwrite,
				WithKubernetesAccess: o.WithKubernetesAccess,
				WithGitHubAction: o.WithGitHubAction,
				WithVirtualCluster: o.WithVirtualCluster,
				WithDockerDaemon: o.WithDockerDaemon,
				WithImageRegistry: o.WithImageRegistry,
				WithKubernetesConsole: o.WithKubernetesConsole,
				WithEditor: o.WithEditor,
				WithTerminal: o.WithTerminal,
			})
			if err != nil {
				return err
			}
			fmt.Printf("Workshop %q created successfully.\n", name)
			return nil
		},
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
		&o.WithGitHubAction,
		"with-github-action",
		"",
		false,
		"add GitHub action to the generated workshop to publish the workshop",
	)
	c.Flags().BoolVarP(
		&o.WithVirtualCluster,
		"with-virtual-cluster",
		"",
		false,
		"enable virtual cluster in the workshop",
	)
	c.Flags().BoolVarP(
		&o.WithDockerDaemon,
		"with-docker-daemon",
		"",
		false,
		"enable docker daemon in the workshop",
	)
	c.Flags().BoolVarP(
		&o.WithImageRegistry,
		"with-image-registry",
		"",
		false,
		"enable image registry in the workshop",
	)
	c.Flags().BoolVarP(
		&o.WithKubernetesConsole,
		"with-kubernetes-console",
		"",
		false,
		"enable Kubernetes console in the workshop",
	)
	c.Flags().BoolVarP(
		&o.WithEditor,
		"with-editor",
		"",
		true,
		"enable editor in the workshop",
	)
	c.Flags().BoolVarP(
		&o.WithTerminal,
		"with-terminal",
		"",
		true,
		"enable terminal in the workshop",
	)

	return c
}
