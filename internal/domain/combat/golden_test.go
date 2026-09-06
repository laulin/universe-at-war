package combat

import (
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"testing"

	"universeatwar/internal/domain/random"
	"universeatwar/internal/domain/unit"
)

var update = flag.Bool("update", false, "rewrite the recorded battles")

// TestRecordedBattles pins the engine down. A difference here means the rules
// changed, which must be a deliberate decision documented in docs/rules.
func TestRecordedBattles(t *testing.T) {
	battles := []struct {
		name      string
		seed      uint64
		attackers map[unit.ID]int64
		defenders map[unit.ID]int64
	}{
		{name: "duel", seed: 1,
			attackers: map[unit.ID]int64{unit.LightFighter: 1},
			defenders: map[unit.ID]int64{unit.RocketLauncher: 1}},
		{name: "raid_on_a_defended_planet", seed: 2,
			attackers: map[unit.ID]int64{unit.LightFighter: 50, unit.SmallCargo: 10},
			defenders: map[unit.ID]int64{unit.RocketLauncher: 20, unit.LightLaser: 5}},
		{name: "rapid_fire", seed: 3,
			attackers: map[unit.ID]int64{unit.Cruiser: 10},
			defenders: map[unit.ID]int64{unit.LightFighter: 40, unit.RocketLauncher: 10}},
		{name: "defender_holds", seed: 4,
			attackers: map[unit.ID]int64{unit.LightFighter: 5},
			defenders: map[unit.ID]int64{unit.PlasmaTurret: 3, unit.LargeShieldDome: 1}},
		{name: "fleet_against_fleet", seed: 5,
			attackers: map[unit.ID]int64{unit.Battleship: 12, unit.HeavyFighter: 30},
			defenders: map[unit.ID]int64{unit.Cruiser: 18, unit.LightFighter: 25}},
	}
	for _, battle := range battles {
		t.Run(battle.name, func(t *testing.T) {
			result, err := Resolve(plainInput(battle.attackers, battle.defenders), random.NewSeeded(battle.seed))
			if err != nil {
				t.Fatalf("Resolve() error = %v", err)
			}
			recorded, err := json.MarshalIndent(result, "", "  ")
			if err != nil {
				t.Fatal(err)
			}
			path := filepath.Join("testdata", battle.name+".json")
			if *update {
				if err := os.WriteFile(path, append(recorded, '\n'), 0o644); err != nil {
					t.Fatal(err)
				}
				return
			}
			expected, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read the recorded battle: %v (run go test -update to record it)", err)
			}
			if string(expected) != string(recorded)+"\n" {
				t.Fatalf("the battle changed:\n%s", recorded)
			}
		})
	}
}
