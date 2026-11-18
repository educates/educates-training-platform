package api

import (
	"fmt"
	"net/http"

	"github.com/educates/educates-training-platform/lookup-service/internal/config"
	"github.com/educates/educates-training-platform/lookup-service/internal/inventory"
)

type clusterDetails struct {
	Name   string        `json:"name"`
	Labels config.Labels `json:"labels"`
}

// registered reports whether the admin routes show the cluster: they show
// a cluster once its kubeconfig is loaded, and not while the configuration
// gives none the service can use, as when it is refused.
func registered(cluster *inventory.Cluster) bool {
	return cluster.Kubeconfig != nil
}

// listClusters answers GET /api/v1/clusters, for an admin.
func (s *Server) listClusters(w http.ResponseWriter, r *http.Request, _ caller) {
	clusters := []clusterDetails{}
	for _, cluster := range s.inventory.Snapshot().Clusters() {
		if registered(cluster) {
			clusters = append(clusters, clusterDetails{Name: cluster.Name, Labels: cluster.Labels})
		}
	}
	s.writeJSON(w, r, struct {
		Clusters []clusterDetails `json:"clusters"`
	}{clusters})
}

// getCluster answers GET /api/v1/clusters/{cluster}, for an admin.
func (s *Server) getCluster(w http.ResponseWriter, r *http.Request, _ caller) {
	cluster, ok := s.cluster(w, r)
	if !ok {
		return
	}
	s.writeJSON(w, r, clusterDetails{Name: cluster.Name, Labels: cluster.Labels})
}

// getClusterKubeconfig answers GET /api/v1/clusters/{cluster}/kubeconfig,
// for an admin: the kubeconfig the cluster is reached through, as a
// kubeconfig file holding the one context the service uses.
func (s *Server) getClusterKubeconfig(w http.ResponseWriter, r *http.Request, _ caller) {
	cluster, ok := s.cluster(w, r)
	if !ok {
		return
	}
	data, err := cluster.Kubeconfig.YAML()
	if err != nil {
		s.internalError(w, r, fmt.Errorf("encoding the kubeconfig of cluster %s: %w", cluster.Name, err))
		return
	}
	writeText(w, http.StatusOK, string(data))
}

// cluster returns the cluster the request's path names, from a snapshot of
// the inventory. It answers the request itself, and returns false, when the
// admin routes show no such cluster.
func (s *Server) cluster(w http.ResponseWriter, r *http.Request) (*inventory.Cluster, bool) {
	cluster, ok := s.inventory.Snapshot().Cluster(r.PathValue("cluster"))
	if !ok || !registered(cluster) {
		writeText(w, http.StatusNotFound, "Cluster not available")
		return nil, false
	}
	return cluster, true
}
