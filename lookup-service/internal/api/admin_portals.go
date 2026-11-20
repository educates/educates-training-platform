package api

import (
	"net/http"

	"github.com/educates/educates-training-platform/lookup-service/internal/config"
	"github.com/educates/educates-training-platform/lookup-service/internal/inventory"
)

// portalDetails is a training portal as the admin routes show it.
type portalDetails struct {
	Name       string        `json:"name"`
	UID        string        `json:"uid"`
	Generation int64         `json:"generation"`
	Labels     config.Labels `json:"labels"`
	Cluster    string        `json:"cluster"`
	URL        *string       `json:"url"`
	// Capacity is the most sessions the portal runs, 0 for no limit.
	Capacity  int64   `json:"capacity"`
	Allocated int     `json:"allocated"`
	Phase     *string `json:"phase"`
}

func portalDetailsOf(portal *inventory.Portal) portalDetails {
	return portalDetails{
		Name:       portal.Name,
		UID:        portal.UID,
		Generation: portal.Generation,
		Labels:     portal.Labels,
		Cluster:    portal.Cluster.Name,
		URL:        optional(portal.URL),
		Capacity:   portal.Capacity,
		Allocated:  portal.Allocated,
		Phase:      optional(portal.Phase),
	}
}

type portalList struct {
	Portals []portalDetails `json:"portals"`
}

func portalListOf(portals []*inventory.Portal) portalList {
	list := portalList{Portals: make([]portalDetails, 0, len(portals))}
	for _, portal := range portals {
		list.Portals = append(list.Portals, portalDetailsOf(portal))
	}
	return list
}

// environmentDetails is a workshop environment as the admin routes show
// it.
type environmentDetails struct {
	Name string `json:"name"`
	UID  string `json:"uid"`
	// Generation is the generation of the workshop the environment runs.
	Generation  int64         `json:"generation"`
	Workshop    string        `json:"workshop"`
	Title       *string       `json:"title"`
	Description *string       `json:"description"`
	Labels      config.Labels `json:"labels"`
	Cluster     string        `json:"cluster"`
	Portal      string        `json:"portal"`
	// Capacity is the most sessions the environment runs, 0 too when the
	// environment gives no capacity yet.
	Capacity  int64   `json:"capacity"`
	Reserved  int64   `json:"reserved"`
	Allocated int     `json:"allocated"`
	Available int     `json:"available"`
	Phase     *string `json:"phase"`
}

func environmentDetailsOf(environment *inventory.Environment) environmentDetails {
	details := environmentDetails{
		Name:        environment.Name,
		UID:         environment.UID,
		Generation:  environment.Generation,
		Workshop:    environment.Workshop,
		Title:       optional(environment.Title),
		Description: optional(environment.Description),
		Labels:      environment.Labels,
		Cluster:     environment.Portal.Cluster.Name,
		Portal:      environment.Portal.Name,
		Reserved:    environment.Reserved,
		Allocated:   environment.Allocated,
		Available:   environment.Available,
		Phase:       optional(environment.Phase),
	}
	if environment.Capacity != nil {
		details.Capacity = *environment.Capacity
	}
	return details
}

// sessionDetails is a workshop session as the admin routes show it.
type sessionDetails struct {
	Name        string  `json:"name"`
	Generation  int64   `json:"generation"`
	Cluster     string  `json:"cluster"`
	Portal      string  `json:"portal"`
	Environment string  `json:"environment"`
	Workshop    string  `json:"workshop"`
	Phase       *string `json:"phase"`
	// User is null for a reserved session waiting for a user.
	User *string `json:"user"`
}

type sessionList struct {
	Sessions []sessionDetails `json:"sessions"`
}

func sessionListOf(sessions []*inventory.Session) sessionList {
	list := sessionList{Sessions: make([]sessionDetails, 0, len(sessions))}
	for _, session := range sessions {
		environment := session.Environment
		list.Sessions = append(list.Sessions, sessionDetails{
			Name:        session.Name,
			Generation:  session.Generation,
			Cluster:     environment.Portal.Cluster.Name,
			Portal:      environment.Portal.Name,
			Environment: environment.Name,
			Workshop:    environment.Workshop,
			Phase:       optional(session.Phase),
			User:        optional(session.User),
		})
	}
	return list
}

// optional returns a string the status of a training resource gives, nil
// when it gives none, which the REST API shows as null.
func optional(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}

// listPortals answers GET /api/v1/portals, for an admin: every portal of
// every cluster.
func (s *Server) listPortals(w http.ResponseWriter, r *http.Request, _ caller) {
	s.writeJSON(w, r, portalListOf(s.inventory.Snapshot().Portals()))
}

