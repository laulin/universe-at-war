package ai

import (
	"testing"
	"time"

	"universeatwar/internal/domain/economy"
	"universeatwar/internal/domain/unit"
	"universeatwar/internal/domain/universe"
)

func TestScoringWeighsPlunderFreshnessAndDefence(t *testing.T) {
	now := time.Date(2042, time.September, 10, 12, 0, 0, 0, time.UTC)
	intel := Intel{
		Coordinate: universe.Coordinate{Galaxy: 1, System: 1, Position: 4}, ReportID: 7,
		ObservedAt: now, Plunder: economy.Resources{Metal: 10000, Crystal: 5000},
		Defence: 2000, Complete: true, Distance: 1010,
	}
	raider := ScoreTarget(intel, now, time.Hour, Raider.Preferences())
	turtle := ScoreTarget(intel, now, time.Hour, Turtle.Preferences())
	if raider.Freshness != 1 || turtle.Freshness != 1 {
		t.Fatalf("a report of this very minute is not fresh: %v %v", raider.Freshness, turtle.Freshness)
	}
	if raider.Score <= turtle.Score {
		t.Fatalf("the turtle wanted the raid more than the raider: %v vs %v", turtle.Score, raider.Score)
	}
	// The same report seen a day later is worth nothing at all.
	old := ScoreTarget(intel, now.Add(24*time.Hour), time.Hour, Raider.Preferences())
	if old.Freshness != 0 || old.Score >= raider.Score {
		t.Fatalf("an old report kept its value: %+v", old)
	}
}

func TestBestTargetNeedsACompleteAndFreshReport(t *testing.T) {
	now := time.Date(2042, time.September, 10, 12, 0, 0, 0, time.UTC)
	rich := Intel{ReportID: 1, ObservedAt: now, Plunder: economy.Resources{Metal: 60000}, Complete: true}
	partial := Intel{ReportID: 2, ObservedAt: now, Plunder: economy.Resources{Metal: 90000}, Complete: false}
	forgotten := Intel{ReportID: 3, ObservedAt: now.Add(-48 * time.Hour), Plunder: economy.Resources{Metal: 90000}, Complete: true}
	preferences := Raider.Preferences()
	targets := []Target{
		ScoreTarget(rich, now, time.Hour, preferences),
		ScoreTarget(partial, now, time.Hour, preferences),
		ScoreTarget(forgotten, now, time.Hour, preferences),
	}
	best, stale, found := BestTarget(targets, preferences)
	if !found || best.ReportID != 1 {
		t.Fatalf("BestTarget() = %+v %v", best, found)
	}
	if stale.ReportID != 2 {
		t.Fatalf("the richest unknown was not held for a look: %+v", stale)
	}
	// A turtle grades the same reports far more harshly and stays home.
	careful := []Target{
		ScoreTarget(rich, now, time.Hour, Turtle.Preferences()),
		ScoreTarget(partial, now, time.Hour, Turtle.Preferences()),
		ScoreTarget(forgotten, now, time.Hour, Turtle.Preferences()),
	}
	if _, _, found := BestTarget(careful, Turtle.Preferences()); found {
		t.Fatal("a turtle attacked on this")
	}
	if _, _, found := BestTarget(nil, preferences); found {
		t.Fatal("a target came out of nowhere")
	}
}

func TestRaidCarriesEnoughHoldsAndRefusesToBeOutgunned(t *testing.T) {
	catalogue := unit.DefaultCatalogue()
	inventory := map[unit.ID]int64{
		unit.LightFighter: 40, unit.SmallCargo: 20, unit.EspionageProbe: 5, unit.RocketLauncher: 30,
	}
	expected := economy.Resources{Metal: 12000, Crystal: 6000}
	composition, ok := ComposeRaid(inventory, catalogue, expected, 100, Raider.Preferences())
	if !ok {
		t.Fatal("a strong fleet refused an easy target")
	}
	if composition[unit.LightFighter] != 40 {
		t.Fatalf("the fighters stayed home: %v", composition)
	}
	if composition[unit.SmallCargo] == 0 {
		t.Fatalf("nothing came along to carry the haul: %v", composition)
	}
	if composition[unit.RocketLauncher] != 0 || composition[unit.EspionageProbe] != 0 {
		t.Fatalf("the raid took turrets or probes along: %v", composition)
	}
	var carried int64
	for id, quantity := range composition {
		definition, _ := catalogue.Definition(id)
		carried += quantity * definition.Cargo
	}
	if carried < expected.Metal+expected.Crystal {
		t.Fatalf("the raid cannot carry what it hopes to take: %d", carried)
	}
	// A fleet that is not clearly stronger stays on the ground.
	if _, ok := ComposeRaid(inventory, catalogue, expected, 10_000_000, Raider.Preferences()); ok {
		t.Fatal("the raider threw itself at a fortress")
	}
	if _, ok := ComposeRaid(map[unit.ID]int64{unit.SmallCargo: 5, unit.EspionageProbe: 9},
		catalogue, expected, 0, Raider.Preferences()); ok {
		t.Fatal("an unarmed convoy went raiding")
	}
}

func TestFleetsaveTakesEveryShipAndNothingElse(t *testing.T) {
	catalogue := unit.DefaultCatalogue()
	composition := ComposeFleetsave(map[unit.ID]int64{
		unit.LightFighter: 10, unit.SmallCargo: 4, unit.RocketLauncher: 20, unit.SolarSatellite: 8,
	}, catalogue)
	if composition[unit.LightFighter] != 10 || composition[unit.SmallCargo] != 4 {
		t.Fatalf("some ships were left behind: %v", composition)
	}
	if composition[unit.RocketLauncher] != 0 || composition[unit.SolarSatellite] != 0 {
		t.Fatalf("what cannot fly was taken along: %v", composition)
	}
	if ComposeFleetsave(map[unit.ID]int64{unit.RocketLauncher: 5}, catalogue) != nil {
		t.Fatal("a planet without ships still saved a fleet")
	}
}

func TestLastReflectionBeforeTheNightIsRecognised(t *testing.T) {
	window := Window{Start: 8, End: 23}
	interval := time.Hour
	evening := time.Date(2042, time.September, 10, 22, 30, 0, 0, time.UTC)
	if !window.LastBefore(evening, interval) {
		t.Fatal("the last reflection of the evening was not seen as such")
	}
	if window.LastBefore(evening.Add(-6*time.Hour), interval) {
		t.Fatal("the afternoon was mistaken for the evening")
	}
	if (Window{Start: 0, End: 0}).LastBefore(evening, interval) {
		t.Fatal("a player that never sleeps prepared for the night")
	}
}
