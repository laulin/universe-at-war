package ai

import "strconv"

// nameEpithets and nameRoots are the two halves every name of a whole
// population is drawn from. They are deliberately plain words: a name a
// server-driven player wears has to be a name a human could have chosen, since
// the two sit in the same list and answer to the same rules.
var (
	nameEpithets = []string{
		"Aube", "Basalte", "Cendre", "Dune", "Eclat", "Faucon",
		"Givre", "Horizon", "Lumen", "Mirage", "Nadir", "Orage",
	}
	nameRoots = []string{
		"Kepler", "Vega", "Altair", "Rigel", "Antares", "Deneb", "Sirius", "Polaris",
		"Arcturus", "Bellatrix", "Capella", "Procyon", "Mizar", "Castor", "Pollux", "Spica",
	}
)

// Name is the name the nth artificial player of a universe wears. Two indexes
// never give the same name, and never two names that differ only by their case
// either: an account is unique on its lowered form, so a pair like that would
// be a collision the caller could do nothing about.
//
// The pairs run out after a while, and a rank is appended from there rather
// than a second word being invented, which keeps the name inside the thirty-two
// characters a display name is allowed.
func Name(index int) string {
	if index < 0 {
		index = 0
	}
	pairs := len(nameEpithets) * len(nameRoots)
	pair, cycle := index%pairs, index/pairs
	name := nameEpithets[pair%len(nameEpithets)] + " " + nameRoots[pair/len(nameEpithets)]
	if cycle > 0 {
		name += " " + strconv.Itoa(cycle+1)
	}
	return name
}
