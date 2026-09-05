// Package server contains the universe server lifecycle rules.
package server

import "fmt"

// State is the persisted lifecycle state of a Universe At War server.
type State string

const (
	BootstrapPending State = "BOOTSTRAP_PENDING"
	SetupInProgress  State = "SETUP_IN_PROGRESS"
	Running          State = "RUNNING"
	Paused           State = "PAUSED"
	Maintenance      State = "MAINTENANCE"
)

// Validate rejects states outside the explicit state machine.
func (s State) Validate() error {
	switch s {
	case BootstrapPending, SetupInProgress, Running, Paused, Maintenance:
		return nil
	default:
		return fmt.Errorf("server: invalid state %q", s)
	}
}

// CanTransition reports whether a direct lifecycle transition is allowed.
// Restoring from maintenance is limited to states from which maintenance can
// be entered; the application layer additionally checks the persisted previous
// state.
func CanTransition(from, to State) bool {
	switch from {
	case BootstrapPending:
		return to == SetupInProgress
	case SetupInProgress:
		return to == Running
	case Running:
		return to == Paused || to == Maintenance
	case Paused:
		return to == Running || to == Maintenance
	case Maintenance:
		return to == Running || to == Paused
	default:
		return false
	}
}
