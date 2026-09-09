package ai

// Difficulty is how well a universe wants its server-driven players to play. It
// is a property of the universe and not of a character: two archetypes at the
// same difficulty stay as different from each other as they ever were.
type Difficulty string

const (
	Easy   Difficulty = "easy"
	Normal Difficulty = "normal"
	Hard   Difficulty = "hard"
)

// Difficulties lists every setting this build knows, in a stable order.
func Difficulties() []Difficulty {
	return []Difficulty{Easy, Normal, Hard}
}

// Valid reports whether the difficulty is one this build knows.
func (d Difficulty) Valid() bool {
	switch d {
	case Easy, Normal, Hard:
		return true
	default:
		return false
	}
}

// At bends the weights of a character towards the competence its universe asked
// for. An easy player wants a wider margin before it strikes, overestimates what
// it is walking into, expects less for its trouble and sends one probe fewer, so
// it goes out less often and sees less when it does; a hard one is the reverse.
//
// What it deliberately leaves alone is as much of the design as what it moves.
// How fast a player develops is the archetype's business, not the universe's, so
// a lenient setting must not hand its players a quieter route to a bigger
// empire. And the fleetsave is a rule of playing properly rather than a dial: a
// player that left its fleet parked outside its hours would simply be playing
// wrongly, at any difficulty.
//
// Anything other than easy or hard leaves the character as it is, which is what
// normal means and also what an unreadable setting deserves.
func (p Preferences) At(difficulty Difficulty) Preferences {
	switch difficulty {
	case Easy:
		p.Greed *= .80
		p.Caution *= 1.30
		p.SafetyMargin *= 1.50
		p.RaidThreshold *= 1.50
		p.Probes = max(1, p.Probes-1)
	case Hard:
		p.Greed *= 1.15
		p.Caution *= .85
		p.SafetyMargin *= .75
		p.RaidThreshold *= .70
		p.Probes++
	}
	return p
}
