package cmd

import (
	"fmt"

	"github.com/educates/educates-training-platform/client-programs/pkg/docker"
	"github.com/educates/educates-training-platform/client-programs/pkg/utils"
	"github.com/pkg/errors"
	"github.com/spf13/cobra"
)

const dockerWorkshopListExample = `
  # List Educates workshops deployed to Docker
  educates docker workshop list
`

func (p *ProjectInfo) NewDockerWorkshopListCmd() *cobra.Command {
	var c = &cobra.Command{
		Args:  cobra.NoArgs,
		Use:   "list",
		Short: "List workshops deployed to Docker",
		RunE: func(_ *cobra.Command, _ []string) error {
			dockerWorkshopsManager := docker.NewDockerWorkshopsManager()

			workshops, err := dockerWorkshopsManager.ListWorkshops()

			if err != nil {
				return errors.Wrap(err, "cannot display list of workshops")
			}

			var data [][]string
			for _, workshop := range workshops {
				data = append(data, []string{workshop.Name, workshop.Url, workshop.Source, workshop.Status})
			}
			fmt.Println(utils.PrintTable([]string{"NAME", "URL", "SOURCE", "STATUS"}, data))

			return nil
		},
		Example: dockerWorkshopListExample,
	}

	return c
}
