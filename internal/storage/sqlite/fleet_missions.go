package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	appeconomy "universeatwar/internal/app/economy"
	"universeatwar/internal/domain/building"
	"universeatwar/internal/domain/combat"
	"universeatwar/internal/domain/debris"
	"universeatwar/internal/domain/economy"
	"universeatwar/internal/domain/espionage"
	domainfleet "universeatwar/internal/domain/fleet"
	"universeatwar/internal/domain/random"
	"universeatwar/internal/domain/report"
	"universeatwar/internal/domain/research"
	"universeatwar/internal/domain/rules"
	"universeatwar/internal/domain/unit"
	"universeatwar/internal/domain/universe"
)

// resolveEspionage reveals what the probes could see, tells the target that
// somebody looked, and sends the probes home unless they were caught.
func (r *FleetRepository) resolveEspionage(ctx context.Context, tx *sql.Tx, row fleetRow, targetPlanetID int64, dueAt, now time.Time) error {
	target, _, production, err := loadPlanetByID(ctx, tx, targetPlanetID, dueAt, r.catalogues.Buildings)
	if err != nil {
		return err
	}
	if err := persistProduction(ctx, tx, targetPlanetID, production); err != nil {
		return err
	}
	composition, err := loadComposition(ctx, tx, row.id)
	if err != nil {
		return err
	}
	probes := composition[unit.EspionageProbe]
	attackerResearch, err := researchOfPlayer(ctx, tx, row.ownerPlayerID)
	if err != nil {
		return err
	}
	defenderPlayerID, err := playerOfPlanet(ctx, tx, targetPlanetID)
	if err != nil {
		return err
	}
	ships, defenses := splitUnits(target.Units, r.catalogues.Units)
	revealed, err := espionage.Reveal(target.Rules.Espionage,
		attackerResearch.EspionageLevel(), target.Researches.EspionageLevel(), probes, espionage.Truth{
			Resources: production.Stock,
			Fleet:     ships,
			Defenses:  defenses,
			Buildings: target.Levels,
			Research:  target.Researches,
		})
	if err != nil {
		return err
	}
	source := random.NewSeeded(uint64(row.seed))
	chance := espionage.DetectionChance(target.Rules.Espionage,
		attackerResearch.EspionageLevel(), target.Researches.EspionageLevel(), probes, totalShips(ships))
	detected := espionage.Detected(chance, source)

	attackerName, err := playerName(ctx, tx, row.ownerPlayerID)
	if err != nil {
		return err
	}
	defenderName, err := playerName(ctx, tx, defenderPlayerID)
	if err != nil {
		return err
	}
	if _, err := insertReport(ctx, tx, row.ownerPlayerID, report.Espionage, "planet", targetPlanetID, row.target, dueAt,
		espionageReport(row, target.Name, defenderName, probes, detected, revealed)); err != nil {
		return err
	}
	if _, err := insertReport(ctx, tx, defenderPlayerID, report.EspionageDetected, "planet", targetPlanetID, row.target, dueAt,
		report.DetectedPayload{
			AttackerPlayerName: attackerName, Origin: row.origin, Probes: probes,
			ProbesDestroyed: detected, TargetPlanetName: target.Name,
		}); err != nil {
		return err
	}
	if !detected {
		return r.sendHome(ctx, tx, row, "spied", now)
	}
	if err := destroyProbes(ctx, tx, row, composition, r.catalogues.Units, target.Rules, now); err != nil {
		return err
	}
	return logFleetEvent(ctx, tx, "espionage_resolved", row.id, targetPlanetID, now,
		fmt.Sprintf("json_object('level', %d, 'probes', %d, 'detected', %t)", revealed.Level, probes, detected))
}

// espionageReport keeps only the sections the mission actually revealed.
func espionageReport(row fleetRow, planetName, ownerName string, probes int64, lost bool, revealed espionage.Report) report.EspionagePayload {
	payload := report.EspionagePayload{
		Target: row.target, TargetPlayerName: ownerName, TargetPlanetName: planetName,
		Probes: probes, Level: revealed.Level, ProbesLost: lost,
	}
	if revealed.Resources != nil {
		resources := *revealed.Resources
		payload.Resources = &resources
	}
	if revealed.Fleet != nil {
		payload.Fleet = inventoryDocument(revealed.Fleet)
	}
	if revealed.Defenses != nil {
		payload.Defenses = inventoryDocument(revealed.Defenses)
	}
	if revealed.Buildings != nil {
		payload.Buildings = buildingDocument(revealed.Buildings)
	}
	if revealed.Research != nil {
		payload.Research = researchDocument(revealed.Research)
	}
	return payload
}

