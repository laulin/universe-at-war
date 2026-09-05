package research

import (
	"errors"
	"math"
	"testing"
	"time"

	"universeatwar/internal/domain/economy"
	"universeatwar/internal/domain/prerequisite"
	"universeatwar/internal/domain/rules"
)

func TestCostFollowsTheCatalogueTable(t *testing.T) {
	catalogue := DefaultCatalogue()
	tests := []struct {
		name       string
		id         ID
		level      int
		multiplier float64
		want       economy.Resources
		wantEnergy int64
	}{
		{name: "energy level one", id: EnergyTechnology, level: 1, multiplier: 1, want: economy.Resources{Crystal: 800, Deuterium: 400}},
		{name: "energy level five", id: EnergyTechnology, level: 5, multiplier: 1, want: economy.Resources{Crystal: 12800, Deuterium: 6400}},
		{name: "astrophysics level two", id: Astrophysics, level: 2, multiplier: 1, want: economy.Resources{Metal: 7000, Crystal: 14000, Deuterium: 7000}},
		{name: "astrophysics level ten", id: Astrophysics, level: 10, multiplier: 1, want: economy.Resources{Metal: 615747, Crystal: 1231494, Deuterium: 615747}},
		{name: "halved weapons", id: WeaponsTechnology, level: 1, multiplier: .5, want: economy.Resources{Metal: 400, Crystal: 100}},
		{name: "graviton costs energy only", id: GravitonTechnology, level: 1, multiplier: 1, wantEnergy: 300_000},
		{name: "graviton level three", id: GravitonTechnology, level: 3, multiplier: 1, wantEnergy: 2_700_000},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			cost, energy, err := catalogue.Cost(test.id, test.level, test.multiplier)
			if err != nil {
				t.Fatalf("Cost() error = %v", err)
			}
			if cost != test.want || energy != test.wantEnergy {
				t.Fatalf("Cost() = %v %d, want %v %d", cost, energy, test.want, test.wantEnergy)
			}
		})
	}

	if _, _, err := catalogue.Cost(EnergyTechnology, 0, 1); err == nil {
		t.Fatal("Cost() accepted level zero")
	}
	if _, _, err := catalogue.Cost(EnergyTechnology, 400, 1); err == nil {
		t.Fatal("Cost() accepted an unrepresentable level")
	}
	if _, _, err := catalogue.Cost("unknown_technology", 1, 1); err == nil {
		t.Fatal("Cost() accepted an unknown research")
	}
}

func TestDurationUsesTheLaboratoryNetwork(t *testing.T) {
	catalogue := DefaultCatalogue()
	cost := economy.Resources{Crystal: 800, Deuterium: 400}
	tests := []struct {
		name         string
		laboratories Laboratories
		minimum      int
		bonus        float64
		speed        float64
		want         time.Duration
	}{
		{name: "no laboratory", laboratories: Laboratories{}, minimum: 0, bonus: 1, speed: 1, want: 2880 * time.Second},
		{name: "single laboratory", laboratories: Laboratories{Local: 1}, minimum: 1, bonus: 1, speed: 1, want: 1440 * time.Second},
		{name: "network adds the best eligible laboratories", laboratories: Laboratories{Local: 1, Remote: []int{3, 5, 2}, NetworkLevel: 2}, minimum: 1, bonus: 1, speed: 1, want: 288 * time.Second},
		{name: "remote laboratory below the requirement is ignored", laboratories: Laboratories{Local: 1, Remote: []int{5}, NetworkLevel: 1}, minimum: 6, bonus: 1, speed: 1, want: 1440 * time.Second},
		{name: "network larger than the empire", laboratories: Laboratories{Local: 1, Remote: []int{3}, NetworkLevel: 9}, minimum: 1, bonus: 1, speed: 1, want: 576 * time.Second},
		{name: "laboratory bonus", laboratories: Laboratories{Local: 1}, minimum: 1, bonus: 2, speed: 1, want: 960 * time.Second},
		{name: "minimum of one second", laboratories: Laboratories{Local: 1}, minimum: 1, bonus: 1, speed: 10000, want: time.Second},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := catalogue.Duration(cost, test.laboratories, test.minimum, test.bonus, test.speed)
			if err != nil {
				t.Fatalf("Duration() error = %v", err)
			}
			if got != test.want {
				t.Fatalf("Duration() = %v, want %v", got, test.want)
			}
		})
	}
}

