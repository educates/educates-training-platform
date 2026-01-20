package cmd

import (
	"github.com/spf13/cobra"

	"github.com/educates/educates-training-platform/client-programs/pkg/educates/local/workshops"
	"github.com/educates/educates-training-platform/client-programs/pkg/utils"
)

type WorkshopNewOptions struct {
	Template    string
	Name        string
	Title       string
	Description string
	Image       string
}

const workshopNewExample = `
  # Create workshop files from template in my-workshop directory
  educates workshop new my-workshop

  # Create workshop files from template in my-workshop directory
  educates workshop new my-workshop --template hugo (default template is hugo)

  # Create workshop files from template in my-workshop directory with a different name
  educates workshop new my-workshop --name "my-workshop" --title "My Workshop" --description "This is a workshop about my workshop"
`
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
			manager := workshops.NewWorkshopManager()
			return manager.NewWorkshop(args[0], &workshops.WorkshopNewConfig{
				Template: o.Template,
				Name: o.Name,
				Title: o.Title,
				Description: o.Description,
				Image: o.Image,
			})
		},
		Example: workshopNewExample,
	}

	c.Flags().StringVarP(
		&o.Template,
		"template",
		"t",
		"hugo",
		"name of the workshop template to use",
	)
	c.Flags().StringVarP(
		&o.Name,
		"name",
		"n",
		"",
		"override name of the workshop",
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

	return c
}
