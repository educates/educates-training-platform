package api

import (
	"net/http"

	"github.com/educates/educates-training-platform/lookup-service/internal/config"
)

type tenantDetails struct {
	Name string `json:"name"`
	// Clients are the clients that may act for the tenant.
	Clients []string `json:"clients"`
}

// tenantDetailsOf returns the tenant's details, with the names of the
// clients whose tenant patterns match it.
func tenantDetailsOf(tenant config.Tenant, clients []config.Client) tenantDetails {
	details := tenantDetails{Name: tenant.Name, Clients: []string{}}
	for _, client := range clients {
		if client.MayActFor(tenant.Name) {
			details.Clients = append(details.Clients, client.Name)
		}
	}
	return details
}

// listTenants answers GET /api/v1/tenants, for an admin.
func (s *Server) listTenants(w http.ResponseWriter, r *http.Request, _ caller) {
	tenants, err := s.config.Tenants(r.Context())
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	clients, err := s.config.Clients(r.Context())
	if err != nil {
		s.internalError(w, r, err)
		return
	}

	list := make([]tenantDetails, 0, len(tenants))
	for _, tenant := range tenants {
		list = append(list, tenantDetailsOf(tenant, clients))
	}
	s.writeJSON(w, r, struct {
		Tenants []tenantDetails `json:"tenants"`
	}{list})
}

// getTenant answers GET /api/v1/tenants/{tenant}, for an admin.
func (s *Server) getTenant(w http.ResponseWriter, r *http.Request, _ caller) {
	tenant, ok := s.tenant(w, r)
	if !ok {
		return
	}
	clients, err := s.config.Clients(r.Context())
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	s.writeJSON(w, r, tenantDetailsOf(tenant, clients))
}

// listTenantPortals answers GET /api/v1/tenants/{tenant}/portals, for an
// admin: the portals the tenant reaches.
func (s *Server) listTenantPortals(w http.ResponseWriter, r *http.Request, _ caller) {
	tenant, ok := s.tenant(w, r)
	if !ok {
		return
	}
	s.writeJSON(w, r, portalListOf(s.inventory.Snapshot().PortalsOf(tenant)))
}

// tenant returns the tenant the request's path names. It answers the
// request itself, and returns false, when there is no such tenant.
func (s *Server) tenant(w http.ResponseWriter, r *http.Request) (config.Tenant, bool) {
	tenant, found, err := s.config.Tenant(r.Context(), r.PathValue("tenant"))
	if err != nil {
		s.internalError(w, r, err)
		return config.Tenant{}, false
	}
	if !found {
		writeText(w, http.StatusNotFound, "Tenant not available")
		return config.Tenant{}, false
	}
	return tenant, true
}
