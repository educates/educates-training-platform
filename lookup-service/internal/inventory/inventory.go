// Package inventory is the projection of every cluster's training portals,
// their workshop environments and their sessions, with the counts placement
// uses. The cluster registry feeds it from each cluster's cache, and the
// REST API reads it through snapshots.
//
// Each resource is kept as the watch last delivered it, and the tree of
// portals, environments and sessions is assembled only when a snapshot is
// taken, so events that arrive out of order, such as a session before its
// environment or portal, need no special handling: a portal or environment
// known only from the resources that belong to it is shown in phase
// Unknown until its own resource arrives.
//
// The inventory also counts the session requests the service has in flight
// to each environment, until the session a request gave shows as taken, so
// that requests sent one after another see where the earlier ones went.
package inventory

import (
	"maps"
	"slices"
	"sync"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/educates/educates-training-platform/lookup-service/internal/config"
	"github.com/educates/educates-training-platform/lookup-service/internal/kubeconfig"
)

// Inventory holds every cluster the configuration names and the training
// resources their caches deliver. It is safe for concurrent use.
type Inventory struct {
	mu       sync.Mutex
	clusters map[string]*clusterEntry
	// snapshot is the snapshot of the current state, nil once a change
	// makes it stale.
	snapshot *Snapshot
}

type clusterEntry struct {
	info ClusterInfo
	// feed is the feed of the cluster's current cache, nil when none runs.
	feed *Feed
	// requests are the session requests in flight to the cluster's portals.
	requests map[*Request]struct{}
}

// ClusterInfo is what the configuration says about a cluster, and whether
// it gives a connection the service can use.
type ClusterInfo struct {
	Labels config.Labels
	// Local reports whether the cluster is the one the service runs on.
	Local bool
	// Kubeconfig is the kubeconfig the cluster is reached through, nil when
	// Problem says why there is none.
	Kubeconfig *kubeconfig.Config
	// Problem says why the cluster is not connected.
	Problem string
}

// New returns an empty inventory.
func New() *Inventory {
	return &Inventory{clusters: map[string]*clusterEntry{}}
}

// SetCluster records what the configuration says about the cluster of that
// name, adding it if it is new. Its training resources stay as its cache
// delivered them.
func (i *Inventory) SetCluster(name string, info ClusterInfo) {
	i.mu.Lock()
	defer i.mu.Unlock()
	entry, ok := i.clusters[name]
	if !ok {
		entry = &clusterEntry{}
		i.clusters[name] = entry
	}
	entry.info = info
	i.snapshot = nil
}

// RemoveCluster removes the cluster of that name and its training
// resources at once. What its cache delivers afterwards is ignored.
func (i *Inventory) RemoveCluster(name string) {
	i.mu.Lock()
	defer i.mu.Unlock()
	if _, ok := i.clusters[name]; ok {
		delete(i.clusters, name)
		i.snapshot = nil
	}
}

// Disconnect drops the feed of the cluster of that name at once, so that
// the cluster has no portals until another feed is attached and synced.
func (i *Inventory) Disconnect(name string) {
	i.mu.Lock()
	defer i.mu.Unlock()
	if entry, ok := i.clusters[name]; ok && entry.feed != nil {
		entry.feed = nil
		i.snapshot = nil
	}
}

// Attach returns a new, empty feed for the cluster of that name, to be
// filled by a newly started cache. It replaces the cluster's previous feed,
// whose resources are dropped and whose later deliveries are ignored. The
// cluster has no portals until the feed is marked synced.
func (i *Inventory) Attach(name string) *Feed {
	i.mu.Lock()
	defer i.mu.Unlock()
	f := &Feed{
		inventory:    i,
		cluster:      name,
		portals:      map[string]portalRecord{},
		environments: map[string]environmentRecord{},
		sessions:     map[string]sessionRecord{},
	}
	if entry, ok := i.clusters[name]; ok {
		entry.feed = f
		i.snapshot = nil
	}
	return f
}

// Snapshot returns the inventory as it is now. Snapshots taken while
// nothing changes are the same snapshot.
func (i *Inventory) Snapshot() *Snapshot {
	i.mu.Lock()
	defer i.mu.Unlock()
	if i.snapshot == nil {
		i.snapshot = i.assemble()
	}
	return i.snapshot
}

// Feed receives the training resources of one cluster from one cache. Once
// the feed is replaced or detached, or its cluster removed, what it
// receives is ignored.
type Feed struct {
	inventory *Inventory
	cluster   string

	synced       bool
	portals      map[string]portalRecord
	environments map[string]environmentRecord
	sessions     map[string]sessionRecord
}

// update applies change to the feed if it is still its cluster's feed.
func (f *Feed) update(change func()) {
	i := f.inventory
	i.mu.Lock()
	defer i.mu.Unlock()
	if entry, ok := i.clusters[f.cluster]; !ok || entry.feed != f {
		return
	}
	change()
	if f.synced {
		i.snapshot = nil
	}
}

// MarkSynced records that the cache has delivered every resource the
// cluster held when it started, so that the cluster's portals are shown.
func (f *Feed) MarkSynced() {
	f.update(func() {
		f.synced = true
		f.inventory.snapshot = nil
	})
}

// Detach drops the feed and its resources, leaving the cluster without
// portals until another feed is attached and synced.
func (f *Feed) Detach() {
	i := f.inventory
	i.mu.Lock()
	defer i.mu.Unlock()
	if entry, ok := i.clusters[f.cluster]; ok && entry.feed == f {
		entry.feed = nil
		i.snapshot = nil
	}
}

