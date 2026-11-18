package api

import (
	"net/http"

	"github.com/educates/educates-training-platform/lookup-service/internal/config"
)

type clientDetails struct {
	Name    string   `json:"name"`
	Roles   []string `json:"roles"`
	Tenants []string `json:"tenants"`
}

func detailsOf(client config.Client) clientDetails {
	return clientDetails{Name: client.Name, Roles: client.Roles, Tenants: client.Tenants}
}

// listClients answers GET /api/v1/clients, for an admin.
func (s *Server) listClients(w http.ResponseWriter, r *http.Request, _ caller) {
	clients, err := s.config.Clients(r.Context())
	if err != nil {
		s.internalError(w, r, err)
		return
	}

	details := make([]clientDetails, 0, len(clients))
	for _, client := range clients {
		details = append(details, detailsOf(client))
	}
	s.writeJSON(w, r, struct {
		Clients []clientDetails `json:"clients"`
	}{details})
}

// getClient answers GET /api/v1/clients/{client}. A client acting as a
// tenant may read only its own details, even when it is an admin too.
func (s *Server) getClient(w http.ResponseWriter, r *http.Request, c caller) {
	name := r.PathValue("client")

	if c.actsAs(roleTenant) && c.client.Name != name {
		writeText(w, http.StatusForbidden, "Client access not permitted")
		return
	}

	client, found, err := s.config.Client(r.Context(), name)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	if !found {
		writeText(w, http.StatusNotFound, "Client not available")
		return
	}
	s.writeJSON(w, r, detailsOf(client))
}
