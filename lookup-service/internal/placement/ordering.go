package placement

import (
	"cmp"
	"math/rand/v2"
	"slices"

	"github.com/educates/educates-training-platform/lookup-service/internal/inventory"
)

// Ordering decides the order in which a session request tries the workshop
// environments that could give it a session.
type Ordering interface {
	// Order returns the candidates in the order they are to be tried. It
	// leaves candidates as they are.
	Order(candidates []*inventory.Environment) []*inventory.Environment
}

// NewOrdering returns the ordering session requests use: by remaining
// capacity, with candidates that rank equally in random order.
func NewOrdering() Ordering {
	return capacityOrdering{}
}

// capacityOrdering ranks candidates, best first, by whether their portal
// has room, whether the environment has room, the portal's remaining
// capacity and the environment's reserved sessions, and the portal's and
// the environment's remaining capacity. A portal or environment without a
// limit counts as having one session left, so that those with a limit and
// room to spare come first. Sessions awaiting activation, in use or
// stopping, and requests in flight, are taken.
type capacityOrdering struct{}

func (capacityOrdering) Order(candidates []*inventory.Environment) []*inventory.Environment {
	ordered := slices.Clone(candidates)
	rand.Shuffle(len(ordered), func(i, j int) { ordered[i], ordered[j] = ordered[j], ordered[i] })
	slices.SortStableFunc(ordered, func(a, b *inventory.Environment) int {
		// Descending: the better candidate sorts first.
		return rankOf(b).compare(rankOf(a))
	})
	return ordered
}

// rank is what a candidate is ordered by, in order of precedence.
type rank struct {
	portalHasRoom        bool
	environmentHasRoom   bool
	portalRemaining      int64
	available            int
	environmentRemaining int64
}

func rankOf(e *inventory.Environment) rank {
	p := e.Portal
	r := rank{portalHasRoom: true, environmentHasRoom: true, portalRemaining: 1, environmentRemaining: 1,
		available: e.Available}
	// A portal's session maximum of 0 is no limit.
	if p.Capacity != 0 {
		r.portalRemaining = p.Capacity - int64(p.Allocated+p.InFlight)
		r.portalHasRoom = r.portalRemaining > 0
	}
	// An environment that gives no capacity has no limit; one whose capacity
	// is 0 has no room.
	if e.Capacity != nil {
		r.environmentRemaining = *e.Capacity - int64(e.Allocated+e.InFlight)
		r.environmentHasRoom = r.environmentRemaining > 0
	}
	return r
}

func (r rank) compare(o rank) int {
	return cmp.Or(
		compareBool(r.portalHasRoom, o.portalHasRoom),
		compareBool(r.environmentHasRoom, o.environmentHasRoom),
		cmp.Compare(r.portalRemaining, o.portalRemaining),
		cmp.Compare(r.available, o.available),
		cmp.Compare(r.environmentRemaining, o.environmentRemaining),
	)
}

func compareBool(a, b bool) int {
	switch {
	case a == b:
		return 0
	case a:
		return 1
	}
	return -1
}
