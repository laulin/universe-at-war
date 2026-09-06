// Package debris holds the wreckage of a battle and the rules of its recycling.
package debris

import "universeatwar/internal/domain/economy"

// Field is what a position holds after a battle. Deuterium never turns into
// debris.
type Field struct {
	Metal   int64
	Crystal int64
}

// Empty reports whether the field no longer exists.
func (f Field) Empty() bool {
	return f.Metal <= 0 && f.Crystal <= 0
}

// Total is the whole content of the field.
func (f Field) Total() int64 {
	return f.Metal + f.Crystal
}

// Harvest is what a fleet can lift out of a field. The hold is shared between
// the two resources, then the remainder goes to whichever still has room.
func Harvest(field Field, capacity int64) Field {
	if capacity <= 0 || field.Empty() {
		return Field{}
	}
	take := capacity
	if total := field.Total(); take > total {
		take = total
	}
	half := take / 2
	harvest := Field{
		Metal:   minimum(field.Metal, half),
		Crystal: minimum(field.Crystal, half),
	}
	remaining := take - harvest.Metal - harvest.Crystal
	harvest.Metal += minimum(field.Metal-harvest.Metal, remaining)
	remaining = take - harvest.Metal - harvest.Crystal
	harvest.Crystal += minimum(field.Crystal-harvest.Crystal, remaining)
	return harvest
}

// Capacity is the room a recycling fleet has for wreckage.
func Capacity(recyclers, recyclerCapacity, hold, carried int64) int64 {
	byRecyclers := recyclers * recyclerCapacity
	if recyclers > 0 && byRecyclers/recyclers != recyclerCapacity {
		byRecyclers = hold
	}
	room := hold - carried
	if room < 0 {
		room = 0
	}
	return minimum(byRecyclers, room)
}

// Resources converts a harvest into the resources it credits.
func (f Field) Resources() economy.Resources {
	return economy.Resources{Metal: f.Metal, Crystal: f.Crystal}
}

func minimum(first, second int64) int64 {
	if first < second {
		return first
	}
	return second
}
