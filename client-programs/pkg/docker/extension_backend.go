package docker

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"regexp"
	"syscall"

	"github.com/pkg/errors"
)

type DockerExtensionBackendConfig struct {
	Socket string
}

type DockerExtensionBackend struct {
	Api *DockerWorkshopsApi
}

func NewDockerExtensionBackend(version string, imageRepository string) DockerExtensionBackend {
	return DockerExtensionBackend{
		Api: &DockerWorkshopsApi{
			Manager:         NewDockerWorkshopsManager(),
			ImageRepository: imageRepository,
			ImageVersion:    version,
		},
	}
}

func (b *DockerExtensionBackend) Run(config *DockerExtensionBackendConfig) error {
	if config.Socket == "" {
		return errors.New("invalid socket for HTTP server")
	}

	router := http.NewServeMux()

	versionHandler := func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, b.Api.ImageVersion)
	}

	router.HandleFunc("/version", versionHandler)

	router.HandleFunc("/workshop/list", b.Api.ListWorkhops)
	router.HandleFunc("/workshop/deploy", b.Api.DeployWorkshop)
	router.HandleFunc("/workshop/delete", b.Api.DeleteWorkshop)

	server := http.Server{
		Handler: router,
	}

	// The socket string can either be of the form host:nnn, or it can be a file
	// system path (absolute or relative). In the first case we start up a
	// normal HTTP server accepting connections over an INET socket connection.
	// In the second case connections will be accepted over a UNIX socket.

	inetRegexPattern := `^([a-zA-Z0-9.-]+):(\d+)$`

	match, err := regexp.MatchString(inetRegexPattern, config.Socket)

	if err != nil {
		return errors.Wrap(err, "failed to perform regex match on socket")
	}

	var listener net.Listener

	if match {
		listener, err = net.Listen("tcp", config.Socket)

		if err != nil {
			return errors.Wrap(err, "unable to create INET HTTP server socket")
		}
	} else {
		listener, err = net.Listen("unix", config.Socket)

		if err != nil {
			return errors.Wrap(err, "unable to create UNIX HTTP server socket")
		}

		defer os.Remove(config.Socket)
	}

	defer listener.Close()

	go func() {
		server.Serve(listener)
	}()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	<-sigChan

	err = server.Shutdown(context.TODO())

	if err != nil {
		return errors.Wrap(err, "failed to shutdown HTTP server")
	}

	return nil
}
