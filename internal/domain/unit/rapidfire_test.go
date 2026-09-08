package unit

import "testing"

// A card lists what a unit shoots fast at in the order the interface shows the
// catalogue, so two cards never disagree on where an entry belongs.
func TestRapidFireOfListsTargetsInCatalogueOrder(t *testing.T) {
	got := DefaultCatalogue().RapidFireOf(Cruiser)
	want := []Volley{
		{Unit: LightFighter, Shots: 6},
		{Unit: EspionageProbe, Shots: 5},
		{Unit: SolarSatellite, Shots: 5},
		{Unit: RocketLauncher, Shots: 10},
	}
	assertVolleys(t, got, want)
}

// The question a player actually asks is what tears their ship apart, which is
// the same table read the other way round.
func TestRapidFireAgainstReadsTheCatalogueBackwards(t *testing.T) {
	got := DefaultCatalogue().RapidFireAgainst(Cruiser)
	want := []Volley{
		{Unit: Deathstar, Shots: 33},
		{Unit: Battlecruiser, Shots: 4},
	}
	assertVolleys(t, got, want)
}

// A defence inflicts no rapid fire at all and suffers a great deal of it, which
// is the half of the table that makes its card worth reading.
func TestADefenceOnlySuffersRapidFire(t *testing.T) {
	catalogue := DefaultCatalogue()
	if inflicted := catalogue.RapidFireOf(RocketLauncher); len(inflicted) != 0 {
		t.Fatalf("a rocket launcher inflicts rapid fire: %v", inflicted)
	}
	assertVolleys(t, catalogue.RapidFireAgainst(RocketLauncher), []Volley{
		{Unit: Cruiser, Shots: 10},
		{Unit: Bomber, Shots: 20},
		{Unit: Deathstar, Shots: 200},
	})
}

// A unit the catalogue never names is not a hole to fall into.
func TestRapidFireOfAnUnknownUnitIsEmpty(t *testing.T) {
	catalogue := DefaultCatalogue()
	if got := catalogue.RapidFireOf("nothing"); len(got) != 0 {
		t.Fatalf("RapidFireOf(unknown) = %v", got)
	}
	if got := catalogue.RapidFireAgainst("nothing"); len(got) != 0 {
		t.Fatalf("RapidFireAgainst(unknown) = %v", got)
	}
}

func assertVolleys(t *testing.T, got, want []Volley) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for index := range want {
		if got[index] != want[index] {
			t.Fatalf("entry %d = %v, want %v (whole list %v)", index, got[index], want[index], got)
		}
	}
}
