package tests

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	appauth "universeatwar/internal/app/authentication"
	appbattlesimulation "universeatwar/internal/app/battlesimulation"
	appreports "universeatwar/internal/app/reports"
	appclock "universeatwar/internal/clock"
	"universeatwar/internal/domain/economy"
	domainfleet "universeatwar/internal/domain/fleet"
	"universeatwar/internal/domain/report"
	"universeatwar/internal/domain/unit"
	"universeatwar/internal/domain/universe"
	storagesqlite "universeatwar/internal/storage/sqlite"
)

func TestBattleSimulationUsesOnlyTheChosenReportAndIsReproducible(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2042, time.September, 10, 11, 12, 13, 0, time.UTC)
	database := economyDatabase(t, ctx, 2)
	game := newWorld(t, database, appclock.NewFake(now))
	attacker := appauth.Principal{AccountID: 1}
	home, err := game.Economy.CreateEmpire(ctx, attacker, "Alice")
	if err != nil {
		t.Fatal(err)
	}
	target, err := game.Economy.CreateEmpire(ctx, appauth.Principal{AccountID: 2}, "Bob")
	if err != nil {
		t.Fatal(err)
	}
	resources := economy.Resources{Metal: 1_000, Crystal: 1_000, Deuterium: 1_000}
	reportID := insertSimulationReport(t, ctx, database, 1, report.Espionage, target.ID, target.Coordinate, now,
		report.EspionagePayload{
			Target: target.Coordinate, TargetPlayerName: "Bob", TargetPlanetName: target.Name,
			Probes: 10, Level: 9, Resources: &resources,
			Fleet: map[string]int64{}, Defenses: map[string]int64{}, Research: map[string]int{},
		})
	request := appbattlesimulation.Request{
		ReportID: reportID, OriginPlanet: home.ID, Target: target.Coordinate,
		Composition: domainfleet.Composition{unit.LightFighter: 1},
		Cargo:       economy.Resources{Metal: 40}, Fuel: 10,
	}

	first, err := game.BattleSimulation.Estimate(ctx, attacker, request)
	if err != nil {
		t.Fatalf("Estimate() error = %v", err)
	}
	second, err := game.BattleSimulation.Estimate(ctx, attacker, request)
	if err != nil {
		t.Fatalf("second Estimate() error = %v", err)
	}
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("same report diverged: %#v then %#v", first, second)
	}
	if !first.Available || first.Samples != 64 || first.WinRate != 100 || first.AttackerLosses.Total.Average != 0 {
		t.Fatalf("empty-target estimate = %#v", first)
	}
	if len(first.AttackerShipLosses) != 0 || len(first.DefenderShipLosses) != 0 || len(first.DefenderDefenseLosses) != 0 ||
		first.ShipDebris.Total.Average != 0 || first.DefenseDebris.Total.Average != 0 {
		t.Fatalf("empty-target losses = %#v", first)
	}
	if !first.LootKnown || first.Loot.Total.Average != 10 || first.Net.Average != first.Loot.Total.Average-10 {
		t.Fatalf("loot estimate = %#v", first)
	}

	request.Composition = domainfleet.Composition{unit.LightFighter: 500_001}
	request.Cargo = economy.Resources{}
	oversized, err := game.BattleSimulation.Estimate(ctx, attacker, request)
	if err != nil {
		t.Fatalf("oversized Estimate() error = %v", err)
	}
	if oversized.Available || oversized.UnavailableReason == "" || oversized.Samples != 0 {
		t.Fatalf("oversized estimate = %#v", oversized)
	}
}

