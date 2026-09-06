package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	domainacs "universeatwar/internal/domain/acs"
	"universeatwar/internal/domain/combat"
	"universeatwar/internal/domain/debris"
	"universeatwar/internal/domain/economy"
	domainfleet "universeatwar/internal/domain/fleet"
	"universeatwar/internal/domain/random"
	"universeatwar/internal/domain/report"
	"universeatwar/internal/domain/research"
	"universeatwar/internal/domain/universe"
)

// battleFleet is one attacking fleet and the party it fights as.
type battleFleet struct {
	row   fleetRow
	party combat.Party
}

// battleOutcome is what the callers of a battle need to journal it.
type battleOutcome struct {
	result combat.Result
	loot   economy.Resources
	moon   bool
}

// resolveBattle fights one battle for every attacking fleet at once, against the
// target and the allied fleets stationed on it. A lone attacker is the same
// battle with a single attacking party, so both paths share these rules.
func (r *FleetRepository) resolveBattle(ctx context.Context, tx *sql.Tx, attackers []battleFleet,
	targetPlanetID, seed int64, dueAt, now time.Time) (battleOutcome, error) {
	if len(attackers) == 0 {
		return battleOutcome{}, fmt.Errorf("fleet repository: a battle needs at least one attacking fleet")
	}
	lead := attackers[0].row
	target, _, production, err := loadPlanetByID(ctx, tx, targetPlanetID, dueAt, r.catalogues.Buildings)
	if err != nil {
		return battleOutcome{}, err
	}
	defenderPlayerID, err := playerOfPlanet(ctx, tx, targetPlanetID)
	if err != nil {
		return battleOutcome{}, err
	}
	stationed, err := r.stationedDefenders(ctx, tx, lead.target)
	if err != nil {
		return battleOutcome{}, err
	}

	attackingParties := make([]combat.Party, 0, len(attackers))
	for _, attacker := range attackers {
		attackingParties = append(attackingParties, attacker.party)
	}
	defendingParties := make([]combat.Party, 0, len(stationed)+1)
	defendingParties = append(defendingParties, combat.Party{
		PlayerID: defenderPlayerID, Units: fightingUnits(target.Units, r.catalogues.Units),
		Technologies: factorsOf(target.Researches),
	})
	for _, defender := range stationed {
		defendingParties = append(defendingParties, defender.party)
	}

	source := random.NewSeeded(uint64(seed))
	result, err := combat.Resolve(combat.Input{
		Attackers: attackingParties,
		Defenders: defendingParties,
		Rules:     target.Rules.Combat,
		Multipliers: combat.CostMultipliers{
			Ship: target.Rules.Progression.ShipCostMultiplier, Defense: target.Rules.Progression.DefenseCostMultiplier,
		},
		Catalogue: r.catalogues.Units,
	}, source)
	if err != nil {
		return battleOutcome{}, err
	}

	for index, attacker := range attackers {
		if err := applyFleetLosses(ctx, tx, attacker.row.id,
			domainfleet.Composition(attacker.party.Units), result.Attackers[index].Survivors); err != nil {
			return battleOutcome{}, err
		}
	}
	for id, lost := range result.Defenders[0].Losses {
		if err := adjustInventory(ctx, tx, targetPlanetID, id, -lost); err != nil {
			return battleOutcome{}, err
		}
	}
	if err := r.applyDefenderLosses(ctx, tx, stationed, result.Defenders[1:], now); err != nil {
		return battleOutcome{}, err
	}
	if err := addDebris(ctx, tx, lead.target,
		debris.Field{Metal: result.Debris.Metal, Crystal: result.Debris.Crystal}, now); err != nil {
		return battleOutcome{}, err
	}
	// The moon draw continues the very sequence the battle used, so replaying
	// the fight from its seed replays the moon as well.
	moonCreated, err := r.attemptMoon(ctx, tx, lead, targetPlanetID, target, result.MoonChance, source, now)
	if err != nil {
		return battleOutcome{}, err
	}

	loot := economy.Resources{}
	if result.Outcome == combat.AttackerWins {
		loot, err = r.shareLoot(ctx, tx, attackers, result, target.Rules.Economy.PillageRatio,
			target.Rules.Combat.MaximumPillage, &production.Stock)
		if err != nil {
			return battleOutcome{}, err
		}
	}
	if err := persistProduction(ctx, tx, targetPlanetID, production); err != nil {
		return battleOutcome{}, err
	}
	if err := r.reportBattle(ctx, tx, lead.target, targetPlanetID, attackers, defenderPlayerID,
		target.Researches, stationed, result, loot, dueAt); err != nil {
		return battleOutcome{}, err
	}
	for index, attacker := range attackers {
		if totalShips(result.Attackers[index].Survivors) == 0 {
			if err := transitionFleet(ctx, tx, attacker.row, domainfleet.Destroyed, "lost_in_battle", now); err != nil {
				return battleOutcome{}, err
			}
			continue
		}
		if err := r.sendHome(ctx, tx, attacker.row, "battle_resolved", now); err != nil {
			return battleOutcome{}, err
		}
	}
	return battleOutcome{result: result, loot: loot, moon: moonCreated}, nil
}