// destroyProbes loses the whole mission and leaves its wreckage behind.
func destroyProbes(ctx context.Context, tx *sql.Tx, row fleetRow, composition domainfleet.Composition,
	catalogue unit.Catalogue, configured rules.Ruleset, now time.Time) error {
	wreckage := debris.Field{}
	for id, quantity := range composition {
		definition, known := catalogue.Definition(id)
		if !known {
			continue
		}
		wreckage.Metal += int64(configured.Combat.ShipsToDebris * float64(quantity*definition.BaseCost.Metal))
		wreckage.Crystal += int64(configured.Combat.ShipsToDebris * float64(quantity*definition.BaseCost.Crystal))
	}
	if err := addDebris(ctx, tx, row.target, wreckage, now); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, "DELETE FROM fleet_ships WHERE fleet_id = ?", row.id); err != nil {
		return fmt.Errorf("fleet repository: destroy probes: %w", err)
	}
	if err := transitionFleet(ctx, tx, row, domainfleet.Destroyed, "probes_lost", now); err != nil {
		return err
	}
	return logFleetEvent(ctx, tx, "debris_created", row.id, row.originPlanetID, now,
		fmt.Sprintf("json_object('metal', %d, 'crystal', %d)", wreckage.Metal, wreckage.Crystal))
}