func TestBattleSimulationRefusesMissingOrForeignIntelligence(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2042, time.September, 10, 11, 12, 13, 0, time.UTC)
	database := economyDatabase(t, ctx, 2)
	game := newWorld(t, database, appclock.NewFake(now))
	alice := appauth.Principal{AccountID: 1}
	home, err := game.Economy.CreateEmpire(ctx, alice, "Alice")
	if err != nil {
		t.Fatal(err)
	}
	bob, err := game.Economy.CreateEmpire(ctx, appauth.Principal{AccountID: 2}, "Bob")
	if err != nil {
		t.Fatal(err)
	}
	// The live target is heavily defended, but an incomplete report must not
	// borrow that secret state to make a plausible-looking simulation.
	setUnits(t, ctx, database, bob.ID, "rocket_launcher", 999)
	incomplete := insertSimulationReport(t, ctx, database, 1, report.Espionage, bob.ID, bob.Coordinate, now,
		report.EspionagePayload{Target: bob.Coordinate, TargetPlayerName: "Bob", TargetPlanetName: bob.Name, Probes: 1})
	request := appbattlesimulation.Request{
		ReportID: incomplete, OriginPlanet: home.ID, Target: bob.Coordinate,
		Composition: domainfleet.Composition{unit.LightFighter: 10},
	}
	estimate, err := game.BattleSimulation.Estimate(ctx, alice, request)
	if err != nil {
		t.Fatal(err)
	}
	if estimate.Available || len(estimate.Missing) != 2 || estimate.Samples != 0 {
		t.Fatalf("incomplete intelligence = %#v", estimate)
	}
	request.Target = home.Coordinate
	if _, err := game.BattleSimulation.Estimate(ctx, alice, request); !errors.Is(err, appbattlesimulation.ErrTargetMismatch) {
		t.Fatalf("mismatched target error = %v, want ErrTargetMismatch", err)
	}

	foreign := insertSimulationReport(t, ctx, database, 2, report.Espionage, home.ID, home.Coordinate, now,
		report.EspionagePayload{
			Target: home.Coordinate, TargetPlayerName: "Alice", TargetPlanetName: home.Name,
			Fleet: map[string]int64{}, Defenses: map[string]int64{},
		})
	request.ReportID = foreign
	request.Target = home.Coordinate
	if _, err := game.BattleSimulation.Estimate(ctx, alice, request); !errors.Is(err, appreports.ErrNotFound) {
		t.Fatalf("foreign report error = %v, want ErrNotFound", err)
	}
}

