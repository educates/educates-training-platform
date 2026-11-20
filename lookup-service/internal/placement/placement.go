// Package placement places session requests on training portals: it gives a
// returning user their live session back, and otherwise filters the
// workshop environments that could give a session, orders them, and tries
// each in turn until a portal gives one.
package placement

import (
	"context"
	"errors"
	"time"

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
			if environment.Phase != inventory.EnvironmentRunning || environment.Workshop != workshop {
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
				if inventory.SessionLive(session.Phase) {
					sessions = append(sessions, session)
				}
			}
		}
	}
	return sessions
}

// DefaultTimeout is the default of Options.Timeout.
const DefaultTimeout = 12 * time.Second

// Options configure the placer.
type Options struct {
	// Timeout bounds the placement of one session request, every portal it
	// contacts included.
	Timeout time.Duration
}

func (o Options) withDefaults() Options {
	if o.Timeout <= 0 {
		o.Timeout = DefaultTimeout
	}
	return o
}

// Placer requests sessions from training portals, recording each request in
// the inventory while it is in flight.
type Placer struct {
	inventory *inventory.Inventory
	portals   *trainingportal.Client
	ordering  Ordering
	timeout   time.Duration
	log       logr.Logger
}

// NewPlacer returns a placer that requests sessions through the portal
// client and records them in inv.
func NewPlacer(inv *inventory.Inventory, portals *trainingportal.Client, opts Options, log logr.Logger) *Placer {
	opts = opts.withDefaults()
	return &Placer{inventory: inv, portals: portals, ordering: NewOrdering(), timeout: opts.Timeout, log: log}
}

// Placement is a session a portal gave, and the environment it is of.
type Placement struct {
	Environment *inventory.Environment
	Session     trainingportal.Session
}

// errOutOfTime is the cause of a session request's context ending once the
// request has run out of time.
var errOutOfTime = errors.New("the session request ran out of time")

// Place gives the request's user, when it names one, their live session of
// the workshop on the portals back, reacquiring each such session in turn
// until its portal gives it. Otherwise it requests a new session of the
// workshop from the environments of the portals that offer it, in the order
// the ordering gives, until a portal gives one. A portal that could not be
// reached or did not answer is not contacted again for the request, and the
// request ends once its time runs out, whatever is left to try. Every attempt
// that fails is logged, and Place reports false when none succeeds.
func (p *Placer) Place(ctx context.Context, portals []*inventory.Portal, workshop string,
	request trainingportal.SessionRequest) (Placement, bool) {
	ctx, cancel := context.WithTimeoutCause(ctx, p.timeout, errOutOfTime)
	defer cancel()
	unanswered := unansweredPortals{}
	placement, ok := p.place(ctx, portals, workshop, request, unanswered)
	if !ok && errors.Is(context.Cause(ctx), errOutOfTime) {
		p.log.Info("The session request ran out of time before a portal gave a session",
			"workshop", workshop, "timeout", p.timeout.String())
	}
	return placement, ok
}

// place is Place within the request's time, recording in unanswered each
// portal that got no answer.
func (p *Placer) place(ctx context.Context, portals []*inventory.Portal, workshop string,
	request trainingportal.SessionRequest, unanswered unansweredPortals) (Placement, bool) {
	if request.User != "" {
		if placement, ok := p.reacquire(ctx, portals, workshop, request, unanswered); ok {
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
		if unanswered.skip(p.log, environment) {
			continue
		}
		inFlight := p.inventory.StartRequest(environment)
		session, err := p.portals.RequestSession(ctx, environment, request)
		if err != nil {
			inFlight.Failed()
			unanswered.record(environment.Portal, err)
			p.log.Error(err, "The portal did not give a session",
				append(environmentKeys(environment), "workshop", workshop)...)
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
	request trainingportal.SessionRequest, unanswered unansweredPortals) (Placement, bool) {
	for _, session := range SessionsOf(portals, workshop, request.User) {
		if ctx.Err() != nil {
			return Placement{}, false
		}
		environment := session.Environment
		if unanswered.skip(p.log, environment) {
			continue
		}
		reacquired, err := p.portals.ReacquireSession(ctx, session, request.IndexURL)
		if err != nil {
			unanswered.record(environment.Portal, err)
			p.log.Error(err, "The portal did not give the user's session back",
				append(environmentKeys(environment), "session", session.Name, "workshop", workshop)...)
			continue
		}
		return Placement{Environment: environment, Session: reacquired}, true
	}
	return Placement{}, false
}

// unansweredPortals are the portals that a session request could not reach,
// or that did not answer it in time. The request contacts none of them
// again, since each would most likely hold it as long again.
type unansweredPortals map[*inventory.Portal]bool

// record records the portal when the attempt's error says it got no answer.
func (u unansweredPortals) record(portal *inventory.Portal, err error) {
	if trainingportal.Unanswered(err) {
		u[portal] = true
	}
}

// skip reports whether the environment's portal got no answer before, and
// logs that it is passed over when it did.
func (u unansweredPortals) skip(log logr.Logger, environment *inventory.Environment) bool {
	if !u[environment.Portal] {
		return false
	}
	log.V(1).Info("Passing over the environment, whose portal did not answer earlier in the session request",
		environmentKeys(environment)...)
	return true
}

// environmentKeys are the keys and values a log line names the environment
// with: its cluster, its portal and its own name.
func environmentKeys(environment *inventory.Environment) []any {
	portal := environment.Portal
	return []any{"cluster", portal.Cluster.Name, "portal", portal.Name, "environment", environment.Name}
}
