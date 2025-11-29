package docker

import (
	"sync"

	"github.com/docker/docker/client"
)

var (
	dockerClient *client.Client
	once         sync.Once
	initErr      error
)

func NewDockerClient() (*client.Client, error) {
	once.Do(func() {
		dockerClient, initErr = client.NewClientWithOpts(
			client.FromEnv,
			client.WithAPIVersionNegotiation(), // <-- This is the fix
		)
	})
	return dockerClient, initErr
}
