package unit

import (
	"errors"
	"math"
	"testing"
	"time"

	"universeatwar/internal/domain/economy"
	"universeatwar/internal/domain/prerequisite"
	"universeatwar/internal/domain/research"
	"universeatwar/internal/domain/rules"
)

func shipyardState(shipyard int) prerequisite.State {
	return prerequisite.State{
		Buildings:  prerequisite.Levels{"shipyard": shipyard, "missile_silo": 4},
		Researches: prerequisite.Levels{"combustion_drive": 10, "impulse_drive": 10, "hyperspace_drive": 10, "energy_technology": 10, "laser_technology": 12, "ion_technology": 10, "hyperspace_technology": 10, "plasma_technology": 10, "shielding_technology": 10, "armour_technology": 10, "espionage_technology": 10, "graviton_technology": 1, "weapons_technology": 10},
	}
}

func shipyardInputs(shipyard, nanite, silo int) Inputs {
	return Inputs{
		State:            shipyardState(shipyard),
		ShipyardLevel:    shipyard,
		NaniteLevel:      nanite,
		MissileSiloLevel: silo,
		Inventory:        Inventory{},
		Pending:          Inventory{},
	}
}

func TestOrderComputesCostAndDuration(t *testing.T) {
	catalogue := DefaultCatalogue()
	configured := rules.Default()

	order, err := catalogue.Order(LightFighter, 3, shipyardInputs(1, 0, 4), configured)
	if err != nil {
		t.Fatalf("Order() error = %v", err)
	}
	if order.UnitCost != (economy.Resources{Metal: 3000, Crystal: 1000}) {
		t.Fatalf("unit cost = %v", order.UnitCost)
	}
	if order.TotalCost != (economy.Resources{Metal: 9000, Crystal: 3000}) {
		t.Fatalf("total cost = %v", order.TotalCost)
	}
	if order.UnitDuration != 2880*time.Second || order.Duration != 8640*time.Second {
		t.Fatalf("durations = %v %v", order.UnitDuration, order.Duration)
	}
	if order.Family != Ship {
		t.Fatalf("family = %q", order.Family)
	}

	faster, err := catalogue.Order(LightFighter, 1, shipyardInputs(3, 1, 4), configured)
	if err != nil || faster.UnitDuration != 720*time.Second {
		t.Fatalf("Order() with nanites = %v %v", faster.UnitDuration, err)
	}

	defense, err := catalogue.Order(RocketLauncher, 2, shipyardInputs(1, 0, 4), configured)
	if err != nil {
		t.Fatalf("Order() defense error = %v", err)
	}
	if defense.Family != Defense || defense.UnitDuration != 1440*time.Second || defense.TotalCost != (economy.Resources{Metal: 4000}) {
		t.Fatalf("defense order = %+v", defense)
	}
}

func TestOrderAppliesTheFamilyCostMultiplier(t *testing.T) {
	catalogue := DefaultCatalogue()
	configured := rules.Default()
	configured.Progression.ShipCostMultiplier = 2
	configured.Progression.DefenseCostMultiplier = .5

	ship, err := catalogue.Order(LightFighter, 1, shipyardInputs(1, 0, 4), configured)
	if err != nil || ship.UnitCost != (economy.Resources{Metal: 6000, Crystal: 2000}) {
		t.Fatalf("ship cost = %v %v", ship.UnitCost, err)
	}
	defense, err := catalogue.Order(RocketLauncher, 1, shipyardInputs(1, 0, 4), configured)
	if err != nil || defense.UnitCost != (economy.Resources{Metal: 1000}) {
		t.Fatalf("defense cost = %v %v", defense.UnitCost, err)
	}
}

func TestOrderRefusesInvalidQuantities(t *testing.T) {
	catalogue := DefaultCatalogue()
	configured := rules.Default()
	tests := []struct {
		name     string
		quantity int64
		want     error
	}{
		{name: "zero", quantity: 0, want: ErrInvalidQuantity},
		{name: "negative", quantity: -1, want: ErrInvalidQuantity},
		{name: "above the hard cap", quantity: MaximumOrderQuantity + 1, want: ErrInvalidQuantity},
		{name: "overflowing cost", quantity: math.MaxInt64 / 2, want: ErrInvalidQuantity},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := catalogue.Order(LightFighter, test.quantity, shipyardInputs(1, 0, 4), configured); !errors.Is(err, test.want) {
				t.Fatalf("Order(%d) error = %v, want %v", test.quantity, err, test.want)
			}
		})
	}
}