// resolveCombat fights the battle, applies its losses, its debris and its loot,
// then sends the survivors home.
func (r *FleetRepository) resolveCombat(ctx context.Context, tx *sql.Tx, row fleetRow, targetPlanetID int64, dueAt, now time.Time) error {
	target, _, production, err := loadPlanetByID(ctx, tx, targetPlanetID, dueAt, r.catalogues.Buildings)
	if err != nil {
		return err
	}
	composition, err := loadComposition(ctx, tx, row.id)
	if err != nil {
		return err
	}
	attackerResearch, err := researchOfPlayer(ctx, tx, row.ownerPlayerID)
	if err != nil {
		return err
	}
	defenderPlayerID, err := playerOfPlanet(ctx, tx, targetPlanetID)
	if err != nil {
		return err
	}
	source := random.NewSeeded(uint64(row.seed))
	result, err := combat.Resolve(combat.Input{
		Attackers: []combat.Party{{PlayerID: row.ownerPlayerID, Units: composition, Technologies: factorsOf(attackerResearch)}},
		Defenders: []combat.Party{{PlayerID: defenderPlayerID, Units: fightingUnits(target.Units, r.catalogues.Units), Technologies: factorsOf(target.Researches)}},
		Rules:     target.Rules.Combat,
		Multipliers: combat.CostMultipliers{
			Ship: target.Rules.Progression.ShipCostMultiplier, Defense: target.Rules.Progression.DefenseCostMultiplier,
		},
		Catalogue: r.catalogues.Units,
	}, source)
	if err != nil {
		return err
	}

	survivors := result.Attackers[0].Survivors
	if err := applyFleetLosses(ctx, tx, row.id, composition, survivors); err != nil {
		return err
	}
	for id, lost := range result.Defenders[0].Losses {
		if err := adjustInventory(ctx, tx, targetPlanetID, id, -lost); err != nil {
			return err
		}
	}
	if err := addDebris(ctx, tx, row.target,
		debris.Field{Metal: result.Debris.Metal, Crystal: result.Debris.Crystal}, now); err != nil {
		return err
	}
	// The moon draw continues the very sequence the battle used, so replaying
	// the fight from its seed replays the moon as well.
	moonCreated, err := r.attemptMoon(ctx, tx, row, targetPlanetID, target, result.MoonChance, source, now)
	if err != nil {
		return err
	}

	loot := economy.Resources{}
	if result.Outcome == combat.AttackerWins && totalShips(survivors) > 0 {
		cargo, cargoErr := loadCargo(ctx, tx, row.id)
		if cargoErr != nil {
			return cargoErr
		}
		capacity, capacityErr := remainingCapacity(survivors, cargo, r.catalogues.Units)
		if capacityErr != nil {
			return capacityErr
		}
		ratio := min(target.Rules.Economy.PillageRatio, target.Rules.Combat.MaximumPillage)
		loot = combat.Pillage(production.Stock, capacity, ratio)
		if production.Stock, err = production.Stock.Debit(loot); err != nil {
			return err
		}
		if err := storeCargo(ctx, tx, row.id, economy.Resources{
			Metal: cargo.Metal + loot.Metal, Crystal: cargo.Crystal + loot.Crystal, Deuterium: cargo.Deuterium + loot.Deuterium,
		}); err != nil {
			return err
		}
	}
	if err := persistProduction(ctx, tx, targetPlanetID, production); err != nil {
		return err
	}

	attackerName, err := playerName(ctx, tx, row.ownerPlayerID)
	if err != nil {
		return err
	}
	defenderName, err := playerName(ctx, tx, defenderPlayerID)
	if err != nil {
		return err
	}
	attackerPayload, defenderPayload := combatReports(row, result, attackerName, defenderName,
		attackerResearch, target.Researches, loot)
	if _, err := insertReport(ctx, tx, row.ownerPlayerID, report.CombatAttack, "planet", targetPlanetID, row.target, dueAt, attackerPayload); err != nil {
		return err
	}
	if _, err := insertReport(ctx, tx, defenderPlayerID, report.CombatDefense, "planet", targetPlanetID, row.target, dueAt, defenderPayload); err != nil {
		return err
	}
	if err := logFleetEvent(ctx, tx, "combat_resolved", row.id, targetPlanetID, now,
		fmt.Sprintf("json_object('outcome', '%s', 'seed', %d, 'rounds', %d, 'debris_metal', %d, 'debris_crystal', %d, 'loot_metal', %d, 'loot_crystal', %d, 'loot_deuterium', %d, 'moon_chance', %f, 'moon_created', %t)",
			result.Outcome, row.seed, len(result.Rounds), result.Debris.Metal, result.Debris.Crystal,
			loot.Metal, loot.Crystal, loot.Deuterium, result.MoonChance, moonCreated)); err != nil {
		return err
	}
	if totalShips(survivors) == 0 {
		return transitionFleet(ctx, tx, row, domainfleet.Destroyed, "lost_in_battle", now)
	}
	return r.sendHome(ctx, tx, row, "battle_resolved", now)
}

// resolveRecycling lifts what the fleet can carry out of a debris field.
func (r *FleetRepository) resolveRecycling(ctx context.Context, tx *sql.Tx, row fleetRow, dueAt, now time.Time) error {
	field, err := loadDebris(ctx, tx, row.target)
	if err != nil {
		return err
	}
	composition, err := loadComposition(ctx, tx, row.id)
	if err != nil {
		return err
	}
	cargo, err := loadCargo(ctx, tx, row.id)
	if err != nil {
		return err
	}
	hold, err := domainfleet.Capacity(composition, r.catalogues.Units)
	if err != nil {
		return err
	}
	configured, _, err := activeRuleset(ctx, tx)
	if err != nil {
		return err
	}
	capacity := debris.Capacity(composition[unit.Recycler], configured.Combat.RecyclerCapacity, hold,
		cargo.Metal+cargo.Crystal+cargo.Deuterium)
	harvest := debris.Harvest(field, capacity)
	if err := takeDebris(ctx, tx, row.target, harvest, now); err != nil {
		return err
	}
	if err := storeCargo(ctx, tx, row.id, economy.Resources{
		Metal: cargo.Metal + harvest.Metal, Crystal: cargo.Crystal + harvest.Crystal, Deuterium: cargo.Deuterium,
	}); err != nil {
		return err
	}
	remaining := debris.Field{Metal: field.Metal - harvest.Metal, Crystal: field.Crystal - harvest.Crystal}
	if _, err := insertReport(ctx, tx, row.ownerPlayerID, report.Recycling, "debris", row.id, row.target, dueAt,
		report.RecyclingPayload{
			Position: row.target, Recyclers: composition[unit.Recycler], Capacity: capacity,
			Collected: harvest.Resources(), Remaining: remaining.Resources(),
		}); err != nil {
		return err
	}
	if err := logFleetEvent(ctx, tx, "debris_recycled", row.id, row.originPlanetID, now,
		fmt.Sprintf("json_object('metal', %d, 'crystal', %d)", harvest.Metal, harvest.Crystal)); err != nil {
		return err
	}
	return r.sendHome(ctx, tx, row, "recycled", now)
}

