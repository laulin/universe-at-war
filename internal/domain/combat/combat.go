package combat

import (
	"errors"
	"fmt"
	"math"
	"slices"

	"universeatwar/internal/domain/economy"
	"universeatwar/internal/domain/random"
	"universeatwar/internal/domain/rules"
	"universeatwar/internal/domain/unit"
)

var (
	ErrNoAttacker      = errors.New("combat: the attacking side is empty")
	ErrInvalidUnits    = errors.New("combat: invalid unit or quantity")
	ErrForbiddenUnit   = errors.New("combat: this unit cannot take part in a battle")
	ErrTooManyUnits    = errors.New("combat: too many units for one battle")
	ErrInvalidSettings = errors.New("combat: invalid combat settings")
)

// Outcome is who is left standing.
type Outcome string

const (
	AttackerWins Outcome = "attacker"
	DefenderWins Outcome = "defender"
	Draw         Outcome = "draw"
)

// Factors are the technology multipliers of one party.
type Factors struct {
	Weapons float64
	Shield  float64
	Armour  float64
}

// Party is one player taking part in the battle.
type Party struct {
	PlayerID     int64
	Units        map[unit.ID]int64
	Technologies Factors
}

// CostMultipliers are the ruleset multipliers used to price the debris, exactly
// as the shipyard priced the units.
type CostMultipliers struct {
	Ship    float64
	Defense float64
}

// Input is everything the battle needs.
type Input struct {
	Attackers   []Party
	Defenders   []Party
	Rules       rules.CombatSettings
	Multipliers CostMultipliers
	Catalogue   unit.Catalogue
}

// SideStats is the public account of one side during one round.
type SideStats struct {
	Shots    int64
	Damage   int64
	Absorbed int64
}

// Round is one exchange of fire.
type Round struct {
	Number    int
	Attackers SideStats
	Defenders SideStats
}

// PartyResult is what one player has left after the battle.
type PartyResult struct {
	PlayerID  int64
	Initial   map[unit.ID]int64
	Survivors map[unit.ID]int64
	Losses    map[unit.ID]int64
	Rebuilt   map[unit.ID]int64
}

// Result is the full outcome of a battle.
type Result struct {
	Outcome    Outcome
	Rounds     []Round
	Attackers  []PartyResult
	Defenders  []PartyResult
	Debris     economy.Resources
	MoonChance float64
}

// instance is one unit on the battlefield.
type instance struct {
	kind   int32
	hull   int64
	shield int64
	alive  bool
}

// kindStats are the effective statistics of one unit type of one party.
type kindStats struct {
	id        unit.ID
	party     int
	weapon    int64
	shieldMax int64
	hullMax   int64
	rapidFire map[unit.ID]int
	paid      economy.Resources
	defense   bool
}

// side is one camp laid out in a stable order: parties in the order received,
// unit types by identifier, instances by index.
type side struct {
	kinds     []kindStats
	instances []instance
	alive     []int32
	parties   []PartyResult
}

// Resolve fights the battle. The same input and the same seed always give the
// same result, which is what makes a battle replayable from its report.
func Resolve(input Input, source random.Source) (Result, error) {
	if input.Rules.MaximumRounds <= 0 {
		return Result{}, ErrInvalidSettings
	}
	if input.Multipliers.Ship <= 0 || input.Multipliers.Defense <= 0 {
		return Result{}, ErrInvalidSettings
	}
	attackers, err := buildSide(input.Attackers, input, false)
	if err != nil {
		return Result{}, err
	}
	defenders, err := buildSide(input.Defenders, input, true)
	if err != nil {
		return Result{}, err
	}
	if len(attackers.instances) == 0 {
		return Result{}, ErrNoAttacker
	}

	var rounds []Round
	for number := 1; number <= input.Rules.MaximumRounds; number++ {
		if len(attackers.alive) == 0 || len(defenders.alive) == 0 {
			break
		}
		round := Round{Number: number}
		attackers.regenerateShields()
		defenders.regenerateShields()
		round.Attackers = fire(&attackers, &defenders, source)
		round.Defenders = fire(&defenders, &attackers, source)
		attackers.settleExplosions(source)
		defenders.settleExplosions(source)
		rounds = append(rounds, round)
	}

	result := Result{Rounds: rounds}
	result.Outcome = outcomeOf(len(attackers.alive), len(defenders.alive))
	attackers.collect()
	defenders.collect()
	rebuildDefenses(&defenders, input.Rules.DefenseRebuildChance, source)
	result.Attackers = attackers.parties
	result.Defenders = defenders.parties
	result.Debris = debrisOf(attackers, defenders, input.Rules)
	result.MoonChance = MoonChance(result.Debris, input.Rules.MaximumMoonChance)
	return result, nil
}

