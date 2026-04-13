package cmd

import (
	"fmt"
	"os"

	"github.com/educates/educates-training-platform/client-programs/pkg/docker"
	"github.com/spf13/cobra"
)

const dockerExtensionBackendExample = `
# Start the backend server on a Unix socket
docker extension backend --socket /run/guest-services/backend.sock
`

type DockerExtensionBackendOptions struct {
	Socket string
}

func (p *ProjectInfo) NewDockerExtensionBackendCmd() *cobra.Command {
	var o DockerExtensionBackendOptions

	var c = &cobra.Command{
		Args:  cobra.NoArgs,
		Use:   "backend",
		Short: "Docker desktop extension backend",
		RunE: func(_ *cobra.Command, _ []string) error {
			fmt.Fprintf(os.Stdout, "starting extension backend version=%s imageRepository=%s socket=%s\n", p.Version, p.ImageRepository, o.Socket)
			dockerExtensionBackend := docker.NewDockerExtensionBackend(p.Version, p.ImageRepository)
			err := dockerExtensionBackend.Run(&docker.DockerExtensionBackendConfig{
				Socket: o.Socket,
			})
			if err != nil {
				fmt.Fprintf(os.Stdout, "extension backend exited with error: %v\n", err)
			}
			return err
		},
		Example: dockerExtensionBackendExample,
	}

	c.Flags().StringVar(
		&o.Socket,
		"socket",
		"",
		"socket to listen on for HTTP server connections",
	)

	cobra.MarkFlagRequired(c.Flags(), "socket")

	return c
}
