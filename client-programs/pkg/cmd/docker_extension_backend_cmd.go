package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"regexp"
	"strconv"
	"strings"
	"syscall"

	"github.com/pkg/errors"
	"github.com/spf13/cobra"
)

var dockerExtensionBackendExample = `
  # Run the Docker Desktop extension backend on a socket:
  educates docker extension backend --socket /run/guest-services/backend.sock
`

type DockerExtensionBackendOptions struct {
	Socket string
}

type DockerWorkshopsBackend struct {
	Manager         DockerWorkshopsManager
	ImageRepository string
	ImageVersion    string
}

func NewDockerWorkshopsBackend(version string, imageRepository string) DockerWorkshopsBackend {
	return DockerWorkshopsBackend{
		Manager:         NewDockerWorkshopsManager(),
		ImageRepository: imageRepository,
		ImageVersion:    version,
	}
}

func (b *DockerWorkshopsBackend) ListWorkhops(w http.ResponseWriter, r *http.Request) {
	workshops, err := b.Manager.ListWorkhops()

	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	jsonData, err := json.Marshal(workshops)

	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")

	w.WriteHeader(http.StatusOK)
	w.Write(jsonData)
}

func (b *DockerWorkshopsBackend) DeployWorkshop(w http.ResponseWriter, r *http.Request) {
	queryParams := r.URL.Query()

	url := queryParams.Get("url")

	if url == "" {
		http.Error(w, "workshop definition url required", http.StatusBadRequest)
		return
	}

	portString := queryParams.Get("port")

	if portString == "" {
		portString = "10081"
	}

	port, err := strconv.Atoi(portString)

	if err != nil || port <= 0 {
		http.Error(w, "invalid workshop port supplied", http.StatusBadRequest)
		return
	}

	o := DockerWorkshopDeployOptions{
		Path:               url,
		Host:               "127.0.0.1",
		Port:               uint(port),
		LocalRepository:    "localhost:5001",
		DisableOpenBrowser: false,
		ImageRepository:    b.ImageRepository,
		ImageVersion:       b.ImageVersion,
		Cluster:            "",
		KubeConfig:         "",
		Assets:             "",
	}

	name, err := b.Manager.DeployWorkshop(&o, os.Stdout, os.Stderr)

	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	sessionUrl := fmt.Sprintf("http://workshop.%s.nip.io:%d", strings.ReplaceAll(o.Host, ".", "-"), o.Port)

	workshop := DockerWorkshopDetails{
		Name:   name,
		Url:    sessionUrl,
		Source: url,
		Status: "Started",
	}

	jsonData, err := json.Marshal(workshop)

	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")

	w.WriteHeader(http.StatusOK)
	w.Write(jsonData)
}

func (b *DockerWorkshopsBackend) DeleteWorkshop(w http.ResponseWriter, r *http.Request) {
	queryParams := r.URL.Query()

	name := queryParams.Get("name")

	if name == "" {
		http.Error(w, "workshop session name required", http.StatusBadRequest)
		return
	}

	err := b.Manager.DeleteWorkshop(name, os.Stdout, os.Stderr)

	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	workshop := DockerWorkshopDetails{
		Name:   name,
		Status: "Stopped",
	}

	jsonData, err := json.Marshal(workshop)

	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")

	w.WriteHeader(http.StatusOK)
	w.Write(jsonData)
}

// loggedResponse records the status and body a handler writes, for the
// request log.
type loggedResponse struct {
	http.ResponseWriter
	status int
	body   bytes.Buffer
}

func (l *loggedResponse) WriteHeader(status int) {
	l.status = status
	l.ResponseWriter.WriteHeader(status)
}

func (l *loggedResponse) Write(b []byte) (int, error) {
	l.body.Write(b)
	return l.ResponseWriter.Write(b)
}

// logRequests logs the requests of the extension UI, so that the backend's
// log, which docker logs shows, says what the UI asked for and what it got
// back: a failed deploy otherwise reports its error to the UI alone. Deploy
// and delete are logged when they arrive and when they finish, as the
// output of a deploy comes in between. List, which the UI polls every few
// seconds, and version are logged only when they fail.
func logRequests(logger *log.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		always := r.URL.Path == "/workshop/deploy" || r.URL.Path == "/workshop/delete"

		if always {
			logger.Printf("request %s %s", r.Method, r.URL.RequestURI())
		}

		response := &loggedResponse{ResponseWriter: w, status: http.StatusOK}

		next.ServeHTTP(response, r)

		if always || response.status >= http.StatusBadRequest {
			logger.Printf("response %s %s status=%d body=%s", r.Method, r.URL.RequestURI(), response.status, strings.TrimSpace(response.body.String()))
		}
	})
}

func (o *DockerExtensionBackendOptions) Run(p *ProjectInfo) error {
	if o.Socket == "" {
		return errors.New("invalid socket for HTTP server")
	}

	logger := log.New(os.Stdout, "", log.LstdFlags)

	router := http.NewServeMux()

	versionHandler := func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, p.Version)
	}

	router.HandleFunc("/version", versionHandler)

	backend := NewDockerWorkshopsBackend(p.Version, p.ImageRepository)

	router.HandleFunc("/workshop/list", backend.ListWorkhops)
	router.HandleFunc("/workshop/deploy", backend.DeployWorkshop)
	router.HandleFunc("/workshop/delete", backend.DeleteWorkshop)

	server := http.Server{
		Handler: logRequests(logger, router),
	}

	// The socket string can either be of the form host:nnn, or it can be a file
	// system path (absolute or relative). In the first case we start up a
	// normal HTTP server accepting connections over an INET socket connection.
	// In the second case connections will be accepted over a UNIX socket.

	inetRegexPattern := `^([a-zA-Z0-9.-]+):(\d+)$`

	match, err := regexp.MatchString(inetRegexPattern, o.Socket)

	if err != nil {
		return errors.Wrap(err, "failed to perform regex match on socket")
	}

	var listener net.Listener

	if match {
		listener, err = net.Listen("tcp", o.Socket)

		if err != nil {
			return errors.Wrap(err, "unable to create INET HTTP server socket")
		}
	} else {
		listener, err = net.Listen("unix", o.Socket)

		if err != nil {
			return errors.Wrap(err, "unable to create UNIX HTTP server socket")
		}

		defer os.Remove(o.Socket)
	}

	defer listener.Close()

	logger.Printf("extension backend version %s listening on %s, with images from %s", p.Version, o.Socket, p.ImageRepository)

	go func() {
		if err := server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Printf("extension backend stopped serving: %v", err)
		}
	}()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	<-sigChan

	logger.Printf("extension backend stopping")

	err = server.Shutdown(context.TODO())

	if err != nil {
		return errors.Wrap(err, "failed to shutdown HTTP server")
	}

	return nil
}

func (p *ProjectInfo) NewDockerExtensionBackendCmd() *cobra.Command {
	var o DockerExtensionBackendOptions

	var c = &cobra.Command{
		Args:    cobra.NoArgs,
		Use:     "backend",
		Short:   "Docker desktop extension backend",
		Example: dockerExtensionBackendExample,
		RunE:    func(_ *cobra.Command, _ []string) error { return o.Run(p) },
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