func TestOrderEnforcesPrerequisitesAndLimits(t *testing.T) {
	catalogue := DefaultCatalogue()
	configured := rules.Default()

	if _, err := catalogue.Order(HeavyFighter, 1, shipyardInputs(1, 0, 4), configured); !errors.Is(err, prerequisite.ErrUnmet) {
		t.Fatalf("Order() without the shipyard level error = %v", err)
	}
	if _, err := catalogue.Order("unknown_unit", 1, shipyardInputs(12, 0, 4), configured); !errors.Is(err, ErrUnknownUnit) {
		t.Fatalf("Order() of an unknown unit error = %v", err)
	}

	owned := shipyardInputs(12, 0, 4)
	owned.Inventory = Inventory{SmallShieldDome: 1}
	if _, err := catalogue.Order(SmallShieldDome, 1, owned, configured); !errors.Is(err, ErrQuantityLimit) {
		t.Fatalf("Order() of a second dome error = %v", err)
	}
	pending := shipyardInputs(12, 0, 4)
	pending.Pending = Inventory{LargeShieldDome: 1}
	if _, err := catalogue.Order(LargeShieldDome, 1, pending, configured); !errors.Is(err, ErrQuantityLimit) {
		t.Fatalf("Order() of a dome already in production error = %v", err)
	}

	silo := shipyardInputs(12, 0, 2)
	if _, err := catalogue.Order(AntiBallisticMissile, 20, silo, configured); err != nil {
		t.Fatalf("Order() of twenty interceptors error = %v", err)
	}
	if _, err := catalogue.Order(AntiBallisticMissile, 21, silo, configured); !errors.Is(err, ErrSiloCapacity) {
		t.Fatalf("Order() above silo capacity error = %v", err)
	}
	if _, err := catalogue.Order(InterplanetaryMissile, 10, silo, configured); err != nil {
		t.Fatalf("Order() of ten missiles error = %v", err)
	}
	if _, err := catalogue.Order(InterplanetaryMissile, 11, silo, configured); !errors.Is(err, ErrSiloCapacity) {
		t.Fatalf("Order() above silo capacity in slots error = %v", err)
	}
	occupied := shipyardInputs(12, 0, 2)
	occupied.Inventory = Inventory{AntiBallisticMissile: 18}
	occupied.Pending = Inventory{AntiBallisticMissile: 2}
	if _, err := catalogue.Order(AntiBallisticMissile, 1, occupied, configured); !errors.Is(err, ErrSiloCapacity) {
		t.Fatalf("Order() on a full silo error = %v", err)
	}
}

func TestDefaultCatalogueIsConsistent(t *testing.T) {
	catalogue := DefaultCatalogue()
	ships := catalogue.Definitions(Ship)
	defenses := catalogue.Definitions(Defense)
	if len(ships) != 14 || len(defenses) != 10 {
		t.Fatalf("catalogue holds %d ships and %d defenses, want 14 and 10", len(ships), len(defenses))
	}
	for _, family := range []Family{Ship, Defense} {
		for _, definition := range catalogue.Definitions(family) {
			if definition.Family != family {
				t.Fatalf("%s is filed under %s", definition.ID, family)
			}
			if err := definition.BaseCost.Validate(); err != nil || definition.BaseCost == (economy.Resources{}) {
				t.Fatalf("%s has an invalid cost %v", definition.ID, definition.BaseCost)
			}
			if definition.Hull() <= 0 || definition.Shield < 0 || definition.Weapon < 0 {
				t.Fatalf("%s has invalid combat statistics", definition.ID)
			}
			for target, shots := range definition.RapidFire {
				if _, known := catalogue.Definition(target); !known {
					t.Fatalf("%s has rapid fire against unknown %s", definition.ID, target)
				}
				if target == definition.ID || shots < 2 {
					t.Fatalf("%s has invalid rapid fire against %s", definition.ID, target)
				}
			}
			if definition.Drive != "" {
				if _, err := (research.Levels{}).DriveFactor(definition.Drive); err != nil {
					t.Fatalf("%s uses %s which is not a drive", definition.ID, definition.Drive)
				}
			}
		}
	}
	if hull := mustDefinition(t, catalogue, LightFighter).Hull(); hull != 400 {
		t.Fatalf("light fighter hull = %d, want 400", hull)
	}
}

func mustDefinition(t *testing.T, catalogue Catalogue, id ID) Definition {
	t.Helper()
	definition, known := catalogue.Definition(id)
	if !known {
		t.Fatalf("unknown unit %s", id)
	}
	return definition
}