func TestBattleSimulationStartsACombatReportFromKnownSurvivors(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2042, time.September, 10, 11, 12, 13, 0, time.UTC)
	database := economyDatabase(t, ctx, 2)
	game := newWorld(t, database, appclock.NewFake(now))
	alice := appauth.Principal{AccountID: 1}
	home, err := game.Economy.CreateEmpire(ctx, alice, "Alice")
	if err != nil {
		t.Fatal(err)
	}
	bob, err := game.Economy.CreateEmpire(ctx, appauth.Principal{AccountID: 2}, "Bob")
	if err != nil {
		t.Fatal(err)
	}
	reportID := insertSimulationReport(t, ctx, database, 1, report.CombatAttack, bob.ID, bob.Coordinate, now,
		report.CombatPayload{
			Coordinate: bob.Coordinate, Outcome: "attacker",
			Defenders: []report.Participant{{
				PlayerName: "Bob", Weapons: 3, Shielding: 2, Armour: 4,
				Initial:   map[string]int64{"light_fighter": 2, "rocket_launcher": 3},
				Survivors: map[string]int64{"light_fighter": 2, "rocket_launcher": 1},
				Losses:    map[string]int64{"rocket_launcher": 2},
			}},
		})
	estimate, err := game.BattleSimulation.Estimate(ctx, alice, appbattlesimulation.Request{
		ReportID: reportID, OriginPlanet: home.ID, Target: bob.Coordinate,
		Composition: domainfleet.Composition{unit.Battleship: 20}, Fuel: 25,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !estimate.Available || estimate.LootKnown || estimate.Samples == 0 || len(estimate.Warnings) < 2 {
		t.Fatalf("combat-report estimate = %#v", estimate)
	}
	shipLoss, found := simulationUnitLoss(estimate.DefenderShipLosses, unit.LightFighter)
	if !found || shipLoss.Minimum != 2 || shipLoss.Maximum != 2 || shipLoss.Average != 2 {
		t.Fatalf("defender ship losses = %#v", estimate.DefenderShipLosses)
	}
	defenseLoss, found := simulationUnitLoss(estimate.DefenderDefenseLosses, unit.RocketLauncher)
	if !found || defenseLoss.Minimum != 0 || defenseLoss.Average <= 0 || defenseLoss.Average >= 1 || defenseLoss.Maximum != 1 {
		t.Fatalf("defender defense losses = %#v", estimate.DefenderDefenseLosses)
	}
	if estimate.ShipDebris.Total.Average <= 0 || estimate.DefenseDebris.Total.Average != 0 ||
		estimate.Debris.Total.Average != estimate.ShipDebris.Total.Average {
		t.Fatalf("debris breakdown = ships %#v defenses %#v total %#v",
			estimate.ShipDebris, estimate.DefenseDebris, estimate.Debris)
	}
}

func TestBattleSimulationAppliesTheAttackersCurrentCombatTechnologies(t *testing.T) {
	ctx := context.Background()
	now := time.Date(2042, time.September, 10, 11, 12, 13, 0, time.UTC)
	database := economyDatabase(t, ctx, 2)
	game := newWorld(t, database, appclock.NewFake(now))
	alice := appauth.Principal{AccountID: 1}
	home, err := game.Economy.CreateEmpire(ctx, alice, "Alice")
	if err != nil {
		t.Fatal(err)
	}
	bob, err := game.Economy.CreateEmpire(ctx, appauth.Principal{AccountID: 2}, "Bob")
	if err != nil {
		t.Fatal(err)
	}
	resources := economy.Resources{Metal: 10_000, Crystal: 5_000}
	reportID := insertSimulationReport(t, ctx, database, 1, report.Espionage, bob.ID, bob.Coordinate, now,
		report.EspionagePayload{
			Target: bob.Coordinate, TargetPlayerName: "Bob", TargetPlanetName: bob.Name,
			Level: 9, Resources: &resources, Fleet: map[string]int64{},
			Defenses: map[string]int64{"rocket_launcher": 10}, Research: map[string]int{},
		})
	request := appbattlesimulation.Request{
		ReportID: reportID, OriginPlanet: home.ID, Target: bob.Coordinate,
		Composition: domainfleet.Composition{unit.LightFighter: 10},
	}
	withoutTechnology, err := game.BattleSimulation.Estimate(ctx, alice, request)
	if err != nil {
		t.Fatal(err)
	}
	for _, technology := range []string{"weapons_technology", "shielding_technology", "armour_technology"} {
		setResearch(t, ctx, database, 1, technology, 20)
	}
	withTechnology, err := game.BattleSimulation.Estimate(ctx, alice, request)
	if err != nil {
		t.Fatal(err)
	}
	if reflect.DeepEqual(withoutTechnology, withTechnology) {
		t.Fatalf("combat technologies changed nothing: %#v", withTechnology)
	}
	if withTechnology.WinRate < withoutTechnology.WinRate || withTechnology.AttackerLosses.Total.Average > withoutTechnology.AttackerLosses.Total.Average {
		t.Fatalf("technology made the estimate worse: before %#v, after %#v", withoutTechnology, withTechnology)
	}
	if loss, found := simulationUnitLoss(withoutTechnology.AttackerShipLosses, unit.LightFighter); !found || loss.Maximum == 0 {
		t.Fatalf("attacker ship losses = %#v", withoutTechnology.AttackerShipLosses)
	}
	if loss, found := simulationUnitLoss(withTechnology.DefenderDefenseLosses, unit.RocketLauncher); !found || loss.Maximum == 0 {
		t.Fatalf("defender defense losses = %#v", withTechnology.DefenderDefenseLosses)
	}
}

func simulationUnitLoss(losses []appbattlesimulation.UnitLossMetric, id unit.ID) (appbattlesimulation.UnitLossMetric, bool) {
	for _, loss := range losses {
		if loss.ID == id {
			return loss, true
		}
	}
	return appbattlesimulation.UnitLossMetric{}, false
}

func insertSimulationReport(t *testing.T, ctx context.Context, database *storagesqlite.Database, recipientPlayer int64,
	kind report.Kind, subjectID int64, at universe.Coordinate, occurredAt time.Time, payload any) int64 {
	t.Helper()
	version, document, err := report.Marshal(kind, payload)
	if err != nil {
		t.Fatal(err)
	}
	result, err := database.Write().ExecContext(ctx, `
		INSERT INTO reports(recipient_player_id, kind, subject_type, subject_id, galaxy, system, position,
			occurred_at, payload_version, payload, created_at)
		VALUES (?, ?, 'planet', ?, ?, ?, ?, ?, ?, ?, ?)
	`, recipientPlayer, string(kind), subjectID, at.Galaxy, at.System, at.Position,
		occurredAt.Format(time.RFC3339Nano), version, string(document), occurredAt.Format(time.RFC3339Nano))
	if err != nil {
		t.Fatal(err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}
	return id
}