// sendHome turns a fleet around and schedules its return.
func (r *FleetRepository) sendHome(ctx context.Context, tx *sql.Tx, row fleetRow, reason string, now time.Time) error {
	returnsAt := row.arrivesAt.Add(row.arrivesAt.Sub(row.departedAt))
	if row.returnsAt != nil {
		returnsAt = *row.returnsAt
	}
	if err := transitionFleet(ctx, tx, row, domainfleet.Returning, reason, now); err != nil {
		return err
	}
	return scheduleReturn(ctx, tx, row.id, row.rulesetVersion, returnsAt, now)
}

// applyFleetLosses rewrites the composition of a fleet after a battle.
func applyFleetLosses(ctx context.Context, tx *sql.Tx, fleetID int64, before domainfleet.Composition, survivors map[unit.ID]int64) error {
	for id := range before {
		remaining := survivors[id]
		if remaining <= 0 {
			if _, err := tx.ExecContext(ctx, "DELETE FROM fleet_ships WHERE fleet_id = ? AND unit_id = ?", fleetID, string(id)); err != nil {
				return fmt.Errorf("fleet repository: remove losses: %w", err)
			}
			continue
		}
		if _, err := tx.ExecContext(ctx, "UPDATE fleet_ships SET quantity = ? WHERE fleet_id = ? AND unit_id = ?",
			remaining, fleetID, string(id)); err != nil {
			return fmt.Errorf("fleet repository: apply losses: %w", err)
		}
	}
	return nil
}

// combatReports builds the two accounts of the battle. Everything a participant
// could observe is shared; the rebuilt defenses belong to the defender alone.
func combatReports(row fleetRow, result combat.Result, attackerName, defenderName string,
	attackerResearch, defenderResearch research.Levels, loot economy.Resources) (report.CombatPayload, report.CombatPayload) {
	shared := report.CombatPayload{
		Coordinate: row.target,
		Outcome:    string(result.Outcome),
		Attackers:  []report.Participant{participantOf(result.Attackers[0], attackerName, attackerResearch)},
		Defenders:  []report.Participant{participantOf(result.Defenders[0], defenderName, defenderResearch)},
		Rounds:     roundsOf(result.Rounds),
		Loot:       loot,
		Debris:     result.Debris,
		MoonChance: result.MoonChance,
	}
	attacker := shared
	defender := shared
	defender.Rebuilt = inventoryDocument(result.Defenders[0].Rebuilt)
	return attacker, defender
}

func participantOf(party combat.PartyResult, name string, levels research.Levels) report.Participant {
	return report.Participant{
		PlayerName: name,
		Weapons:    levels[research.WeaponsTechnology],
		Shielding:  levels[research.ShieldingTechnology],
		Armour:     levels[research.ArmourTechnology],
		Initial:    inventoryDocument(party.Initial),
		Survivors:  inventoryDocument(party.Survivors),
		Losses:     inventoryDocument(party.Losses),
	}
}

func roundsOf(rounds []combat.Round) []report.RoundSummary {
	summaries := make([]report.RoundSummary, 0, len(rounds))
	for _, round := range rounds {
		summaries = append(summaries, report.RoundSummary{
			Number:           round.Number,
			AttackerShots:    round.Attackers.Shots,
			DefenderShots:    round.Defenders.Shots,
			AttackerDamage:   round.Attackers.Damage,
			DefenderDamage:   round.Defenders.Damage,
			AttackerAbsorbed: round.Attackers.Absorbed,
			DefenderAbsorbed: round.Defenders.Absorbed,
		})
	}
	return summaries
}

