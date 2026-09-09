package building

import (
	"errors"
	"testing"
	"time"

	"universeatwar/internal/domain/economy"
	"universeatwar/internal/domain/prerequisite"
	"universeatwar/internal/domain/rules"
)

func TestCost(t *testing.T) {
	catalogue := DefaultCatalogue()
	tests := []struct {
		level int
		want  economy.Resources
	}{{1, economy.Resources{Metal: 60, Crystal: 15}}, {2, economy.Resources{Metal: 90, Crystal: 22}}}
	for _, test := range tests {
		got, err := catalogue.Cost(MetalMine, test.level, 1)
		if err != nil || got != test.want {
			t.Fatalf("Cost(level %d) = %#v, %v; want %#v", test.level, got, err, test.want)
		}
	}
}

func TestDuration(t *testing.T) {
	catalogue := DefaultCatalogue()
	cost := economy.Resources{Metal: 60, Crystal: 15}
	for _, test := range []struct {
		name     string
		robotics int
		speed    float64
		want     time.Duration
	}{{"base", 0, 1, 108 * time.Second}, {"robotics", 1, 1, 54 * time.Second}, {"minimum", 0, 1000, time.Second}} {
		t.Run(test.name, func(t *testing.T) {
			got, err := catalogue.Duration(cost, test.robotics, 0, test.speed)
			if err != nil || got != test.want {
				t.Fatalf("Duration() = %v, %v; want %v", got, err, test.want)
			}
		})
	}
}

func TestValidateStart(t *testing.T) {
	catalogue := DefaultCatalogue()
	configured := rules.Default()
	levels := Levels{RoboticsFactory: 9}
	researches := prerequisite.Levels{"computer_technology": 10}
	if _, err := catalogue.Plan(NaniteFactory, OnPlanet, levels, researches, 20, 100, configured); err == nil {
		t.Fatal("missing prerequisite accepted")
	}
	levels[RoboticsFactory] = 10
	if _, err := catalogue.Plan(NaniteFactory, OnPlanet, levels, prerequisite.Levels{"computer_technology": 9}, 20, 100, configured); err == nil {
		t.Fatal("missing research prerequisite accepted")
	}
	plan, err := catalogue.Plan(NaniteFactory, OnPlanet, levels, researches, 20, 100, configured)
	if err != nil {
		t.Fatal(err)
	}
	if plan.TargetLevel != 1 || plan.Cost != (economy.Resources{Metal: 1_000_000, Crystal: 500_000, Deuterium: 100_000}) {
		t.Fatalf("plan = %#v", plan)
	}
	if _, err := catalogue.Plan(MetalMine, OnPlanet, Levels{}, nil, 1, 1, configured); err == nil {
		t.Fatal("full planet accepted")
	}
	if err := prerequisite.ValidateGraph(catalogue.RequirementEdges()); err != nil {
		t.Fatalf("default building graph invalid: %v", err)
	}
}

func TestPlacementSeparatesPlanetsFromMoons(t *testing.T) {
	catalogue := DefaultCatalogue()
	configured := rules.Default()
	planetLevels := Levels{RoboticsFactory: 2}
	moonLevels := Levels{LunarBase: 1}
	researches := prerequisite.Levels{"hyperspace_technology": 7}

	if _, err := catalogue.Plan(LunarBase, OnPlanet, planetLevels, researches, 0, 100, configured); !errors.Is(err, ErrWrongPlacement) {
		t.Fatalf("a lunar base on a planet error = %v, want ErrWrongPlacement", err)
	}
	if _, err := catalogue.Plan(MetalMine, OnMoon, moonLevels, researches, 0, 10, configured); !errors.Is(err, ErrWrongPlacement) {
		t.Fatalf("a mine on a moon error = %v, want ErrWrongPlacement", err)
	}
	if _, err := catalogue.Plan(LunarBase, OnMoon, Levels{}, researches, 0, 1, configured); err != nil {
		t.Fatalf("Plan(lunar base on a moon) error = %v", err)
	}
	if _, err := catalogue.Plan(SensorPhalanx, OnMoon, Levels{}, researches, 0, 4, configured); err == nil {
		t.Fatal("a phalanx was accepted without a lunar base")
	}
	if _, err := catalogue.Plan(JumpGate, OnMoon, moonLevels, prerequisite.Levels{}, 0, 4, configured); err == nil {
		t.Fatal("a jump gate was accepted without hyperspace technology")
	}

	moonBuildings := catalogue.DefinitionsFor(OnMoon)
	if len(moonBuildings) != 3 {
		t.Fatalf("the moon catalogue holds %d buildings, want 3", len(moonBuildings))
	}
	for _, definition := range catalogue.DefinitionsFor(OnPlanet) {
		if definition.Placement != OnPlanet {
			t.Fatalf("%s is not a planetary building", definition.ID)
		}
	}
}