// listClusterPortals answers GET /api/v1/clusters/{cluster}/portals, for
// an admin.
func (s *Server) listClusterPortals(w http.ResponseWriter, r *http.Request, _ caller) {
	cluster, ok := s.cluster(w, r)
	if !ok {
		return
	}
	s.writeJSON(w, r, portalListOf(cluster.Portals))
}

// getPortal answers GET /api/v1/clusters/{cluster}/portals/{portal}, for an
// admin.
func (s *Server) getPortal(w http.ResponseWriter, r *http.Request, _ caller) {
	portal, ok := s.portal(w, r)
	if !ok {
		return
	}
	s.writeJSON(w, r, portalDetailsOf(portal))
}

// listEnvironments answers
// GET /api/v1/clusters/{cluster}/portals/{portal}/environments, for an
// admin.
func (s *Server) listEnvironments(w http.ResponseWriter, r *http.Request, _ caller) {
	portal, ok := s.portal(w, r)
	if !ok {
		return
	}
	environments := make([]environmentDetails, 0, len(portal.Environments))
	for _, environment := range portal.Environments {
		environments = append(environments, environmentDetailsOf(environment))
	}
	s.writeJSON(w, r, struct {
		Environments []environmentDetails `json:"environments"`
	}{environments})
}

// getEnvironment answers
// GET /api/v1/clusters/{cluster}/portals/{portal}/environments/{environment},
// for an admin.
func (s *Server) getEnvironment(w http.ResponseWriter, r *http.Request, _ caller) {
	environment, ok := s.environment(w, r)
	if !ok {
		return
	}
	s.writeJSON(w, r, environmentDetailsOf(environment))
}

// listSessions answers
// GET /api/v1/clusters/{cluster}/portals/{portal}/environments/{environment}/sessions,
// for an admin: every session of the environment, reserved, in use or
// stopping.
func (s *Server) listSessions(w http.ResponseWriter, r *http.Request, _ caller) {
	environment, ok := s.environment(w, r)
	if !ok {
		return
	}
	s.writeJSON(w, r, sessionListOf(environment.Sessions))
}

// listUsers answers
// GET /api/v1/clusters/{cluster}/portals/{portal}/environments/{environment}/users,
// for an admin: the users of the environment's sessions, each once, null
// standing for the reserved sessions that have none.
func (s *Server) listUsers(w http.ResponseWriter, r *http.Request, _ caller) {
	environment, ok := s.environment(w, r)
	if !ok {
		return
	}
	users := []*string{}
	seen := map[string]bool{}
	for _, session := range environment.Sessions {
		if !seen[session.User] {
			seen[session.User] = true
			users = append(users, optional(session.User))
		}
	}
	s.writeJSON(w, r, struct {
		Users []*string `json:"users"`
	}{users})
}

// listUserSessions answers
// GET /api/v1/clusters/{cluster}/portals/{portal}/environments/{environment}/users/{user}/sessions,
// for an admin: the environment's sessions of that user.
func (s *Server) listUserSessions(w http.ResponseWriter, r *http.Request, _ caller) {
	environment, ok := s.environment(w, r)
	if !ok {
		return
	}
	user := r.PathValue("user")
	sessions := []*inventory.Session{}
	for _, session := range environment.Sessions {
		if session.User != "" && session.User == user {
			sessions = append(sessions, session)
		}
	}
	s.writeJSON(w, r, sessionListOf(sessions))
}

// portal returns the portal the request's path names, on the cluster it
// names. It answers the request itself, and returns false, when there is
// no such cluster or portal.
func (s *Server) portal(w http.ResponseWriter, r *http.Request) (*inventory.Portal, bool) {
	cluster, ok := s.cluster(w, r)
	if !ok {
		return nil, false
	}
	portal, ok := cluster.Portal(r.PathValue("portal"))
	if !ok {
		writeText(w, http.StatusNotFound, "Portal not available")
		return nil, false
	}
	return portal, true
}

// environment returns the workshop environment the request's path names,
// of the portal and cluster it names. It answers the request itself, and
// returns false, when there is no such cluster, portal or environment.
func (s *Server) environment(w http.ResponseWriter, r *http.Request) (*inventory.Environment, bool) {
	portal, ok := s.portal(w, r)
	if !ok {
		return nil, false
	}
	environment, ok := portal.Environment(r.PathValue("environment"))
	if !ok {
		writeText(w, http.StatusNotFound, "Environment not available")
		return nil, false
	}
	return environment, true
}
