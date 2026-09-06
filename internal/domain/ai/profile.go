package ai

import "time"

// Profile is what an artificial player is: a character, a timetable, a rhythm
// and a seed. It carries no secret and no privilege.
type Profile struct {
	PlayerID  int64
	AccountID int64
	Name      string
	Archetype Archetype
	Window    Window
	Interval  time.Duration
	Seed      int64
	Tick      int64
	Retired   bool
}

// Validate refuses a profile this build could not run faithfully.
func (p Profile) Validate() error {
	if !p.Archetype.Valid() {
		return ErrUnknownArchetype
	}
	if !p.Window.Valid() {
		return ErrInvalidWindow
	}
	if p.Interval <= 0 {
		return ErrInvalidInterval
	}
	return nil
}

// Preferences is a shortcut to the weights of the character.
func (p Profile) Preferences() Preferences {
	return p.Archetype.Preferences()
}
