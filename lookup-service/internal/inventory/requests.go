package inventory

import "time"

// requestLimit bounds how long a request counts once its portal named the
// session it gave, for a session the inventory never shows as taken, such as
// one deleted at once.
const requestLimit = time.Minute

// Request is a session request the service has sent to a workshop
// environment. While it is in flight it counts against the environment, and
// its portal, as a session taken: from when it is sent until it fails, or
// until the inventory shows the session it gave as taken.
type Request struct {
	inventory                    *Inventory
	cluster, portal, environment string
	// session names the session the portal gave, empty until it gave one.
	session string
	timer   *time.Timer
}

// StartRequest records a session request sent to the environment of that
// name of the portal on the cluster, which counts against it until the
// request ends.
func (i *Inventory) StartRequest(cluster, portal, environment string) *Request {
	i.mu.Lock()
	defer i.mu.Unlock()
	r := &Request{inventory: i, cluster: cluster, portal: portal, environment: environment}
	if entry, ok := i.clusters[cluster]; ok {
		if entry.requests == nil {
			entry.requests = map[*Request]struct{}{}
		}
		entry.requests[r] = struct{}{}
		i.snapshot = nil
	}
	return r
}

// Failed ends the request, whose attempt failed.
func (r *Request) Failed() {
	i := r.inventory
	i.mu.Lock()
	defer i.mu.Unlock()
	i.endRequest(r)
}

// Placed records the session the portal gave the request, which counts
// until the inventory shows that session as taken, at once if it does
// already.
func (r *Request) Placed(session string) {
	i := r.inventory
	i.mu.Lock()
	defer i.mu.Unlock()
	entry, ok := i.clusters[r.cluster]
	if !ok {
		return
	}
	if _, inFlight := entry.requests[r]; !inFlight {
		return
	}
	r.session = session
	if feed := entry.feed; feed != nil {
		if record, ok := feed.sessions[session]; ok && SessionTaken(record.phase) {
			i.endRequest(r)
			return
		}
	}
	r.timer = time.AfterFunc(requestLimit, r.Failed)
}

// endRequest stops the request counting. It is called with the lock held.
func (i *Inventory) endRequest(r *Request) {
	entry, ok := i.clusters[r.cluster]
	if !ok {
		return
	}
	if _, inFlight := entry.requests[r]; !inFlight {
		return
	}
	delete(entry.requests, r)
	if r.timer != nil {
		r.timer.Stop()
	}
	i.snapshot = nil
}

// settleRequests ends the requests of the cluster whose session the record
// shows as taken, or that name a session that is gone. It is called with
// the lock held.
func (i *Inventory) settleRequests(cluster, session string, record *sessionRecord) {
	entry, ok := i.clusters[cluster]
	if !ok {
		return
	}
	for r := range entry.requests {
		if r.session == session && (record == nil || SessionTaken(record.phase)) {
			i.endRequest(r)
		}
	}
}
