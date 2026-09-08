package unit

// Volley is one rapid-fire pairing: a unit, and how many shots the pairing
// grants against it.
type Volley struct {
	Unit  ID
	Shots int
}

// RapidFireOf lists what a unit shoots fast at.
func (c Catalogue) RapidFireOf(id ID) []Volley {
	definition, known := c.definitions[id]
	if !known {
		return nil
	}
	return c.volleys(func(target ID) int { return definition.RapidFire[target] })
}

// RapidFireAgainst lists what shoots fast at a unit, which is the same table
// read the other way round and the question a player actually asks.
func (c Catalogue) RapidFireAgainst(id ID) []Volley {
	if _, known := c.definitions[id]; !known {
		return nil
	}
	return c.volleys(func(attacker ID) int { return c.definitions[attacker].RapidFire[id] })
}

// volleys walks the catalogue in the order the interface displays it, so two
// cards never disagree on where an entry belongs. A single shot is no rapid
// fire at all and is left out, the way the battle itself treats it.
func (c Catalogue) volleys(shots func(ID) int) []Volley {
	var listed []Volley
	for _, id := range c.order {
		if count := shots(id); count > 1 {
			listed = append(listed, Volley{Unit: id, Shots: count})
		}
	}
	return listed
}
