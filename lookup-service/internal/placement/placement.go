// Package placement places session requests on training portals: it gives a
// returning user their live session back, and otherwise filters the
// workshop environments that could give a session, orders them, and tries
// each in turn until a portal gives one.
package placement

import (
	"context"

	"github.com/go-logr/logr"

	"github.com/educates/educates-training-platform/lookup-service/internal/inventory"
	"github.com/educates/educates-training-platform/lookup-service/internal/trainingportal"
)

// Candidates returns the environments of the portals that could give a
// session of the workshop: those running it, matched by the name the portal
// offers it under, that do not have a capacity of 0.
func Candidates(portals []*inventory.Portal, workshop string) []*inventory.Environment {
	var candidates []*inventory.Environment
	for _, portal := range portals {
		for _, environment := range portal.Environments {
			if environment.Phase != inventory.PhaseRunning || environment.Workshop != workshop {
				continue
			}
			if environment.Capacity != nil && *environment.Capacity == 0 {
				continue
			}
			candidates = append(candidates, environment)
		}
	}
	return candidates
}

// SessionsOf returns the user's live sessions of the workshop on the
// portals, in the order the portals list them: those awaiting activation or
// in use, of the environments offering the workshop, whatever the
// environment's phase or the room left on its portal. A stopping session is
// not live.
func SessionsOf(portals []*inventory.Portal, workshop, user string) []*inventory.Session {
	var sessions []*inventory.Session
	for _, portal := range portals {
		for _, environment := range portal.Environments {
			if environment.Workshop != workshop {
				continue
			}
			for _, session := range environment.Sessions {
				if session.User != user {
					continue
				}
				if session.Phase == inventory.SessionAllocating || session.Phase == inventory.SessionAllocated {
					sessions = append(sessions, session)
				}
			}
		}
	}
	return sessions
}

// Placer requests sessions from training portals, recording each request in
// the inventory while it is in flight.
type Placer struct {
	inventory *inventory.Inventory
	portals   *trainingportal.Client
	ordering  Ordering
	log       logr.Logger
}

// NewPlacer returns a placer that requests sessions through the portal
// client and records them in inv.
func NewPlacer(inv *inventory.Inventory, portals *trainingportal.Client, log logr.Logger) *Placer {
	return &Placer{inventory: inv, portals: portals, ordering: NewOrdering(), log: log}
}

// Placement is a session a portal gave, and the environment it is of.
type Placement struct {
	Environment *inventory.Environment
	Session     trainingportal.Session
}

// Place gives the request's user, when it names one, their live session of
// the workshop on the portals back, reacquiring each such session in turn
// until its portal gives it. Otherwise it requests a new session of the
// workshop from the environments of the portals that offer it, in the order
// the ordering gives, until a portal gives one. Every attempt that fails is
// logged, and Place reports false when none succeeds.
func (p *Placer) Place(ctx context.Context, portals []*inventory.Portal, workshop string,
	request trainingportal.SessionRequest) (Placement, bool) {
	if request.User != "" {
		if placement, ok := p.reacquire(ctx, portals, workshop, request); ok {
			return placement, true
		}
	}
	candidates := Candidates(portals, workshop)
	if len(candidates) == 0 {
		p.log.Info("No environment of the portals the tenant reaches can give a session of the workshop",
			"workshop", workshop)
		return Placement{}, false
	}
	for _, environment := range p.ordering.Order(candidates) {
		if ctx.Err() != nil {
			return Placement{}, false
		}
		portal := environment.Portal
		inFlight := p.inventory.StartRequest(portal.Cluster.Name, portal.Name, environment.Name)
		session, err := p.portals.RequestSession(ctx, environment, request)
		if err != nil {
			inFlight.Failed()
			p.log.Error(err, "The portal did not give a session",
				"cluster", portal.Cluster.Name, "portal", portal.Name, "environment", environment.Name,
				"workshop", workshop)
			continue
		}
		inFlight.Placed(session.Name)
		return Placement{Environment: environment, Session: session}, true
	}
	return Placement{}, false
}

// reacquire reacquires the request's user's live sessions of the workshop on
// the portals in turn, and reports the first one its portal gives back. A
// reacquired session is counted already, so it starts no request in flight.
func (p *Placer) reacquire(ctx context.Context, portals []*inventory.Portal, workshop string,
	request trainingportal.SessionRequest) (Placement, bool) {
	for _, session := range SessionsOf(portals, workshop, request.User) {
		if ctx.Err() != nil {
			return Placement{}, false
		}
		environment := session.Environment
		portal := environment.Portal
		reacquired, err := p.portals.ReacquireSession(ctx, session, request.IndexURL)
		if err != nil {
			p.log.Error(err, "The portal did not give the user's session back",
				"cluster", portal.Cluster.Name, "portal", portal.Name, "environment", environment.Name,
				"session", session.Name, "workshop", workshop)
			continue
		}
		return Placement{Environment: environment, Session: reacquired}, true
	}
	return Placement{}, false
}