// fightingUnits keeps the units a planet can actually defend itself with.
func fightingUnits(inventory unit.Inventory, catalogue unit.Catalogue) map[unit.ID]int64 {
	fighting := make(map[unit.ID]int64, len(inventory))
	for id, quantity := range inventory {
		definition, known := catalogue.Definition(id)
		if !known || quantity <= 0 || definition.SiloSlots > 0 {
			continue
		}
		fighting[id] = quantity
	}
	return fighting
}

// splitUnits separates what a spy sees as a fleet from what it sees as defenses.
func splitUnits(inventory unit.Inventory, catalogue unit.Catalogue) (unit.Inventory, unit.Inventory) {
	ships := unit.Inventory{}
	defenses := unit.Inventory{}
	for id, quantity := range inventory {
		if quantity <= 0 {
			continue
		}
		definition, known := catalogue.Definition(id)
		if !known {
			continue
		}
		if definition.Family == unit.Defense {
			defenses[id] = quantity
			continue
		}
		ships[id] = quantity
	}
	return ships, defenses
}

func totalShips[V ~int64](units map[unit.ID]V) int64 {
	var total int64
	for _, quantity := range units {
		total += int64(quantity)
	}
	return total
}

// remainingCapacity is the hold left once the current cargo is accounted for.
func remainingCapacity(survivors map[unit.ID]int64, cargo economy.Resources, catalogue unit.Catalogue) (int64, error) {
	composition := domainfleet.Composition{}
	for id, quantity := range survivors {
		if quantity > 0 {
			composition[id] = quantity
		}
	}
	if len(composition) == 0 {
		return 0, nil
	}
	hold, err := domainfleet.Capacity(composition, catalogue)
	if err != nil {
		return 0, err
	}
	remaining := hold - cargo.Metal - cargo.Crystal - cargo.Deuterium
	if remaining < 0 {
		return 0, nil
	}
	return remaining, nil
}

func factorsOf(levels research.Levels) combat.Factors {
	return combat.Factors{
		Weapons: levels.WeaponsFactor(),
		Shield:  levels.ShieldingFactor(),
		Armour:  levels.ArmourFactor(),
	}
}

func researchOfPlayer(ctx context.Context, tx *sql.Tx, playerID int64) (research.Levels, error) {
	rows, err := tx.QueryContext(ctx, "SELECT research_id, level FROM player_research WHERE player_id = ?", playerID)
	if err != nil {
		return nil, fmt.Errorf("fleet repository: read research: %w", err)
	}
	defer rows.Close()
	levels := research.Levels{}
	for rows.Next() {
		var id string
		var level int
		if err := rows.Scan(&id, &level); err != nil {
			return nil, fmt.Errorf("fleet repository: scan research: %w", err)
		}
		levels[research.ID(id)] = level
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("fleet repository: iterate research: %w", err)
	}
	return levels, nil
}

func playerName(ctx context.Context, tx *sql.Tx, playerID int64) (string, error) {
	var name string
	if err := tx.QueryRowContext(ctx, "SELECT display_name FROM players WHERE id = ?", playerID).Scan(&name); err != nil {
		return "", fmt.Errorf("fleet repository: read player name: %w", err)
	}
	return name, nil
}

func inventoryDocument[V ~int64](units map[unit.ID]V) map[string]int64 {
	if len(units) == 0 {
		return nil
	}
	document := make(map[string]int64, len(units))
	for id, quantity := range units {
		document[string(id)] = int64(quantity)
	}
	return document
}

func buildingDocument(levels building.Levels) map[string]int {
	if len(levels) == 0 {
		return nil
	}
	document := make(map[string]int, len(levels))
	for id, level := range levels {
		document[string(id)] = level
	}
	return document
}

func researchDocument(levels research.Levels) map[string]int {
	if len(levels) == 0 {
		return nil
	}
	document := make(map[string]int, len(levels))
	for id, level := range levels {
		document[string(id)] = level
	}
	return document
}