// shareLoot pillages once for the whole attacking side, then splits the haul
// between the surviving fleets in proportion to the room each has left.
func (r *FleetRepository) shareLoot(ctx context.Context, tx *sql.Tx, attackers []battleFleet,
	result combat.Result, ratio, maximum float64, stock *economy.Resources) (economy.Resources, error) {
	capacities := make([]int64, len(attackers))
	cargos := make([]economy.Resources, len(attackers))
	var total int64
	for index, attacker := range attackers {
		survivors := result.Attackers[index].Survivors
		if totalShips(survivors) == 0 {
			continue
		}
		cargo, err := loadCargo(ctx, tx, attacker.row.id)
		if err != nil {
			return economy.Resources{}, err
		}
		capacity, err := remainingCapacity(survivors, cargo, r.catalogues.Units)
		if err != nil {
			return economy.Resources{}, err
		}
		cargos[index] = cargo
		capacities[index] = capacity
		total += capacity
	}
	if total <= 0 {
		return economy.Resources{}, nil
	}
	loot := combat.Pillage(*stock, total, min(ratio, maximum))
	if loot.Metal+loot.Crystal+loot.Deuterium == 0 {
		return economy.Resources{}, nil
	}
	debited, err := stock.Debit(loot)
	if err != nil {
		return economy.Resources{}, err
	}
	*stock = debited
	shares := domainacs.DistributeLoot(loot, capacities)
	for index, attacker := range attackers {
		if capacities[index] <= 0 {
			continue
		}
		share := shares[index]
		if err := storeCargo(ctx, tx, attacker.row.id, economy.Resources{
			Metal:     cargos[index].Metal + share.Metal,
			Crystal:   cargos[index].Crystal + share.Crystal,
			Deuterium: cargos[index].Deuterium + share.Deuterium,
		}); err != nil {
			return economy.Resources{}, err
		}
	}
	return loot, nil
}

// reportBattle writes one account of the battle for every player who took part.
// Everything a participant could observe is shared; only the defending side is
// told which defenses were rebuilt.
func (r *FleetRepository) reportBattle(ctx context.Context, tx *sql.Tx, at universe.Coordinate,
	targetPlanetID int64, attackers []battleFleet, defenderPlayerID int64, defenderLevels research.Levels,
	stationed []defendingFleet, result combat.Result, loot economy.Resources, dueAt time.Time) error {
	names := map[int64]string{}
	nameOf := func(playerID int64) (string, error) {
		if known, ok := names[playerID]; ok {
			return known, nil
		}
		name, err := playerName(ctx, tx, playerID)
		if err != nil {
			return "", err
		}
		names[playerID] = name
		return name, nil
	}

	attackingParticipants := make([]report.Participant, 0, len(attackers))
	for index, attacker := range attackers {
		name, err := nameOf(attacker.row.ownerPlayerID)
		if err != nil {
			return err
		}
		levels, err := researchOfPlayer(ctx, tx, attacker.row.ownerPlayerID)
		if err != nil {
			return err
		}
		attackingParticipants = append(attackingParticipants, participantOf(result.Attackers[index], name, levels))
	}
	defenderName, err := nameOf(defenderPlayerID)
	if err != nil {
		return err
	}
	defendingParticipants := []report.Participant{participantOf(result.Defenders[0], defenderName, defenderLevels)}
	for index, defender := range stationed {
		name, err := nameOf(defender.row.ownerPlayerID)
		if err != nil {
			return err
		}
		levels, err := researchOfPlayer(ctx, tx, defender.row.ownerPlayerID)
		if err != nil {
			return err
		}
		defendingParticipants = append(defendingParticipants,
			participantOf(result.Defenders[index+1], name, levels))
	}

	shared := report.CombatPayload{
		Coordinate: at,
		Outcome:    string(result.Outcome),
		Attackers:  attackingParticipants,
		Defenders:  defendingParticipants,
		Rounds:     roundsOf(result.Rounds),
		Loot:       loot,
		Debris:     result.Debris,
		MoonChance: result.MoonChance,
	}
	defenderPayload := shared
	defenderPayload.Rebuilt = inventoryDocument(result.Defenders[0].Rebuilt)

	written := map[int64]bool{}
	for _, attacker := range attackers {
		if written[attacker.row.ownerPlayerID] {
			continue
		}
		written[attacker.row.ownerPlayerID] = true
		if _, err := insertReport(ctx, tx, attacker.row.ownerPlayerID, report.CombatAttack,
			"planet", targetPlanetID, at, dueAt, shared); err != nil {
			return err
		}
	}
	if _, err := insertReport(ctx, tx, defenderPlayerID, report.CombatDefense,
		"planet", targetPlanetID, at, dueAt, defenderPayload); err != nil {
		return err
	}
	defended := map[int64]bool{defenderPlayerID: true}
	for _, defender := range stationed {
		if defended[defender.row.ownerPlayerID] {
			continue
		}
		defended[defender.row.ownerPlayerID] = true
		if _, err := insertReport(ctx, tx, defender.row.ownerPlayerID, report.CombatDefense,
			"planet", targetPlanetID, at, dueAt, defenderPayload); err != nil {
			return err
		}
	}
	return nil
}
