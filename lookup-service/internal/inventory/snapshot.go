package inventory

import (
	"github.com/educates/educates-training-platform/lookup-service/internal/config"
	"github.com/educates/educates-training-platform/lookup-service/internal/kubeconfig"
)

// Snapshot is the inventory at one moment: every cluster, with the training
// portals, workshop environments and sessions of those whose cache has
// synced. A snapshot never changes, so a request that works from one sees
// one consistent view whatever the watches deliver meanwhile. Nothing in it
// may be modified.
type Snapshot struct {
	clusters []*Cluster
	byName   map[string]*Cluster
}

// Clusters returns every cluster, ordered by name.
func (s *Snapshot) Clusters() []*Cluster {
	return s.clusters
}

// Cluster returns the cluster of that name.
func (s *Snapshot) Cluster(name string) (*Cluster, bool) {
	c, ok := s.byName[name]
	return c, ok
}

// Portals returns every portal of every cluster, ordered by cluster and
// then by name.
func (s *Snapshot) Portals() []*Portal {
	var portals []*Portal
	for _, c := range s.clusters {
		portals = append(portals, c.Portals...)
	}
	return portals
}

// PortalsOf returns the portals the tenant reaches: those, on every cluster
// the tenant's cluster selector matches, that its portal selector matches,
// ordered by cluster and then by name.
func (s *Snapshot) PortalsOf(tenant config.Tenant) []*Portal {
	var portals []*Portal
	for _, c := range s.clusters {
		if !tenant.ReachesCluster(c.Name, c.Labels) {
			continue
		}
		for _, p := range c.Portals {
			if tenant.ReachesPortal(p.Name, p.Labels) {
				portals = append(portals, p)
			}
		}
	}
	return portals
}

// Cluster is a cluster as the configuration describes it and as its cache
// shows it.
type Cluster struct {
	Name   string
	Labels config.Labels
	// Local reports whether the cluster is the one the service runs on.
	Local bool
	// Kubeconfig is the kubeconfig the cluster is reached through, nil
	// while the configuration gives none the service can use.
	Kubeconfig *kubeconfig.Config
	// Problem says why the cluster is not connected, as a kubeconfig that is
	// refused or a token that is not mounted.
	Problem string
	// Synced reports whether the cluster's cache has synced. Until it has,
	// the cluster has no portals.
	Synced bool
	// Portals are the cluster's training portals, ordered by name.
	Portals []*Portal

	portals map[string]*Portal
}

// Portal returns the cluster's portal of that name.
func (c *Cluster) Portal(name string) (*Portal, bool) {
	p, ok := c.portals[name]
	return p, ok
}

// Portal is a training portal.
type Portal struct {
	Cluster    *Cluster
	Name       string
	UID        string
	Generation int64
	// Labels are the portal's Educates labels, which tenants select
	// portals by.
	Labels config.Labels
	// URL is the portal's public URL.
	URL string
	// Namespace is the namespace the portal runs in.
	Namespace string
	// RobotCredentialsSecret names the Secret, in the portal's namespace,
	// holding the credentials of the portal's REST API.
	RobotCredentialsSecret string
	Phase                  string
	// Capacity is the most sessions the portal runs, 0 for no limit.
	Capacity int64
	// Allocated is the sum of its environments' allocated sessions.
	Allocated int
	// InFlight is the sum of its environments' requests in flight.
	InFlight int
	// Environments are the portal's workshop environments, ordered by name.
	Environments []*Environment

	environments map[string]*Environment
}

// Environment returns the portal's environment of that name.
func (p *Portal) Environment(name string) (*Environment, bool) {
	e, ok := p.environments[name]
	return e, ok
}

// Environment is a workshop environment of a portal.
type Environment struct {
	Portal *Portal
	Name   string
	UID    string
	// Generation is the generation of the workshop the environment runs.
	Generation int64
	// Workshop is the name the portal offers the workshop under.
	Workshop    string
	Title       string
	Description string
	// Labels are the workshop's Educates labels.
	Labels config.Labels
	// Capacity is the most sessions the environment runs, nil when the
	// environment gives no capacity.
	Capacity *int64
	// Reserved is how many reserved sessions the environment keeps.
	Reserved int64
	Phase    string
	// Allocated counts the sessions that are taken: awaiting activation,
	// in use or stopping.
	Allocated int
	// InFlight counts the session requests the service has sent to the
	// environment whose session the inventory does not show as taken yet.
	// The environment's allocated count, as placement reckons it, is
	// Allocated plus InFlight.
	InFlight int
	// Available counts the reserved sessions waiting for a user.
	Available int
	// Sessions are the environment's sessions, ordered by name.
	Sessions []*Session
}

// Session is a workshop session of an environment.
type Session struct {
	Environment *Environment
	Name        string
	Generation  int64
	Phase       string
	// User is the name of the portal user the session belongs to, empty for
	// a reserved session waiting for a user.
	User string
}
