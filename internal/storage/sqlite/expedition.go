package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"universeatwar/internal/domain/combat"
	"universeatwar/internal/domain/debris"
	"universeatwar/internal/domain/economy"
	"universeatwar/internal/domain/expedition"
	domainfleet "universeatwar/internal/domain/fleet"
	"universeatwar/internal/domain/random"
	"universeatwar/internal/domain/report"
	"universeatwar/internal/domain/rules"
	"universeatwar/internal/domain/unit"
)

// activeExpeditionCount counts the expeditions a player already has out there,
// which astrophysics bounds.
func activeExpeditionCount(ctx context.Context, tx *sql.Tx, playerID int64) (int, error) {
	var count int
	if err := tx.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM fleets
		WHERE owner_player_id = ? AND mission = 'expedition' AND state IN ('outbound', 'holding', 'returning')
	`, playerID).Scan(&count); err != nil {
		return 0, fmt.Errorf("fleet repository: count expeditions: %w", err)
	}
	return count, nil
}

// resolveExpedition draws what the fleet found out there, applies it and turns
// the fleet round. The whole result lands in one transaction, so a redelivered
// event never doubles a find.
func (r *FleetRepository) resolveExpedition(ctx context.Context, tx *sql.Tx, row fleetRow, dueAt, now time.Time) error {
	configured, _, err := activeRuleset(ctx, tx)
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
	source := random.NewSeeded(uint64(row.seed))
	outcome, err := expedition.Draw(configured.Expedition, source)
	if err != nil {
		return err
	}
	hold, err := domainfleet.Capacity(composition, r.catalogues.Units)
	if err != nil {
		return err
	}
	capacity := hold - cargo.Metal - cargo.Crystal - cargo.Deuterium
	if capacity < 0 {
		capacity = 0
	}
	payload := report.ExpeditionPayload{
		Position: row.target, Outcome: string(outcome), Story: expeditionStory(outcome),
	}

	switch outcome {
	case expedition.Resources, expedition.Rare:
		factor := configured.Expedition.ResourceFactor
		if outcome == expedition.Rare {
			factor = configured.Expedition.RareFactor
		}
		found := expedition.Find(capacity, factor)
		if err := storeCargo(ctx, tx, row.id, economy.Resources{
			Metal: cargo.Metal + found.Metal, Crystal: cargo.Crystal + found.Crystal,
			Deuterium: cargo.Deuterium + found.Deuterium,
		}); err != nil {
			return err
		}
		payload.Found = found
	case expedition.Ships:
		definition, known := r.catalogues.Units.Definition(unit.SmallCargo)
		if !known {
			break
		}
		gained := expedition.Salvage(capacity, configured.Expedition.ShipFactor, definition.BaseCost)
		if gained > 0 {
			if err := addShips(ctx, tx, row.id, unit.SmallCargo, gained); err != nil {
				return err
			}
			payload.Gained = map[string]int64{string(unit.SmallCargo): gained}
		}
	case expedition.Delay:
		delay := time.Duration(float64(configured.Expedition.HoldHours) *
			configured.Expedition.DelayFactor * float64(time.Hour))
		if delay > 0 && row.returnsAt != nil {
			delayed := row.returnsAt.Add(delay)
			if _, err := tx.ExecContext(ctx, "UPDATE fleets SET returns_at = ? WHERE id = ?",
				timestamp(delayed), row.id); err != nil {
				return fmt.Errorf("fleet repository: delay the return: %w", err)
			}
			row.returnsAt = &delayed
			payload.DelayedBy = int64(delay / time.Second)
		}
	case expedition.Losses:
		losses := expedition.Lose(composition, configured.Expedition.LossShare)
		survivors := map[unit.ID]int64{}
		for id, quantity := range composition {
			survivors[id] = quantity - losses[id]
		}
		if err := applyFleetLosses(ctx, tx, row.id, composition, survivors); err != nil {
			return err
		}
		payload.Lost = inventoryDocument(losses)
		payload.Survivors = inventoryDocument(survivors)
	case expedition.Pirates, expedition.Aliens:
		survivors, ambush, err := r.fightAmbush(ctx, tx, row, composition, outcome, configured, source, now)
		if err != nil {
			return err
		}
		payload.Ambush = inventoryDocument(ambush)
		payload.Survivors = inventoryDocument(survivors)
		payload.Lost = inventoryDocument(lossesOf(composition, survivors))
		if totalShips(survivors) == 0 {
			if _, err := insertReport(ctx, tx, row.ownerPlayerID, report.Expedition, "fleet", row.id,
				row.target, dueAt, payload); err != nil {
				return err
			}
			if err := transitionFleet(ctx, tx, row, domainfleet.Destroyed, "lost_on_expedition", now); err != nil {
				return err
			}
			return logFleetEvent(ctx, tx, "expedition_resolved", row.id, row.originPlanetID, now,
				fmt.Sprintf("json_object('outcome', '%s', 'seed', %d, 'lost', 1)", outcome, row.seed))
		}
	}

	if _, err := insertReport(ctx, tx, row.ownerPlayerID, report.Expedition, "fleet", row.id,
		row.target, dueAt, payload); err != nil {
		return err
	}
	if err := logFleetEvent(ctx, tx, "expedition_resolved", row.id, row.originPlanetID, now,
		fmt.Sprintf("json_object('outcome', '%s', 'seed', %d, 'found_metal', %d, 'found_crystal', %d, 'found_deuterium', %d)",
			outcome, row.seed, payload.Found.Metal, payload.Found.Crystal, payload.Found.Deuterium)); err != nil {
		return err
	}
	return r.sendHome(ctx, tx, row, "expedition_resolved", now)
}

// fightAmbush settles the meeting an expedition did not want, with the same
// engine as any other battle. The wreckage falls where it happened.
func (r *FleetRepository) fightAmbush(ctx context.Context, tx *sql.Tx, row fleetRow,
	composition domainfleet.Composition, outcome expedition.Outcome, configured rules.Ruleset,
	source random.Source, now time.Time) (map[unit.ID]int64, map[unit.ID]int64, error) {
	levels, err := researchOfPlayer(ctx, tx, row.ownerPlayerID)
	if err != nil {
		return nil, nil, err
	}
	strength := r.catalogues.Units.Strength(composition)
	ambush := expedition.Ambush(strength, outcome, source)
	if len(ambush) == 0 {
		return composition, nil, nil
	}
	result, err := combat.Resolve(combat.Input{
		Attackers: []combat.Party{{PlayerID: row.ownerPlayerID, Units: composition, Technologies: factorsOf(levels)}},
		Defenders: []combat.Party{{Units: ambush, Technologies: combat.Factors{Weapons: 1, Shield: 1, Armour: 1}}},
		Rules:     configured.Combat,
		Multipliers: combat.CostMultipliers{
			Ship: configured.Progression.ShipCostMultiplier, Defense: configured.Progression.DefenseCostMultiplier,
		},
		Catalogue: r.catalogues.Units,
	}, source)
	if err != nil {
		return nil, nil, err
	}
	survivors := result.Attackers[0].Survivors
	if err := applyFleetLosses(ctx, tx, row.id, composition, survivors); err != nil {
		return nil, nil, err
	}
	if err := addDebris(ctx, tx, row.target,
		debris.Field{Metal: result.Debris.Metal, Crystal: result.Debris.Crystal}, now); err != nil {
		return nil, nil, err
	}
	return survivors, ambush, nil
}

// addShips puts what an expedition brought back into the fleet that found it.
func addShips(ctx context.Context, tx *sql.Tx, fleetID int64, id unit.ID, quantity int64) error {
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO fleet_ships(fleet_id, unit_id, quantity) VALUES (?, ?, ?)
		ON CONFLICT(fleet_id, unit_id) DO UPDATE SET quantity = quantity + excluded.quantity
	`, fleetID, string(id), quantity); err != nil {
		return fmt.Errorf("fleet repository: add ships: %w", err)
	}
	return nil
}

func lossesOf(before domainfleet.Composition, survivors map[unit.ID]int64) map[unit.ID]int64 {
	losses := map[unit.ID]int64{}
	for id, quantity := range before {
		if lost := quantity - survivors[id]; lost > 0 {
			losses[id] = lost
		}
	}
	return losses
}

// expeditionStory is the sentence the report leads with.
func expeditionStory(outcome expedition.Outcome) string {
	switch outcome {
	case expedition.Resources:
		return "La flotte revient les soutes pleines."
	case expedition.Rare:
		return "La flotte a trouvé bien plus qu'espéré."
	case expedition.Ships:
		return "La flotte ramène des vaisseaux abandonnés."
	case expedition.Delay:
		return "La flotte s'est égarée et rentrera en retard."
	case expedition.Pirates:
		return "Des pirates attendaient la flotte."
	case expedition.Aliens:
		return "Une flotte inconnue a barré la route."
	case expedition.Losses:
		return "Une avarie a coûté des vaisseaux à la flotte."
	default:
		return "La flotte n'a rien trouvé."
	}
}