// buildSide lays out one camp and computes the effective statistics once.
func buildSide(parties []Party, input Input, defending bool) (side, error) {
	built := side{parties: make([]PartyResult, 0, len(parties))}
	var total int64
	for index, party := range parties {
		result := PartyResult{
			PlayerID:  party.PlayerID,
			Initial:   make(map[unit.ID]int64, len(party.Units)),
			Survivors: map[unit.ID]int64{},
			Losses:    map[unit.ID]int64{},
		}
		for _, id := range sortedUnits(party.Units) {
			quantity := party.Units[id]
			if quantity < 0 {
				return side{}, ErrInvalidUnits
			}
			if quantity == 0 {
				continue
			}
			definition, known := input.Catalogue.Definition(id)
			if !known {
				return side{}, fmt.Errorf("%w: %s", ErrInvalidUnits, id)
			}
			if definition.SiloSlots > 0 {
				return side{}, fmt.Errorf("%w: %s", ErrForbiddenUnit, id)
			}
			if !defending && definition.Family == unit.Defense {
				return side{}, fmt.Errorf("%w: %s", ErrForbiddenUnit, id)
			}
			total += quantity
			if total > MaximumUnitsPerSide {
				return side{}, ErrTooManyUnits
			}
			stats := statsOf(definition, party.Technologies, input.Multipliers)
			stats.party = index
			built.kinds = append(built.kinds, stats)
			kind := int32(len(built.kinds) - 1)
			for range quantity {
				built.instances = append(built.instances, instance{
					kind: kind, hull: stats.hullMax, shield: stats.shieldMax, alive: true,
				})
			}
			result.Initial[id] = quantity
		}
		built.parties = append(built.parties, result)
	}
	built.alive = make([]int32, 0, len(built.instances))
	for index := range built.instances {
		built.alive = append(built.alive, int32(index))
	}
	return built, nil
}

// statsOf applies the technologies once, rounding to the nearest integer so the
// battle itself is pure integer arithmetic.
func statsOf(definition unit.Definition, factors Factors, multipliers CostMultipliers) kindStats {
	multiplier := multipliers.Ship
	if definition.Family == unit.Defense {
		multiplier = multipliers.Defense
	}
	return kindStats{
		id:        definition.ID,
		weapon:    rounded(definition.Weapon, factors.Weapons),
		shieldMax: rounded(definition.Shield, factors.Shield),
		hullMax:   max64(1, rounded(definition.Hull(), factors.Armour)),
		rapidFire: definition.RapidFire,
		paid: economy.Resources{
			Metal:   rounded(definition.BaseCost.Metal, multiplier),
			Crystal: rounded(definition.BaseCost.Crystal, multiplier),
		},
		defense: definition.Family == unit.Defense,
	}
}

func (s *side) regenerateShields() {
	for _, index := range s.alive {
		s.instances[index].shield = s.kinds[s.instances[index].kind].shieldMax
	}
}

// fire makes every unit of the shooting side take its shot, in layout order.
func fire(shooting, target *side, source random.Source) SideStats {
	var stats SideStats
	if len(target.alive) == 0 {
		return stats
	}
	for _, index := range shooting.alive {
		shooter := shooting.kinds[shooting.instances[index].kind]
		for {
			victim := target.alive[pick(source, len(target.alive))]
			stats.Shots++
			damage, absorbed := shoot(shooter, target, victim)
			stats.Damage += damage
			stats.Absorbed += absorbed
			shots := shooter.rapidFire[target.kinds[target.instances[victim].kind].id]
			if shots <= 1 || !random.Chance(source, float64(shots-1)/float64(shots)) {
				break
			}
		}
	}
	return stats
}

// pick draws a target uniformly, without consuming a draw when there is only
// one possible target.
func pick(source random.Source, count int) int {
	if count <= 1 {
		return 0
	}
	return source.IntN(count)
}

// shoot applies one shot and reports the damage dealt and the damage absorbed.
func shoot(shooter kindStats, target *side, victim int32) (int64, int64) {
	stats := target.kinds[target.instances[victim].kind]
	damage := shooter.weapon
	// A shot worth less than one percent of the shield bounces off it.
	if damage*100/shieldBouncePercent < stats.shieldMax {
		return 0, 0
	}
	shield := target.instances[victim].shield
	if damage <= shield {
		target.instances[victim].shield = shield - damage
		return damage, damage
	}
	target.instances[victim].shield = 0
	target.instances[victim].hull -= damage - shield
	return damage, shield
}

// settleExplosions destroys the units that cannot hold any longer, at the end
// of the round so both sides shot at the same battlefield.
func (s *side) settleExplosions(source random.Source) {
	remaining := s.alive[:0]
	for _, index := range s.alive {
		current := &s.instances[index]
		stats := s.kinds[current.kind]
		switch {
		case current.hull <= 0:
			current.alive = false
		case current.hull*100 < stats.hullMax*explosionThresholdPercent:
			if !random.Chance(source, float64(current.hull)/float64(stats.hullMax)) {
				current.alive = false
			}
		}
		if current.alive {
			remaining = append(remaining, index)
		}
	}
	s.alive = remaining
}

