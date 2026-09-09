package universe

import "testing"

func TestAPopulationSpreadsAcrossTheWholeMap(t *testing.T) {
	limits := Limits{Galaxies: 3, Systems: 100, Positions: 15}
	const count = 50

	galaxies, systems := map[int]bool{}, map[int]bool{}
	for index := range count {
		aim := Spread(limits, index, count)
		if err := aim.Validate(limits); err != nil {
			t.Fatalf("Spread(%d) = %s: %v", index, aim, err)
		}
		if aim.System == 1 {
			t.Fatalf("Spread(%d) = %s, aiming at the corner the first players hold", index, aim)
		}
		if index > 0 {
			if previous := Spread(limits, index-1, count); previous.Galaxy == aim.Galaxy {
				t.Fatalf("Spread(%d) = %s and Spread(%d) = %s share a galaxy", index-1, previous, index, aim)
			}
		}
		galaxies[aim.Galaxy], systems[aim.System] = true, true
	}
	if len(galaxies) != limits.Galaxies {
		t.Fatalf("population reached %d galaxies, want %d", len(galaxies), limits.Galaxies)
	}
	if len(systems) < 8 {
		t.Fatalf("population reached %d systems, want it spread wider", len(systems))
	}
}

func TestASmallOrACrampedPopulationStillAimsSomewhere(t *testing.T) {
	for _, this := range []struct {
		name   string
		limits Limits
		count  int
	}{
		{"one player", Limits{Galaxies: 3, Systems: 100, Positions: 15}, 1},
		{"one galaxy", Limits{Galaxies: 1, Systems: 10, Positions: 15}, 20},
		{"more players than systems", Limits{Galaxies: 1, Systems: 2, Positions: 15}, 30},
	} {
		t.Run(this.name, func(t *testing.T) {
			for index := range this.count {
				aim := Spread(this.limits, index, this.count)
				if err := aim.Validate(this.limits); err != nil {
					t.Fatalf("Spread(%d) = %s: %v", index, aim, err)
				}
			}
		})
	}
}

func TestAnAimIsRefusedWhenThereIsNothingToAimAt(t *testing.T) {
	limits := Limits{Galaxies: 3, Systems: 100, Positions: 15}
	for _, this := range []struct {
		name   string
		limits Limits
		index  int
		count  int
	}{
		{"nobody to place", limits, 0, 0},
		{"a negative batch", limits, 0, -1},
		{"no galaxy", Limits{Systems: 100, Positions: 15}, 0, 50},
		{"no system", Limits{Galaxies: 3, Positions: 15}, 0, 50},
		{"no position", Limits{Galaxies: 3, Systems: 100}, 0, 50},
	} {
		t.Run(this.name, func(t *testing.T) {
			if aim := Spread(this.limits, this.index, this.count); aim != (Coordinate{}) {
				t.Fatalf("Spread() = %s, want the zero coordinate", aim)
			}
		})
	}
}

func TestAnAimAskedForBeforeTheFirstIsTheFirst(t *testing.T) {
	limits := Limits{Galaxies: 3, Systems: 100, Positions: 15}
	if Spread(limits, -1, 50) != Spread(limits, 0, 50) {
		t.Fatalf("Spread(-1) = %s, want %s", Spread(limits, -1, 50), Spread(limits, 0, 50))
	}
}
