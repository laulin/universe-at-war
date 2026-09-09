package espionage

import (
	"errors"
	"testing"

	"universeatwar/internal/domain/building"
	"universeatwar/internal/domain/economy"
	"universeatwar/internal/domain/random"
	"universeatwar/internal/domain/research"
	"universeatwar/internal/domain/rules"
	"universeatwar/internal/domain/unit"
)

func truthOfARichPlanet() Truth {
	return Truth{
		Resources: economy.Resources{Metal: 1000, Crystal: 500, Deuterium: 100},
		Fleet:     unit.Inventory{unit.LightFighter: 20},
		Defenses:  unit.Inventory{unit.RocketLauncher: 10},
		Buildings: building.Levels{building.MetalMine: 5},
		Research:  research.Levels{research.EnergyTechnology: 3},
	}
}

func TestLevelCombinesTechnologyGapAndProbes(t *testing.T) {
	tests := []struct {
		attacker, defender int
		probes             int64
		want               int
	}{
		{attacker: 0, defender: 0, probes: 1, want: 1},
		{attacker: 0, defender: 0, probes: 4, want: 2},
		{attacker: 0, defender: 0, probes: 9, want: 3},
		{attacker: 0, defender: 0, probes: 16, want: 4},
		{attacker: 0, defender: 0, probes: 25, want: 5},
		{attacker: 0, defender: 0, probes: 8, want: 2},
		{attacker: 5, defender: 0, probes: 1, want: 6},
		{attacker: 0, defender: 3, probes: 1, want: -2},
	}
	for _, test := range tests {
		if got := Level(test.attacker, test.defender, test.probes); got != test.want {
			t.Fatalf("Level(%d, %d, %d) = %d, want %d", test.attacker, test.defender, test.probes, got, test.want)
		}
	}
}

func TestRevealFollowsTheThresholds(t *testing.T) {
	settings := rules.Default().Espionage
	truth := truthOfARichPlanet()

	nothing, err := Reveal(settings, 0, 3, 1, truth)
	if err != nil {
		t.Fatalf("Reveal() error = %v", err)
	}
	if nothing.Resources != nil || nothing.Fleet != nil || nothing.Defenses != nil || nothing.Buildings != nil || nothing.Research != nil {
		t.Fatalf("a level of %d revealed something: %+v", nothing.Level, nothing)
	}

	resourcesOnly, _ := Reveal(settings, 0, 0, 1, truth)
	if resourcesOnly.Resources == nil || *resourcesOnly.Resources != truth.Resources {
		t.Fatalf("resources = %+v", resourcesOnly.Resources)
	}
	if resourcesOnly.Fleet != nil || resourcesOnly.Defenses != nil {
		t.Fatalf("level one revealed too much: %+v", resourcesOnly)
	}

	withFleet, _ := Reveal(settings, 0, 0, 4, truth)
	if withFleet.Fleet[unit.LightFighter] != 20 || withFleet.Defenses != nil {
		t.Fatalf("level two = %+v", withFleet)
	}
	everything, _ := Reveal(settings, 5, 0, 1, truth)
	if everything.Defenses[unit.RocketLauncher] != 10 ||
		everything.Buildings[building.MetalMine] != 5 ||
		everything.Research[research.EnergyTechnology] != 3 {
		t.Fatalf("a high level did not reveal everything: %+v", everything)
	}

	if _, err := Reveal(settings, 0, 0, 0, truth); !errors.Is(err, ErrNoProbe) {
		t.Fatalf("Reveal() without probes error = %v", err)
	}
}

func TestRevealDoesNotAliasTheTruth(t *testing.T) {
	settings := rules.Default().Espionage
	truth := truthOfARichPlanet()
	report, _ := Reveal(settings, 5, 0, 1, truth)

	truth.Fleet[unit.LightFighter] = 999
	truth.Buildings[building.MetalMine] = 99
	truth.Research[research.EnergyTechnology] = 99
	truth.Resources.Metal = 999

	if report.Fleet[unit.LightFighter] != 20 || report.Buildings[building.MetalMine] != 5 ||
		report.Research[research.EnergyTechnology] != 3 || report.Resources.Metal != 1000 {
		t.Fatalf("the report follows later changes of the truth: %+v", report)
	}
}

func TestDetectionChanceGrowsWithProbesAndDefenders(t *testing.T) {
	settings := rules.Default().Espionage
	tests := []struct {
		name               string
		attacker, defender int
		probes, fleet      int64
		want               float64
	}{
		{name: "no defending ship", probes: 10, fleet: 0, want: 0},
		{name: "no probe", probes: 0, fleet: 10, want: 0},
		{name: "equal technology", probes: 1, fleet: 1, want: .0025},
		{name: "ten probes", probes: 10, fleet: 1, want: .025},
		{name: "two levels behind", defender: 2, probes: 1, fleet: 1, want: .01},
		{name: "certain detection", probes: 1000, fleet: 1000, want: 1},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := DetectionChance(settings, test.attacker, test.defender, test.probes, test.fleet)
			if got != test.want {
				t.Fatalf("DetectionChance() = %v, want %v", got, test.want)
			}
		})
	}
}

func TestDetectedConsumesNoDrawWhenImpossible(t *testing.T) {
	if Detected(0, random.NewScript(nil, nil)) {
		t.Fatal("an impossible detection happened")
	}
	if !Detected(.5, random.NewScript(nil, []float64{.1})) {
		t.Fatal("a draw of 0.1 must detect at probability 0.5")
	}
}

// A planet that holds nothing gives back sections that are empty rather than
// absent: the mission earned them, and what it earned is the knowledge that
// there is nothing to find.
func TestARevealedSectionOfAnEmptyPlanetIsEmptyRatherThanAbsent(t *testing.T) {
	settings := rules.Default().Espionage
	bare := Truth{Resources: economy.Resources{Metal: 40}}

	report, err := Reveal(settings, 6, 0, 10, bare)
	if err != nil {
		t.Fatalf("Reveal() error = %v", err)
	}
	if report.Level < settings.ResearchThreshold {
		t.Fatalf("level %d does not reach every threshold", report.Level)
	}
	if report.Fleet == nil || report.Defenses == nil || report.Buildings == nil || report.Research == nil {
		t.Fatalf("a revealed section of an empty planet came back unrevealed: %#v", report)
	}
	if len(report.Fleet) != 0 || len(report.Defenses) != 0 || len(report.Research) != 0 {
		t.Fatalf("an empty planet reported contents: %#v", report)
	}
}