// resolveColonization founds a planet if the position is still free when the
// fleet lands. The whole creation is one transaction, and the uniqueness of a
// coordinate arbitrates two colonisations racing for it.
func (r *FleetRepository) resolveColonization(ctx context.Context, tx *sql.Tx, row fleetRow, dueAt, now time.Time) error {
	configured, rulesetVersion, err := activeRuleset(ctx, tx)
	if err != nil {
		return err
	}
	occupied, _, err := planetAt(ctx, tx, row.target)
	if err != nil {
		return err
	}
	if occupied != 0 {
		return r.abortMission(ctx, tx, row, "position_taken", now)
	}
	levels, err := researchOfPlayer(ctx, tx, row.ownerPlayerID)
	if err != nil {
		return err
	}
	if err := colonySlotAvailable(ctx, tx, row.ownerPlayerID, levels, configured); err != nil {
		if errors.Is(err, domainfleet.ErrNoColonySlot) {
			return r.abortMission(ctx, tx, row, "no_colony_slot", now)
		}
		return err
	}
	composition, err := loadComposition(ctx, tx, row.id)
	if err != nil {
		return err
	}
	if composition[unit.ColonyShip] <= 0 {
		return r.abortMission(ctx, tx, row, "no_colony_ship", now)
	}

	traits, err := universe.Generate(row.target,
		configured.Topology.MinPlanetFields, configured.Topology.MaxPlanetFields,
		configured.Topology.PositionsPerSystem, random.NewSeeded(uint64(row.seed)))
	if err != nil {
		return err
	}
	planetID, err := createBody(ctx, tx, bodyRecord{
		ownerPlayerID: row.ownerPlayerID,
		kind:          building.OnPlanet,
		name:          "Colonie",
		at:            row.target,
		traits:        traits,
		createdAt:     dueAt,
	})
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			// Another colonisation reached the position first.
			return r.abortMission(ctx, tx, row, "position_taken", now)
		}
		return err
	}

	// The colony ship becomes the settlement; the rest of the fleet goes home.
	if err := consumeColonyShip(ctx, tx, row.id, composition); err != nil {
		return err
	}
	cargo, err := loadCargo(ctx, tx, row.id)
	if err != nil {
		return err
	}
	planet, _, production, err := loadPlanetByID(ctx, tx, planetID, dueAt, r.catalogues.Buildings)
	if err != nil {
		return err
	}
	delivered := deliverable(cargo, production.Stock, planet.Capacity)
	production.Stock = economy.Resources{
		Metal:     production.Stock.Metal + delivered.Metal,
		Crystal:   production.Stock.Crystal + delivered.Crystal,
		Deuterium: production.Stock.Deuterium + delivered.Deuterium,
	}
	if err := persistProduction(ctx, tx, planetID, production); err != nil {
		return err
	}
	if err := storeCargo(ctx, tx, row.id, economy.Resources{
		Metal:     cargo.Metal - delivered.Metal,
		Crystal:   cargo.Crystal - delivered.Crystal,
		Deuterium: cargo.Deuterium - delivered.Deuterium,
	}); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO game_event_log(event_type, actor_type, actor_id, entity_type, entity_id, occurred_at, payload)
		VALUES ('colony_founded', 'player', ?, 'planet', ?, ?, json_object('coordinate', ?, 'fields', ?, 'seed', ?, 'ruleset_version', ?))
	`, row.ownerPlayerID, planetID, timestamp(now), row.target.String(), traits.TotalFields, row.seed, rulesetVersion); err != nil {
		return fmt.Errorf("fleet repository: log colonisation: %w", err)
	}

	remaining, err := loadComposition(ctx, tx, row.id)
	if err != nil {
		return err
	}
	if remaining.Count() == 0 {
		return transitionFleet(ctx, tx, row, domainfleet.Completed, "colonised", now)
	}
	return r.sendHome(ctx, tx, row, "colonised", now)
}

// bodyRecord is a celestial body about to be created.
type bodyRecord struct {
	ownerPlayerID int64
	kind          building.Placement
	parentID      int64
	name          string
	at            universe.Coordinate
	traits        universe.Characteristics
	createdAt     time.Time
}

// createBody inserts a planet or a moon with its own empty stock.
func createBody(ctx context.Context, tx *sql.Tx, record bodyRecord) (int64, error) {
	var parent any
	if record.parentID > 0 {
		parent = record.parentID
	}
	result, err := tx.ExecContext(ctx, `
		INSERT INTO planets(owner_player_id, kind, parent_planet_id, name, galaxy, system, position,
			total_fields, minimum_temperature, maximum_temperature, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, record.ownerPlayerID, string(record.kind), parent, record.name,
		record.at.Galaxy, record.at.System, record.at.Position,
		record.traits.TotalFields, record.traits.MinimumTemperature, record.traits.MaximumTemperature,
		timestamp(record.createdAt))
	if err != nil {
		return 0, fmt.Errorf("fleet repository: create body: %w", err)
	}
	bodyID, err := result.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("fleet repository: body id: %w", err)
	}
	// A new body starts empty: a colony brings its own stock, a moon holds none.
	if _, err := tx.ExecContext(ctx,
		"INSERT INTO planet_resources(planet_id, metal, crystal, deuterium, produced_at) VALUES (?, 0, 0, 0, ?)",
		bodyID, timestamp(record.createdAt)); err != nil {
		return 0, fmt.Errorf("fleet repository: create body resources: %w", err)
	}
	return bodyID, nil
}

