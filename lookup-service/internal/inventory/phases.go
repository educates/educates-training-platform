package inventory

// The phases of training resources the inventory looks at.
const (
	// EnvironmentRunning is the phase of a workshop environment that can be
	// given sessions.
	EnvironmentRunning = "Running"
	// PlaceholderPhase is the phase of a portal or environment the inventory
	// knows only from the resources that belong to it, such as a session
	// seen before its environment.
	PlaceholderPhase = "Unknown"

	// The phases of a workshop session: a reserved session waiting for a
	// user, a session waiting for its user to activate it, a session in
	// use, and a session being stopped.
	SessionAvailable  = "Available"
	SessionAllocating = "Allocating"
	SessionAllocated  = "Allocated"
	SessionStopping   = "Stopping"
)

// SessionTaken reports whether a session in the phase is taken, counting
// against its environment's and its portal's capacity: awaiting activation,
// in use or stopping.
func SessionTaken(phase string) bool {
	switch phase {
	case SessionAllocating, SessionAllocated, SessionStopping:
		return true
	}
	return false
}

// SessionLive reports whether a session in the phase is one its user can be
// given back: awaiting activation or in use. A stopping session is taken but
// not live.
func SessionLive(phase string) bool {
	switch phase {
	case SessionAllocating, SessionAllocated:
		return true
	}
	return false
}
