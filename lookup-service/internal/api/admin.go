package api

// handleAdminRoutes registers the admin routes, which show the tenants and
// every cluster's training portals, workshop environments and sessions.
// Only a client with the admin role reaches them.
func (s *Server) handleAdminRoutes() {
	get := func(path string, handler callerHandler) {
		s.mux.Handle("GET "+path, s.loginRequired(rolesAccepted(handler, roleAdmin)))
	}

	get("/api/v1/tenants", s.listTenants)
	get("/api/v1/tenants/{tenant}", s.getTenant)
	get("/api/v1/tenants/{tenant}/portals", s.listTenantPortals)

	get("/api/v1/portals", s.listPortals)

	get("/api/v1/clusters", s.listClusters)
	get("/api/v1/clusters/{cluster}", s.getCluster)
	get("/api/v1/clusters/{cluster}/kubeconfig", s.getClusterKubeconfig)
	get("/api/v1/clusters/{cluster}/portals", s.listClusterPortals)
	get("/api/v1/clusters/{cluster}/portals/{portal}", s.getPortal)
	get("/api/v1/clusters/{cluster}/portals/{portal}/environments", s.listEnvironments)
	get("/api/v1/clusters/{cluster}/portals/{portal}/environments/{environment}", s.getEnvironment)
	get("/api/v1/clusters/{cluster}/portals/{portal}/environments/{environment}/sessions", s.listSessions)
	get("/api/v1/clusters/{cluster}/portals/{portal}/environments/{environment}/users", s.listUsers)
	get("/api/v1/clusters/{cluster}/portals/{portal}/environments/{environment}/users/{user}/sessions",
		s.listUserSessions)
}
