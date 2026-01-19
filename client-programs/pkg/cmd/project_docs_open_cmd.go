package cmd

import (
	"github.com/educates/educates-training-platform/client-programs/pkg/config"
	"github.com/educates/educates-training-platform/client-programs/pkg/utils"
	"github.com/spf13/cobra"
)

/*
Create Cobra command object for opening hosted docs in browser.
*/
func (p *ProjectInfo) NewProjectDocsOpenCmd() *cobra.Command {
	var c = &cobra.Command{
		Args:  cobra.NoArgs,
		Use:   "open",
		Short: "Open browser on project documentation",
		RunE: func(_ *cobra.Command, _ []string) error {
			return utils.OpenBrowser(config.PROJECT_DOCS_URL)
		},
	}

	return c
}
