package utils

import (
	"context"
	"fmt"
	"strings"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/client"
)

func GetContainerName(container container.Summary) string {
	name := "unknown"

	if len(container.Names) > 0 {
		// Get the first name and strip the leading slash "/"
		name = strings.TrimPrefix(container.Names[0], "/")
	}

	return name
}

/**
 * This function is used to get the container id and status of a container.
 * If the container does not exist, it will return an empty string for the container id and status.
 */
 func GetContainerInfo(containerName string) (containerID string, status string) {
	ctx := context.Background()

	cli, err := client.NewClientWithOpts(client.FromEnv)
	if err != nil {
		panic(err)
	}

	filters := filters.NewArgs()
	filters.Add(
		"name", containerName,
	)

	resp, err := cli.ContainerList(ctx, container.ListOptions{Filters: filters})
	if err != nil {
		panic(err)
	}

	if len(resp) > 0 {
		containerID = resp[0].ID
		containerStatus := strings.Split(resp[0].Status, " ")
		status = containerStatus[0] //fmt.Println(status[0])
	} else {
		fmt.Printf("container '%s' does not exists\n", containerName)
	}

	return
}
