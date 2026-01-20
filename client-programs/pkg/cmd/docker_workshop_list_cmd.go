package cmd

import (
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/educates/educates-training-platform/client-programs/pkg/docker"
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

			// TODO: Move this to a helper function
			w := new(tabwriter.Writer)
			w.Init(os.Stdout, 8, 8, 3, ' ', 0)

			defer w.Flush()

			fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", "NAME", "URL", "SOURCE", "STATUS")

			for _, workshop := range workshops {
				fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", workshop.Name, workshop.Url, workshop.Source, workshop.Status)
			}

			return nil
		},
		Example: dockerWorkshopListExample,
	}

	return c
}
