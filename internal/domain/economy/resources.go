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

// Refund gives a cost back without ever passing the storage capacity. It
// returns the new stock and what the full stores could not hold, so the caller
// can tell the player exactly what the refund cost them.
func (r Resources) Refund(amount, capacity Resources) (Resources, Resources) {
	metal, lostMetal := refundOne(r.Metal, amount.Metal, capacity.Metal)
	crystal, lostCrystal := refundOne(r.Crystal, amount.Crystal, capacity.Crystal)
	deuterium, lostDeuterium := refundOne(r.Deuterium, amount.Deuterium, capacity.Deuterium)
	return Resources{Metal: metal, Crystal: crystal, Deuterium: deuterium},
		Resources{Metal: lostMetal, Crystal: lostCrystal, Deuterium: lostDeuterium}
}

// refundOne never adds beyond the capacity, which is also how it stays clear of
// any overflow.
func refundOne(stock, amount, capacity int64) (int64, int64) {
	if amount <= 0 {
		return stock, 0
	}
	if stock >= capacity {
		return stock, amount
	}
	if room := capacity - stock; amount > room {
		return capacity, amount - room
	}
	return stock + amount, 0
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
