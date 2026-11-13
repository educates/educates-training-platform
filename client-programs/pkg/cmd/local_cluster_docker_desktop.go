package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"

	"github.com/moby/moby/client"

	"github.com/educates/educates-training-platform/client-programs/pkg/docker"
	"github.com/educates/educates-training-platform/client-programs/pkg/localclusters"
)

// checkDockerDesktopPrivilegedPorts refuses, as
// localclusters.CheckPrivilegedPortForwarding decides, a Docker engine
// that can't serve a local cluster's ports 80 and 443 on its listen
// address: Docker Desktop 4.97 or later with "Allow privileged port
// forwarding to external machines" off. It writes to w the warning for a
// setting it can't read.
func checkDockerDesktopPrivilegedPorts(ctx context.Context, w io.Writer) error {
	cli, err := docker.NewDockerClient()
	if err != nil {
		return fmt.Errorf("docker client: %w", err)
	}
	version, err := cli.ServerVersion(ctx, client.ServerVersionOptions{})
	if err != nil {
		return fmt.Errorf("read the Docker server's version: %w", err)
	}
	warning, err := localclusters.CheckPrivilegedPortForwarding(version.Platform.Name, privilegedPortForwardingSetting())
	if warning != "" {
		fmt.Fprintf(w, "Warning: %s\n", warning)
	}
	return err
}

// privilegedPortForwardingSetting reads Docker Desktop's setting "Allow
// privileged port forwarding to external machines": on macOS, the boolean
// key AllowPrivilegedPortFallback of
// ~/Library/Group Containers/group.com.docker/settings-store.json. It is
// unknown elsewhere, and when the file or the key can't be read.
func privilegedPortForwardingSetting() localclusters.PrivilegedPortForwarding {
	if runtime.GOOS != "darwin" {
		return localclusters.PrivilegedPortForwardingUnknown
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return localclusters.PrivilegedPortForwardingUnknown
	}
	data, err := os.ReadFile(filepath.Join(home, "Library", "Group Containers", "group.com.docker", "settings-store.json"))
	if err != nil {
		return localclusters.PrivilegedPortForwardingUnknown
	}
	var settings struct {
		AllowPrivilegedPortFallback *bool
	}
	if err := json.Unmarshal(data, &settings); err != nil || settings.AllowPrivilegedPortFallback == nil {
		return localclusters.PrivilegedPortForwardingUnknown
	}
	if *settings.AllowPrivilegedPortFallback {
		return localclusters.PrivilegedPortForwardingOn
	}
	return localclusters.PrivilegedPortForwardingOff
}