// consumeColonyShip removes exactly one colony ship from the fleet.
func consumeColonyShip(ctx context.Context, tx *sql.Tx, fleetID int64, composition domainfleet.Composition) error {
	remaining := composition[unit.ColonyShip] - 1
	if remaining <= 0 {
		if _, err := tx.ExecContext(ctx, "DELETE FROM fleet_ships WHERE fleet_id = ? AND unit_id = ?",
			fleetID, string(unit.ColonyShip)); err != nil {
			return fmt.Errorf("fleet repository: consume colony ship: %w", err)
		}
		return nil
	}
	if _, err := tx.ExecContext(ctx, "UPDATE fleet_ships SET quantity = ? WHERE fleet_id = ? AND unit_id = ?",
		remaining, fleetID, string(unit.ColonyShip)); err != nil {
		return fmt.Errorf("fleet repository: consume colony ship: %w", err)
	}
	return nil
}

// attemptMoon draws whether the wreckage gathers into a moon. A position that
// already carries one never gets a second.
func (r *FleetRepository) attemptMoon(ctx context.Context, tx *sql.Tx, row fleetRow, planetID int64,
	target appeconomy.Planet, chance float64, source random.Source, now time.Time) (bool, error) {
	if chance <= 0 {
		return false, nil
	}
	existing, err := moonAt(ctx, tx, row.target)
	if err != nil {
		return false, err
	}
	if existing != 0 {
		return false, nil
	}
	if !random.Chance(source, chance) {
		return false, nil
	}
	owner, err := playerOfPlanet(ctx, tx, planetID)
	if err != nil {
		return false, err
	}
	moonID, err := createBody(ctx, tx, bodyRecord{
		ownerPlayerID: owner,
		kind:          building.OnMoon,
		parentID:      planetID,
		name:          "Lune",
		at:            row.target,
		traits: universe.Characteristics{
			TotalFields:        target.Rules.Expansion.BaseMoonFields,
			MinimumTemperature: target.MinimumTemperature,
			MaximumTemperature: target.MaximumTemperature,
		},
		createdAt: now,
	})
	if err != nil {
		return false, err
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO game_event_log(event_type, entity_type, entity_id, occurred_at, payload)
		VALUES ('moon_created', 'planet', ?, ?, json_object('coordinate', ?, 'chance', ?, 'seed', ?, 'parent_planet_id', ?))
	`, moonID, timestamp(now), row.target.String(), chance, row.seed, planetID); err != nil {
		return false, fmt.Errorf("fleet repository: log moon: %w", err)
	}
	return true, nil
}

// moonAt returns the moon of a position, if it has one.
func moonAt(ctx context.Context, tx *sql.Tx, at universe.Coordinate) (int64, error) {
	var moonID int64
	err := tx.QueryRowContext(ctx,
		"SELECT id FROM planets WHERE galaxy = ? AND system = ? AND position = ? AND kind = 'moon'",
		at.Galaxy, at.System, at.Position).Scan(&moonID)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("fleet repository: read moon: %w", err)
	}
	return moonID, nil
}