// TestPlanCarriesTheEnergyItChanges checks the sign the card depends on: a mine
// costs the balance, the plant gives to it, and nothing else touches it.
func TestPlanCarriesTheEnergyItChanges(t *testing.T) {
	catalogue := DefaultCatalogue()
	configured := rules.Default()
	growth := configured.Economy.EnergyConsumptionGrowth

	mine, err := catalogue.Plan(MetalMine, OnPlanet, Levels{MetalMine: 12}, nil, 20, 100, configured)
	if err != nil {
		t.Fatalf("Plan(metal mine) error = %v", err)
	}
	before, _ := economy.MineEnergy(12, growth)
	after, _ := economy.MineEnergy(13, growth)
	if mine.EnergyChange != before-after {
		t.Fatalf("a mine changes energy by %d, want %d", mine.EnergyChange, before-after)
	}
	if mine.EnergyChange >= 0 {
		t.Fatalf("a mine level should cost energy, changed by %d", mine.EnergyChange)
	}

	plant, err := catalogue.Plan(SolarPlant, OnPlanet, Levels{SolarPlant: 13}, nil, 20, 100, configured)
	if err != nil {
		t.Fatalf("Plan(solar plant) error = %v", err)
	}
	yieldBefore, _ := economy.SolarPlantEnergy(13)
	yieldAfter, _ := economy.SolarPlantEnergy(14)
	if plant.EnergyChange != yieldAfter-yieldBefore {
		t.Fatalf("a plant changes energy by %d, want %d", plant.EnergyChange, yieldAfter-yieldBefore)
	}
	if plant.EnergyChange <= 0 {
		t.Fatalf("a plant level should give energy, changed by %d", plant.EnergyChange)
	}

	for _, id := range []ID{MetalStorage, RoboticsFactory, ResearchLab} {
		plan, err := catalogue.Plan(id, OnPlanet, Levels{}, nil, 20, 100, configured)
		if err != nil {
			t.Fatalf("Plan(%s) error = %v", id, err)
		}
		if plan.EnergyChange != 0 {
			t.Fatalf("%s changed energy by %d, want none", id, plan.EnergyChange)
		}
	}
}

// TestPlanNamesWhyItRefuses keeps the reasons a caller must tell apart from one
// another, rather than folding them into one opaque error.
func TestPlanNamesWhyItRefuses(t *testing.T) {
	catalogue := DefaultCatalogue()
	configured := rules.Default()
	if _, err := catalogue.Plan(MetalMine, OnPlanet, Levels{}, nil, 1, 1, configured); !errors.Is(err, ErrNoFreeField) {
		t.Fatalf("a full planet error = %v, want ErrNoFreeField", err)
	}
	if _, err := catalogue.Plan("not_a_building", OnPlanet, Levels{}, nil, 0, 100, configured); !errors.Is(err, ErrUnknownBuilding) {
		t.Fatalf("an unknown building error = %v, want ErrUnknownBuilding", err)
	}
}
