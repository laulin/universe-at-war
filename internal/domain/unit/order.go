package unit

import "time"

// Delivered is the number of units an order has finished at a given instant.
// The arithmetic is integer and monotonic, so settling twice in a row delivers
// exactly what a single settlement would have delivered.
func Delivered(startedAt time.Time, unitDuration time.Duration, quantity int64, now time.Time) int64 {
	if quantity <= 0 || unitDuration <= 0 || now.Before(startedAt) {
		return 0
	}
	produced := int64(now.Sub(startedAt) / unitDuration)
	if produced > quantity {
		return quantity
	}
	return produced
}
