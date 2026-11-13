package cmd

import (
	"fmt"
	"io"

	"github.com/pkg/errors"
	"github.com/spf13/cobra"

	"github.com/educates/educates-training-platform/client-programs/pkg/config"
	"github.com/educates/educates-training-platform/client-programs/pkg/config/v1alpha1"
	"github.com/educates/educates-training-platform/client-programs/pkg/localclusters"
	"github.com/educates/educates-training-platform/client-programs/pkg/registry"
	"github.com/educates/educates-training-platform/client-programs/pkg/utils"
)

var localMirrorListExample = `
  # List the deployed local image registry mirrors:
  educates local mirror list
`

func (p *ProjectInfo) NewLocalMirrorListCmd() *cobra.Command {
	var c = &cobra.Command{
		Args:  cobra.NoArgs,
		Use:   "list",
		Short: "Lists the local image registry mirrors",
		Long: `Lists the registry mirror containers, which every local cluster shares,
and the mirrors the registry mirrors file, mirrors.yaml in the data home,
declares.

DECLARED says whether mirrors.yaml declares the mirror. A declared mirror
whose container is missing shows the status missing, with the URL and
username it declares: the next 'educates local cluster create' or
'educates local cluster start' creates it.`,
		Example: localMirrorListExample,
		RunE: func(cmd *cobra.Command, _ []string) error {
			declared, err := config.LoadLocalMirrors(utils.GetEducatesHomeDir())
			if err != nil {
				return err
			}

			mirrors, err := registry.ListRegistryMirrors()
			if err != nil {
				return errors.Wrap(err, "failed to list registry mirrors")
			}

			writeMirrorList(cmd.OutOrStdout(), mirrors, declared)
			return nil
		},
	}

	return c
}

// writeMirrorList writes to w the table of local mirror list: a row for
// each registry mirror whose container is among containers, and for each
// mirror among declared, the mirrors the registry mirrors file declares,
// with whether the file declares it. A declared mirror whose container is
// missing shows the URL and username it declares, and never its password.
func writeMirrorList(w io.Writer, containers []registry.RegistryMirror, declared []v1alpha1.RegistryMirror) {
	byName := make(map[string]registry.RegistryMirror, len(containers))
	names := make([]string, 0, len(containers))
	for _, m := range containers {
		byName[m.Name] = m
		names = append(names, m.Name)
	}

	listed := localclusters.ListMirrors(names, declared)
	if len(listed) == 0 {
		fmt.Fprintln(w, "No mirrors found.")
		return
	}

	rows := make([][]string, 0, len(listed))
	for _, l := range listed {
		isDeclared := "no"
		if l.Declaration != nil {
			isDeclared = "yes"
		}
		if l.HasContainer {
			m := byName[l.Mirror]
			rows = append(rows, []string{m.Name, m.URL, m.Username, m.Status, m.ContainerName, isDeclared})
			continue
		}
		url := l.Declaration.URL
		if url == "" {
			url = l.Mirror
		}
		rows = append(rows, []string{l.Mirror, url, l.Declaration.Username, "missing", "", isDeclared})
	}

	fmt.Fprintln(w, utils.PrintTable([]string{"NAME", "URL", "USERNAME", "STATUS", "CONTAINER_NAME", "DECLARED"}, rows))
}