// collect counts the survivors and the losses per party.
func (s *side) collect() {
	for _, index := range s.alive {
		stats := s.kinds[s.instances[index].kind]
		s.parties[stats.party].Survivors[stats.id]++
	}
	for index := range s.parties {
		party := &s.parties[index]
		for id, initial := range party.Initial {
			if lost := initial - party.Survivors[id]; lost > 0 {
				party.Losses[id] = lost
			}
		}
	}
}

// rebuildDefenses gives every destroyed defense its chance to be rebuilt.
func rebuildDefenses(defenders *side, chance float64, source random.Source) {
	byParty := make([]map[unit.ID]bool, len(defenders.parties))
	for _, stats := range defenders.kinds {
		if !stats.defense {
			continue
		}
		if byParty[stats.party] == nil {
			byParty[stats.party] = map[unit.ID]bool{}
		}
		byParty[stats.party][stats.id] = true
	}
	for index := range defenders.parties {
		party := &defenders.parties[index]
		for _, id := range sortedUnits(party.Losses) {
			if !byParty[index][id] {
				continue
			}
			var rebuilt int64
			for range party.Losses[id] {
				if random.Chance(source, chance) {
					rebuilt++
				}
			}
			if rebuilt == 0 {
				continue
			}
			if party.Rebuilt == nil {
				party.Rebuilt = map[unit.ID]int64{}
			}
			party.Rebuilt[id] = rebuilt
			party.Survivors[id] += rebuilt
			if remaining := party.Losses[id] - rebuilt; remaining > 0 {
				party.Losses[id] = remaining
			} else {
				delete(party.Losses, id)
			}
		}
	}
}

// debrisOf prices the wrecks with the cost their owners actually paid.
func debrisOf(attackers, defenders side, settings rules.CombatSettings) economy.Resources {
	shipMetal, shipCrystal := lostValue(attackers, false)
	defenderShipMetal, defenderShipCrystal := lostValue(defenders, false)
	defenseMetal, defenseCrystal := lostValue(defenders, true)
	return economy.Resources{
		Metal: floored(settings.ShipsToDebris*float64(shipMetal+defenderShipMetal)) +
			floored(settings.DefensesToDebris*float64(defenseMetal)),
		Crystal: floored(settings.ShipsToDebris*float64(shipCrystal+defenderShipCrystal)) +
			floored(settings.DefensesToDebris*float64(defenseCrystal)),
	}
}

// lostValue totals what one family of one side lost, at the price paid for it.
func lostValue(camp side, defenses bool) (int64, int64) {
	priced := map[int]map[unit.ID]economy.Resources{}
	for _, stats := range camp.kinds {
		if stats.defense != defenses {
			continue
		}
		if priced[stats.party] == nil {
			priced[stats.party] = map[unit.ID]economy.Resources{}
		}
		priced[stats.party][stats.id] = stats.paid
	}
	var metal, crystal int64
	for index, party := range camp.parties {
		for id, lost := range party.Losses {
			paid, known := priced[index][id]
			if !known {
				continue
			}
			metal += lost * paid.Metal
			crystal += lost * paid.Crystal
		}
	}
	return metal, crystal
}

// MoonChance is the probability a moon forms over the wreckage.
func MoonChance(debris economy.Resources, maximum float64) float64 {
	steps := (debris.Metal + debris.Crystal) / moonChanceDebrisStep
	chance := float64(steps) / 100
	if chance > maximum {
		return maximum
	}
	return chance
}

func outcomeOf(attackers, defenders int) Outcome {
	switch {
	case defenders == 0 && attackers > 0:
		return AttackerWins
	case attackers == 0 && defenders > 0:
		return DefenderWins
	default:
		return Draw
	}
}

func sortedUnits[V any](units map[unit.ID]V) []unit.ID {
	identifiers := make([]unit.ID, 0, len(units))
	for id := range units {
		identifiers = append(identifiers, id)
	}
	slices.Sort(identifiers)
	return identifiers
}

func rounded(base int64, factor float64) int64 {
	value := math.Floor(float64(base)*factor + .5)
	if math.IsNaN(value) || value < 0 {
		return 0
	}
	if value > math.MaxInt64 {
		return math.MaxInt64
	}
	return int64(value)
}

func floored(value float64) int64 {
	if math.IsNaN(value) || value <= 0 {
		return 0
	}
	if value > math.MaxInt64 {
		return math.MaxInt64
	}
	return int64(math.Floor(value))
}

func max64(first, second int64) int64 {
	if first > second {
		return first
	}
	return second
}
