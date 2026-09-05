// Package economy owns resource arithmetic and deterministic lazy production.
package economy

import "errors"

var ErrInsufficientResources = errors.New("economy: insufficient resources")

// Resources is a non-negative, whole-unit stock or cost.
type Resources struct {
	Metal     int64
	Crystal   int64
	Deuterium int64
}

func (r Resources) Validate() error {
	if r.Metal < 0 || r.Crystal < 0 || r.Deuterium < 0 {
		return errors.New("economy: resources cannot be negative")
	}
	return nil
}

// Covers reports whether every component of a cost is affordable.
func (r Resources) Covers(cost Resources) bool {
	return cost.Validate() == nil && r.Validate() == nil &&
		r.Metal >= cost.Metal && r.Crystal >= cost.Crystal && r.Deuterium >= cost.Deuterium
}

// Debit subtracts a cost without ever returning a negative balance.
func (r Resources) Debit(cost Resources) (Resources, error) {
	if !r.Covers(cost) {
		return Resources{}, ErrInsufficientResources
	}
	return Resources{
		Metal: r.Metal - cost.Metal, Crystal: r.Crystal - cost.Crystal,
		Deuterium: r.Deuterium - cost.Deuterium,
	}, nil
}
