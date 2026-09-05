// Package clock defines the time source used at domain boundaries.
package clock

import "time"

// Clock supplies the effective current time to application use cases.
type Clock interface {
	Now() time.Time
}
