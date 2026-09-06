package ai

import "universeatwar/internal/domain/universe"

// Layer names the horizon a decision belongs to.
type Layer string

const (
	Strategic   Layer = "strategic"
	Operational Layer = "operational"
	Tactical    Layer = "tactical"
)

// Valid reports whether the layer is one this build knows.
func (l Layer) Valid() bool {
	switch l {
	case Strategic, Operational, Tactical:
		return true
	default:
		return false
	}
}

// Outcome is what became of a decision once the game had its say.
type Outcome string

const (
	Done    Outcome = "done"
	Skipped Outcome = "skipped"
	Failed  Outcome = "failed"
)

// Valid reports whether the outcome is one this build knows.
func (o Outcome) Valid() bool {
	switch o {
	case Done, Skipped, Failed:
		return true
	default:
		return false
	}
}

// Decision is one line of the diary of an artificial player. It exists for
// diagnosis: it explains what was attempted and why, never what was secretly
// known.
type Decision struct {
	Layer   Layer
	Action  string
	Outcome Outcome
	Reason  string
	Score   float64
	BodyID  int64
	Target  *universe.Coordinate
}

// Skip records a decision that was considered and left alone.
func Skip(layer Layer, action, reason string) Decision {
	return Decision{Layer: layer, Action: action, Outcome: Skipped, Reason: reason}
}
