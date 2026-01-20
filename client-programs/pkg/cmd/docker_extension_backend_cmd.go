package cmd

import (
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
		RunE:  func(_ *cobra.Command, _ []string) error {
			dockerExtensionBackend := docker.NewDockerExtensionBackend(p.Version, p.ImageRepository)
			return dockerExtensionBackend.Run(&docker.DockerExtensionBackendConfig{
				Socket: o.Socket,
			})
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
