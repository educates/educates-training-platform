package api

import (
	"encoding/json"
	"net/http"

	"github.com/educates/educates-training-platform/lookup-service/internal/trainingportal"
)

// sessionResponse is the answer to a session request.
type sessionResponse struct {
	TenantName           string `json:"tenantName"`
	ClusterName          string `json:"clusterName"`
	PortalName           string `json:"portalName"`
	EnvironmentName      string `json:"environmentName"`
	SessionName          string `json:"sessionName"`
	ClientUserID         string `json:"clientUserId"`
	SessionActivationURL string `json:"sessionActivationUrl"`
}

// requestSession answers POST /api/v1/workshops: a session of a workshop for
// one of the tenant's users, from a portal the tenant reaches, the user's
// live session there when they have one. The client's own user, when its
// configuration sets one, overrides the request's.
func (s *Server) requestSession(w http.ResponseWriter, r *http.Request, c caller) {
	body, err := readJSONObject(w, r)
	if err != nil {
		badRequestBody(w, err)
		return
	}

	tenantName := stringField(body, "tenantName")
	workshopName := stringField(body, "workshopName")
	indexURL := stringField(body, "clientIndexUrl")
	userID := c.client.User
	if userID == "" {
		userID = stringField(body, "clientUserId")
	}
	analyticsURL := stringField(body, "analyticsWebhookUrl")
	s.log.Info("Workshop session requested", "client", c.client.Name, "tenant", tenantName,
		"workshop", workshopName, "user", userID, "action", stringField(body, "clientActionId"),
		"analytics", analyticsURL)

	switch {
	case tenantName == "":
		writeText(w, http.StatusBadRequest, "Missing tenantName")
		return
	case workshopName == "":
		writeText(w, http.StatusBadRequest, "Missing workshopName")
		return
	case !c.client.MayActFor(tenantName):
		s.log.Info("A client requested a session for a tenant it may not act for", "client", c.client.Name,
			"tenant", tenantName)
		writeText(w, http.StatusForbidden, "Client not allowed access to tenant")
		return
	case indexURL == "":
		writeText(w, http.StatusBadRequest, "Missing clientIndexUrl")
		return
	}

	tenant, ok := s.tenant(w, r, tenantName, http.StatusServiceUnavailable)
	if !ok {
		return
	}

	request := trainingportal.SessionRequest{
		User:         userID,
		Email:        stringField(body, "userEmailAddress"),
		FirstName:    stringField(body, "userFirstName"),
		LastName:     stringField(body, "userLastName"),
		IndexURL:     indexURL,
		AnalyticsURL: analyticsURL,
	}
	// A null member is passed on as null; only an absent one means none.
	if parameters, ok := body["workshopParams"]; ok {
		if request.Parameters, err = json.Marshal(parameters); err != nil {
			s.internalError(w, r, err)
			return
		}
	}

	portals := s.inventory.Snapshot().PortalsOf(tenant)
	placement, ok := s.placer.Place(r.Context(), portals, workshopName, request)
	if !ok {
		s.log.Info("Workshop not available", "client", c.client.Name, "tenant", tenantName,
			"workshop", workshopName, "user", userID)
		writeText(w, http.StatusServiceUnavailable, "Workshop not available")
		return
	}
	environment := placement.Environment
	s.writeJSON(w, r, sessionResponse{
		TenantName:           tenantName,
		ClusterName:          environment.Portal.Cluster.Name,
		PortalName:           environment.Portal.Name,
		EnvironmentName:      environment.Name,
		SessionName:          placement.Session.Name,
		ClientUserID:         userID,
		SessionActivationURL: placement.Session.ActivationURL,
	})
}

// stringField returns the request body's member of that name as a string:
// a string as it is, the empty string for a member that is missing or null,
// and any other value as its JSON text.
func stringField(body map[string]any, name string) string {
	switch value := body[name].(type) {
	case nil:
		return ""
	case string:
		return value
	default:
		text, err := json.Marshal(value)
		if err != nil {
			return ""
		}
		return string(text)
	}
}
