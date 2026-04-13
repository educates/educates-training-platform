package docker

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
)

type DockerWorkshopsApi struct {
	Manager         DockerWorkshopsManager
	ImageRepository string
	ImageVersion    string
}

// func NewDockerWorkshopsApi(version string, imageRepository string) *DockerWorkshopsApi {
// 	return &DockerWorkshopsApi{
// 		Manager:         NewDockerWorkshopsManager(),
// 		ImageRepository: imageRepository,
// 		ImageVersion:    version,
// 	}
// }

func (b *DockerWorkshopsApi) ListWorkhops(w http.ResponseWriter, r *http.Request) {
	workshops, err := b.Manager.ListWorkshops()

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

func (b *DockerWorkshopsApi) DeployWorkshop(w http.ResponseWriter, r *http.Request) {
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

	o := DockerWorkshopDeployConfig{
		Path:               url,
		Host:               "127.0.0.1",
		Port:               uint(port),
		LocalRepository:    "localhost:5001",
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

func (b *DockerWorkshopsApi) DeleteWorkshop(w http.ResponseWriter, r *http.Request) {
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