// SetPortal records a TrainingPortal as the watch delivered it.
func (f *Feed) SetPortal(u *unstructured.Unstructured) {
	record := projectPortal(u)
	f.update(func() { f.portals[record.name] = record })
}

// DeletePortal forgets the TrainingPortal of that name.
func (f *Feed) DeletePortal(name string) {
	f.update(func() { delete(f.portals, name) })
}

// SetEnvironment records a WorkshopEnvironment as the watch delivered it.
func (f *Feed) SetEnvironment(u *unstructured.Unstructured) {
	record := projectEnvironment(u)
	f.update(func() { f.environments[record.name] = record })
}

// DeleteEnvironment forgets the WorkshopEnvironment of that name.
func (f *Feed) DeleteEnvironment(name string) {
	f.update(func() { delete(f.environments, name) })
}

// SetSession records a WorkshopSession as the watch delivered it. A request
// in flight that gave the session ends once the session is taken.
func (f *Feed) SetSession(u *unstructured.Unstructured) {
	record := projectSession(u)
	f.update(func() {
		f.sessions[record.name] = record
		f.inventory.settleRequests(f.cluster, record.name, &record)
	})
}

// DeleteSession forgets the WorkshopSession of that name, ending a request
// in flight that gave it.
func (f *Feed) DeleteSession(name string) {
	f.update(func() {
		delete(f.sessions, name)
		f.inventory.settleRequests(f.cluster, name, nil)
	})
}

// assemble builds a snapshot of the current state. It is called with the
// lock held.
func (i *Inventory) assemble() *Snapshot {
	s := &Snapshot{byName: map[string]*Cluster{}}
	for _, name := range slices.Sorted(maps.Keys(i.clusters)) {
		entry := i.clusters[name]
		c := &Cluster{
			Name:       name,
			Labels:     entry.info.Labels,
			Local:      entry.info.Local,
			Kubeconfig: entry.info.Kubeconfig,
			Problem:    entry.info.Problem,
			Portals:    []*Portal{},
			portals:    map[string]*Portal{},
		}
		if entry.feed != nil && entry.feed.synced {
			c.Synced = true
			entry.feed.assemble(c, entry.requests)
		}
		s.clusters = append(s.clusters, c)
		s.byName[name] = c
	}
	return s
}

// assemble builds the cluster's tree of portals, environments and sessions
// from the feed's resources, standing in a portal or environment in phase
// Unknown for one known only from the resources that belong to it, and
// counts the requests in flight against their environments.
func (f *Feed) assemble(c *Cluster, requests map[*Request]struct{}) {
	portal := func(name, uid string) *Portal {
		if p, ok := c.portals[name]; ok {
			return p
		}
		p := &Portal{Cluster: c, Name: name, UID: uid, Labels: config.Labels{}, Phase: PhaseUnknown,
			environments: map[string]*Environment{}}
		c.portals[name] = p
		return p
	}
	for _, name := range slices.Sorted(maps.Keys(f.portals)) {
		r := f.portals[name]
		p := portal(r.name, r.uid)
		p.Generation = r.generation
		p.Labels = r.labels
		p.URL = r.url
		p.Namespace = r.namespace
		p.RobotCredentialsSecret = r.robot
		p.Phase = r.phase
		p.Capacity = r.capacity
	}

	for _, name := range slices.Sorted(maps.Keys(f.environments)) {
		r := f.environments[name]
		p := portal(r.portal, r.portalUID)
		p.environments[r.name] = &Environment{
			Portal:      p,
			Name:        r.name,
			UID:         r.uid,
			Generation:  r.generation,
			Workshop:    r.workshop,
			Title:       r.title,
			Description: r.description,
			Labels:      r.labels,
			Capacity:    r.capacity,
			Reserved:    r.reserved,
			Phase:       r.phase,
		}
	}

	for _, name := range slices.Sorted(maps.Keys(f.sessions)) {
		r := f.sessions[name]
		p := portal(r.portal, r.portalUID)
		e, ok := p.environments[r.environment]
		if !ok {
			e = &Environment{Portal: p, Name: r.environment, UID: r.environmentUID, Workshop: r.workshop,
				Labels: config.Labels{}, Phase: PhaseUnknown}
			p.environments[r.environment] = e
		}
		e.Sessions = append(e.Sessions, &Session{
			Environment: e,
			Name:        r.name,
			Generation:  r.generation,
			Phase:       r.phase,
			User:        r.user,
		})
		switch {
		case isAllocated(r.phase):
			e.Allocated++
		case r.phase == SessionAvailable:
			e.Available++
		}
	}

	for r := range requests {
		if p, ok := c.portals[r.portal]; ok {
			if e, ok := p.environments[r.environment]; ok {
				e.InFlight++
			}
		}
	}

	for _, name := range slices.Sorted(maps.Keys(c.portals)) {
		p := c.portals[name]
		c.Portals = append(c.Portals, p)
		p.Environments = []*Environment{}
		for _, environment := range slices.Sorted(maps.Keys(p.environments)) {
			e := p.environments[environment]
			if e.Sessions == nil {
				e.Sessions = []*Session{}
			}
			p.Environments = append(p.Environments, e)
			p.Allocated += e.Allocated
			p.InFlight += e.InFlight
		}
	}
}