func TestPlanChecksPrerequisitesAndTargetsTheNextLevel(t *testing.T) {
	catalogue := DefaultCatalogue()
	configured := rules.Default()
	state := prerequisite.State{
		Buildings:  prerequisite.Levels{"research_lab": 1},
		Researches: prerequisite.Levels{},
	}

	plan, err := catalogue.Plan(EnergyTechnology, state, Laboratories{Local: 1}, configured)
	if err != nil {
		t.Fatalf("Plan() error = %v", err)
	}
	if plan.TargetLevel != 1 || plan.Duration != 1440*time.Second || plan.EffectiveLaboratory != 1 {
		t.Fatalf("Plan() = %+v", plan)
	}
	if plan.Cost != (economy.Resources{Crystal: 800, Deuterium: 400}) || plan.Energy != 0 {
		t.Fatalf("Plan() cost = %v %d", plan.Cost, plan.Energy)
	}

	state.Researches["energy_technology"] = 1
	next, err := catalogue.Plan(EnergyTechnology, state, Laboratories{Local: 1}, configured)
	if err != nil || next.TargetLevel != 2 {
		t.Fatalf("Plan() second level = %+v %v", next, err)
	}

	if _, err := catalogue.Plan(LaserTechnology, state, Laboratories{Local: 1}, configured); !errors.Is(err, prerequisite.ErrUnmet) {
		t.Fatalf("Plan() without the energy prerequisite error = %v", err)
	}
	bare := prerequisite.State{Buildings: prerequisite.Levels{}, Researches: prerequisite.Levels{}}
	if _, err := catalogue.Plan(EnergyTechnology, bare, Laboratories{}, configured); !errors.Is(err, prerequisite.ErrUnmet) {
		t.Fatalf("Plan() without a laboratory error = %v", err)
	}
}

func TestGravitonPlanCostsEnergyAndOneSecond(t *testing.T) {
	catalogue := DefaultCatalogue()
	state := prerequisite.State{
		Buildings:  prerequisite.Levels{"research_lab": 12},
		Researches: prerequisite.Levels{},
	}
	plan, err := catalogue.Plan(GravitonTechnology, state, Laboratories{Local: 12}, rules.Default())
	if err != nil {
		t.Fatalf("Plan() error = %v", err)
	}
	if plan.Cost != (economy.Resources{}) || plan.Energy != 300_000 || plan.Duration != time.Second {
		t.Fatalf("Plan() = %+v", plan)
	}
}

func TestDefaultCatalogueGraphIsAcyclicAndComplete(t *testing.T) {
	catalogue := DefaultCatalogue()
	definitions := catalogue.Definitions()
	if len(definitions) != 16 {
		t.Fatalf("catalogue holds %d researches, want 16", len(definitions))
	}
	for _, definition := range definitions {
		if definition.ID == "" || definition.Growth <= 1 {
			t.Fatalf("invalid definition %+v", definition)
		}
		if err := definition.BaseCost.Validate(); err != nil {
			t.Fatalf("definition %s has an invalid cost: %v", definition.ID, err)
		}
		if definition.BaseCost == (economy.Resources{}) && definition.BaseEnergy <= 0 {
			t.Fatalf("definition %s costs nothing at all", definition.ID)
		}
		for _, requirement := range definition.Prerequisites {
			if requirement.Level <= 0 {
				t.Fatalf("definition %s has a null requirement", definition.ID)
			}
			if requirement.Kind == prerequisite.Research {
				if _, known := catalogue.Definition(ID(requirement.ID)); !known {
					t.Fatalf("definition %s requires unknown research %s", definition.ID, requirement.ID)
				}
			}
		}
	}
	if err := prerequisite.ValidateGraph(catalogue.RequirementEdges()); err != nil {
		t.Fatalf("default catalogue graph invalid: %v", err)
	}
}

func FuzzCostStaysRepresentable(f *testing.F) {
	f.Add(3, 1.0)
	f.Add(1, 0.25)
	f.Fuzz(func(t *testing.T, level int, multiplier float64) {
		cost, energy, err := DefaultCatalogue().Cost(Astrophysics, level, multiplier)
		if err != nil {
			return
		}
		if cost.Validate() != nil || energy < 0 {
			t.Fatalf("Cost(%d, %v) = %v %d", level, multiplier, cost, energy)
		}
		if math.IsNaN(multiplier) || math.IsInf(multiplier, 0) {
			t.Fatalf("Cost() accepted multiplier %v", multiplier)
		}
	})
}
