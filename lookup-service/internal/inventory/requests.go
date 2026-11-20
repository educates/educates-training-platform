package inventory

import (
	"slices"
	"time"
)

// requestLimit bounds how long a request counts once its portal named the
// session it gave, or did not answer, for a session the inventory never
// shows as taken, such as one deleted at once or one the portal never gave.
const requestLimit = time.Minute

// Request is a session request the service has sent to a workshop
// environment. While it is in flight it counts against the environment, and
// its portal, as a session taken: from when it is sent until it fails, or
// until the inventory shows the session it gave as taken.
//
// A request its portal did not answer may have been given a session all the
// same. It goes on counting until the inventory shows a session of the
// environment taken that no other request accounts for, or for a minute.
type Request struct {
	inventory                    *Inventory
	cluster, portal, environment string
	// sequence orders the requests by when they were started.
	sequence uint64
	// session names the session the portal gave, empty until it gave one.
	session string
	// unanswered records that the portal did not answer the request.
	unanswered bool
	// seen are the sessions of the environment the inventory showed taken
	// since the request was started, which the request may have given,
	// while neither its portal nor another request says which it gave.
	seen  []string
	timer *time.Timer
}

// StartRequest records a session request sent to the environment, which
// counts against it until the request ends.
func (i *Inventory) StartRequest(environment *Environment) *Request {
	i.mu.Lock()
	defer i.mu.Unlock()
	portal := environment.Portal
	i.requests++
	r := &Request{inventory: i, cluster: portal.Cluster.Name, portal: portal.Name, environment: environment.Name,
		sequence: i.requests}
	if entry, ok := i.clusters[r.cluster]; ok {
		if entry.requests == nil {
			entry.requests = map[*Request]struct{}{}
		}
		entry.requests[r] = struct{}{}
		i.snapshot = nil
	}
	return r
}

// Failed ends the request, whose portal gave no session.
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
	r.seen = nil
	entry.accountFor(session)
	if feed := entry.feed; feed != nil {
		if record, ok := feed.sessions[session]; ok && SessionTaken(record.phase) {
			i.endRequest(r)
			return
		}
	}
	r.timer = time.AfterFunc(requestLimit, r.Failed)
}

// Unanswered records that the request's portal did not answer it, so that
// whether it gave a session is not known. The request counts until the
// inventory shows a session of its environment taken that no other request
// accounts for, at once if it has shown one since the request was started,
// or for a minute at most.
func (r *Request) Unanswered() {
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
	r.unanswered = true
	if feed := entry.feed; feed != nil {
		for _, session := range r.seen {
			if record, ok := feed.sessions[session]; ok && SessionTaken(record.phase) {
				entry.accountFor(session)
				i.endRequest(r)
				return
			}
		}
	}
	r.seen = nil
	r.timer = time.AfterFunc(requestLimit, r.Failed)
}

// accountFor records that a request accounts for the session, so that no
// other request takes it for the session it may have given.
func (e *clusterEntry) accountFor(session string) {
	for r := range e.requests {
		r.seen = slices.DeleteFunc(r.seen, func(seen string) bool { return seen == session })
	}
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
// shows as taken, or that name a session that is gone. A session that
// newly shows as taken and that no request names ends the oldest request of
// its environment that got no answer; when there is none, the requests of
// the environment still waiting for an answer remember it, since one of
// them may have given it. It is called with the lock held.
func (i *Inventory) settleRequests(cluster, session string, record *sessionRecord, newlyTaken bool) {
	entry, ok := i.clusters[cluster]
	if !ok {
		return
	}
	named := false
	for r := range entry.requests {
		if r.session == session {
			named = true
			if record == nil || SessionTaken(record.phase) {
				i.endRequest(r)
			}
		}
	}
	if named || !newlyTaken {
		return
	}

	var unanswered *Request
	var waiting []*Request
	for r := range entry.requests {
		if r.portal != record.portal || r.environment != record.environment || r.session != "" {
			continue
		}
		switch {
		case !r.unanswered:
			waiting = append(waiting, r)
		case unanswered == nil || r.sequence < unanswered.sequence:
			unanswered = r
		}
	}
	if unanswered != nil {
		i.endRequest(unanswered)
		return
	}
	for _, r := range waiting {
		r.seen = append(r.seen, session)
	}
}
