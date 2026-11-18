package api

import (
	"net/http"

	"github.com/educates/educates-training-platform/lookup-service/internal/config"
	"github.com/educates/educates-training-platform/lookup-service/internal/inventory"
)

type workshopDetails struct {
	Name        string        `json:"name"`
	Title       string        `json:"title"`
	Description string        `json:"description"`
	Labels      config.Labels `json:"labels"`
}

type workshopList struct {
	Workshops []workshopDetails `json:"workshops"`
}

// listWorkshops answers GET /api/v1/workshops: the workshops of the
// portals the tenant the tenant parameter names reaches. A client acting as
// a tenant must name a tenant it may act for; an admin naming none gets the
// workshops of every portal on every cluster.
func (s *Server) listWorkshops(w http.ResponseWriter, r *http.Request, c caller) {
	tenantName := r.URL.Query().Get("tenant")

	if c.actsAs(roleTenant) {
		if tenantName == "" {
			s.log.Info("A tenant client listed workshops without a tenant", "client", c.client.Name)
			writeText(w, http.StatusBadRequest, "Missing tenant name")
			return
		}
		if !c.client.MayActFor(tenantName) {
			writeText(w, http.StatusForbidden, "Client not allowed access to tenant")
			return
		}
	}

	snapshot := s.inventory.Snapshot()
	portals := snapshot.Portals()
	if tenantName != "" {
		tenant, found, err := s.config.Tenant(r.Context(), tenantName)
		if err != nil {
			s.internalError(w, r, err)
			return
		}
		if !found {
			writeText(w, http.StatusServiceUnavailable, "Tenant not available")
			return
		}
		portals = snapshot.PortalsOf(tenant)
	}
	s.writeJSON(w, r, workshopList{Workshops: workshopsOf(portals)})
}

// listTenantWorkshops answers GET /api/v1/tenants/{tenant}/workshops: the
// workshops of the portals the tenant reaches. A client acting as a tenant
// must be one that may act for it.
func (s *Server) listTenantWorkshops(w http.ResponseWriter, r *http.Request, c caller) {
	tenantName := r.PathValue("tenant")

	if c.actsAs(roleTenant) && !c.client.MayActFor(tenantName) {
		writeText(w, http.StatusForbidden, "Client access not permitted")
		return
	}

	tenant, found, err := s.config.Tenant(r.Context(), tenantName)
	if err != nil {
		s.internalError(w, r, err)
		return
	}
	if !found {
		writeText(w, http.StatusNotFound, "Tenant not available")
		return
	}
	s.writeJSON(w, r, workshopList{Workshops: workshopsOf(s.inventory.Snapshot().PortalsOf(tenant))})
}

// workshopsOf lists the workshops of the portals' running environments.
// A workshop offered by several environments is listed once, in the place
// it is first found, with the title, description and labels of the last
// environment found to offer it.
func workshopsOf(portals []*inventory.Portal) []workshopDetails {
	workshops := []workshopDetails{}
	index := map[string]int{}
	for _, portal := range portals {
		for _, environment := range portal.Environments {
			if environment.Phase != inventory.PhaseRunning {
				continue
			}
			details := workshopDetails{
				Name:        environment.Workshop,
				Title:       environment.Title,
				Description: environment.Description,
				Labels:      environment.Labels,
			}
			if i, seen := index[details.Name]; seen {
				workshops[i] = details
				continue
			}
			index[details.Name] = len(workshops)
			workshops = append(workshops, details)
		}
	}
	return workshops
}
