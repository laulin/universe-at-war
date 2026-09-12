package ai

import "time"

// Profile is what an artificial player is: a character, a timetable, a rhythm
// and a seed. It carries no secret and no privilege.
type Profile struct {
	PlayerID  int64
	AccountID int64
	Name      string
	Archetype Archetype
	// Difficulty is the competence its universe asked of it. An empty setting
	// plays as normal, which is what an artificial player made before a universe
	// had an opinion on the matter has always done.
	Difficulty Difficulty
	// Custom overrides the archetype defaults. Nil keeps following the
	// archetype, including future improvements made to it.
	Custom   *Tuning
	Window   Window
	Interval time.Duration
	Seed     int64
	Tick     int64
	Retired  bool
	Version  int64
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
	if p.Custom != nil {
		if err := p.Custom.Validate(); err != nil {
			return err
		}
	}
	return nil
}

// Preferences is a shortcut to the weights of the character.
func (p Profile) Preferences() Preferences {
	return p.Behaviour().Preferences.At(p.Difficulty)
}

// Behaviour returns either the individual configuration or the defaults of
// the current archetype.
func (p Profile) Behaviour() Tuning {
	if p.Custom != nil {
		return *p.Custom
	}
	return p.Archetype.Tuning()
}
