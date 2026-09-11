// Package battlesimulation estimates an attack exclusively from intelligence
// a player is allowed to read. It never inspects the target's live state.
package battlesimulation

import (
	"context"
	"errors"
	"fmt"
	"hash/fnv"
	"slices"
	"time"

	appauth "universeatwar/internal/app/authentication"
	appeconomy "universeatwar/internal/app/economy"
	appreports "universeatwar/internal/app/reports"
	"universeatwar/internal/domain/catalogue"
	"universeatwar/internal/domain/combat"
	"universeatwar/internal/domain/economy"
	domainfleet "universeatwar/internal/domain/fleet"
	"universeatwar/internal/domain/random"
	"universeatwar/internal/domain/report"
	"universeatwar/internal/domain/research"
	"universeatwar/internal/domain/rules"
	"universeatwar/internal/domain/unit"
	"universeatwar/internal/domain/universe"
)

var (
	ErrInvalidRequest    = errors.New("battle simulation: invalid request")
	ErrUnsupportedReport = errors.New("battle simulation: report cannot describe an attack")
	ErrTargetMismatch    = errors.New("battle simulation: report and mission have different targets")
)

// Reports is the intelligence boundary. Its implementation enforces that a
// report belongs to the player or was explicitly shared with their alliance.
type Reports interface {
	Get(context.Context, appauth.Principal, int64) (appreports.Detail, error)
}

// Economy reads only the attacking player's current technology. The target is
// deliberately absent from this interface.
type Economy interface {
	Planet(context.Context, appauth.Principal, int64) (appeconomy.Planet, error)
}

// Request identifies the immutable report and the fleet already validated by
// the ordinary mission preview.
type Request struct {
	ReportID     int64
	OriginPlanet int64
	Target       universe.Coordinate
	Composition  domainfleet.Composition
	Cargo        economy.Resources
	Fuel         int64
}

// Metric summarises the simulations without pretending one random fight is a
// prediction. Average is accompanied by the observed bounds.
type Metric struct {
	Minimum int64
	Average int64
	Maximum int64
}

// ResourceMetrics applies the same summary to each resource and their total.
type ResourceMetrics struct {
	Metal     Metric
	Crystal   Metric
	Deuterium Metric
	Total     Metric
}

// UnitLossMetric keeps fractional averages meaningful when an individual unit
// is lost in only some of the sampled battles.
type UnitLossMetric struct {
	ID      unit.ID
	Minimum int64
	Average float64
	Maximum int64
}

// Estimate is the safe projection rendered before launch. Missing means the
// report did not reveal enough to fight even an estimated battle.
type Estimate struct {
	Available  bool
	Kind       report.Kind
	OccurredAt time.Time
	Freshness  report.Freshness
	Samples    int
	Wins       int
	Draws      int
	Defeats    int
	WinRate    float64
	DrawRate   float64
	LossRate   float64

	AttackerLosses        ResourceMetrics
	DefenderLosses        ResourceMetrics
	AttackerShipLosses    []UnitLossMetric
	DefenderShipLosses    []UnitLossMetric
	DefenderDefenseLosses []UnitLossMetric
	ShipDebris            ResourceMetrics
	DefenseDebris         ResourceMetrics
	Debris                ResourceMetrics
	MoonChance            Metric
	Loot                  ResourceMetrics
	LootKnown             bool
	Net                   Metric
	NetWithDebris         Metric
	Missing               []string
	Warnings              []string
	UnavailableReason     string
}

// Service adapts reports to the pure combat engine and repeats the battle with
// stable seeds. It persists no simulation result and needs no simulation table.
type Service struct {
	Reports    Reports
	Economy    Economy
	Catalogues catalogue.Set
}

type defenderBlueprint struct {
	playerID     int64
	units        map[unit.ID]int64
	technologies combat.Factors
}

type intelligence struct {
	defenders []defenderBlueprint
	resources *economy.Resources
	missing   []string
	warnings  []string
}

// Estimate runs a bounded Monte Carlo projection. Every enemy figure comes
// from the chosen report; only the caller's technology is read live.
func (s Service) Estimate(ctx context.Context, principal appauth.Principal, request Request) (Estimate, error) {
	if s.Reports == nil || s.Economy == nil {
		return Estimate{}, errors.New("battle simulation: incomplete dependencies")
	}
	if request.ReportID <= 0 || request.OriginPlanet <= 0 || len(request.Composition) == 0 || request.Fuel < 0 {
		return Estimate{}, ErrInvalidRequest
	}
	if err := request.Cargo.Validate(); err != nil {
		return Estimate{}, fmt.Errorf("%w: %v", ErrInvalidRequest, err)
	}
	if err := request.Composition.Validate(s.Catalogues.Units); err != nil {
		return Estimate{}, fmt.Errorf("%w: %v", ErrInvalidRequest, err)
	}
	detail, err := s.Reports.Get(ctx, principal, request.ReportID)
	if err != nil {
		return Estimate{}, err
	}
	if detail.Summary.Coordinate != request.Target {
		return Estimate{}, ErrTargetMismatch
	}
	origin, err := s.Economy.Planet(ctx, principal, request.OriginPlanet)
	if err != nil {
		return Estimate{}, err
	}
	known, err := intelligenceOf(detail, origin.Rules, s.Catalogues.Units)
	if err != nil {
		return Estimate{}, err
	}
	estimate := Estimate{
		Kind: detail.Summary.Kind, OccurredAt: detail.Summary.OccurredAt, Freshness: detail.Summary.Freshness,
		Missing: known.missing, Warnings: known.warnings, LootKnown: known.resources != nil,
	}
	if detail.Summary.Freshness != report.Recent {
		estimate.Warnings = append(estimate.Warnings, "Le rapport est ancien : la cible peut avoir changé avant l'arrivée.")
	}
	if len(known.missing) > 0 {
		return estimate, nil
	}
	if units := battleUnits(request.Composition, known.defenders); units > 500_000 {
		estimate.UnavailableReason = "La bataille dépasse la limite de calcul interactif de 500 000 unités."
		return estimate, nil
	}

	attacker := combat.Party{
		PlayerID: 1, Units: cloneUnits(request.Composition),
		Technologies: combat.Factors{
			Weapons: origin.Researches.WeaponsFactor(), Shield: origin.Researches.ShieldingFactor(),
			Armour: origin.Researches.ArmourFactor(),
		},
	}
	samples := sampleCount(request.Composition, known.defenders)
	results := sampleValues{
		attackerLosses: make([]economy.Resources, 0, samples), defenderLosses: make([]economy.Resources, 0, samples),
		attackerUnitLosses: unitLossSamples(request.Composition), defenderUnitLosses: defenderLossSamples(known.defenders),
		shipDebris: make([]economy.Resources, 0, samples), defenseDebris: make([]economy.Resources, 0, samples),
		debris: make([]economy.Resources, 0, samples), moonChance: make([]int64, 0, samples),
	}
	if estimate.LootKnown {
		results.loot = make([]economy.Resources, 0, samples)
		results.net = make([]int64, 0, samples)
		results.netWithDebris = make([]int64, 0, samples)
	}
	for sample := range samples {
		if err := ctx.Err(); err != nil {
			return Estimate{}, err
		}
		source := random.NewSeeded(sampleSeed(request, sample))
		defenders := sampledDefenders(known.defenders)
		result, err := combat.Resolve(combat.Input{
			Attackers: []combat.Party{attacker}, Defenders: defenders,
			Rules: origin.Rules.Combat,
			Multipliers: combat.CostMultipliers{
				Ship: origin.Rules.Progression.ShipCostMultiplier, Defense: origin.Rules.Progression.DefenseCostMultiplier,
			},
			Catalogue: s.Catalogues.Units,
		}, source)
		if err != nil {
			return Estimate{}, fmt.Errorf("battle simulation: resolve: %w", err)
		}
		switch result.Outcome {
		case combat.AttackerWins:
			estimate.Wins++
		case combat.Draw:
			estimate.Draws++
		case combat.DefenderWins:
			estimate.Defeats++
		}
		attackerLosses, err := lossValue(result.Attackers, origin.Rules, s.Catalogues.Units)
		if err != nil {
			return Estimate{}, err
		}
		defenderLosses, err := lossValue(result.Defenders, origin.Rules, s.Catalogues.Units)
		if err != nil {
			return Estimate{}, err
		}
		results.attackerLosses = append(results.attackerLosses, attackerLosses)
		results.defenderLosses = append(results.defenderLosses, defenderLosses)
		appendUnitLossSample(results.attackerUnitLosses, result.Attackers, sample)
		appendUnitLossSample(results.defenderUnitLosses, result.Defenders, sample)
		results.shipDebris = append(results.shipDebris, result.ShipDebris)
		results.defenseDebris = append(results.defenseDebris, result.DefenseDebris)
		results.debris = append(results.debris, result.Debris)
		results.moonChance = append(results.moonChance, int64(result.MoonChance*100+0.5))
		if known.resources != nil {
			loot := economy.Resources{}
			if result.Outcome == combat.AttackerWins {
				capacity, err := survivorCapacity(result.Attackers[0].Survivors, request.Cargo, s.Catalogues.Units)
				if err != nil {
					return Estimate{}, err
				}
				loot = combat.Pillage(*known.resources, capacity,
					min(origin.Rules.Economy.PillageRatio, origin.Rules.Combat.MaximumPillage))
			}
			results.loot = append(results.loot, loot)
			net := resourceTotal(loot) - resourceTotal(attackerLosses) - request.Fuel
			results.net = append(results.net, net)
			results.netWithDebris = append(results.netWithDebris, net+resourceTotal(result.Debris))
		}
	}
	estimate.Available = true
	estimate.Samples = samples
	estimate.WinRate = percentage(estimate.Wins, samples)
	estimate.DrawRate = percentage(estimate.Draws, samples)
	estimate.LossRate = percentage(estimate.Defeats, samples)
	estimate.AttackerLosses = resourceMetrics(results.attackerLosses)
	estimate.DefenderLosses = resourceMetrics(results.defenderLosses)
	estimate.AttackerShipLosses, _ = classifiedUnitLosses(results.attackerUnitLosses, s.Catalogues.Units)
	estimate.DefenderShipLosses, estimate.DefenderDefenseLosses = classifiedUnitLosses(results.defenderUnitLosses, s.Catalogues.Units)
	estimate.ShipDebris = resourceMetrics(results.shipDebris)
	estimate.DefenseDebris = resourceMetrics(results.defenseDebris)
	estimate.Debris = resourceMetrics(results.debris)
	estimate.MoonChance = metric(results.moonChance)
	if estimate.LootKnown {
		estimate.Loot = resourceMetrics(results.loot)
		estimate.Net = metric(results.net)
		estimate.NetWithDebris = metric(results.netWithDebris)
	}
	return estimate, nil
}

type sampleValues struct {
	attackerLosses     []economy.Resources
	defenderLosses     []economy.Resources
	attackerUnitLosses map[unit.ID][]int64
	defenderUnitLosses map[unit.ID][]int64
	shipDebris         []economy.Resources
	defenseDebris      []economy.Resources
	debris             []economy.Resources
	moonChance         []int64
	loot               []economy.Resources
	net                []int64
	netWithDebris      []int64
}

func intelligenceOf(detail appreports.Detail, configured rules.Ruleset, units unit.Catalogue) (intelligence, error) {
	switch payload := detail.Payload.(type) {
	case report.EspionagePayload:
		known := intelligence{}
		fleetKnown := payload.Fleet != nil || payload.Level >= configured.Espionage.FleetThreshold
		defensesKnown := payload.Defenses != nil || payload.Level >= configured.Espionage.DefensesThreshold
		researchKnown := payload.Research != nil || payload.Level >= configured.Espionage.ResearchThreshold
		if !fleetKnown {
			known.missing = append(known.missing, "flotte ennemie")
		}
		if !defensesKnown {
			known.missing = append(known.missing, "défenses ennemies")
		}
		if len(known.missing) > 0 {
			return known, nil
		}
		inventory := documentUnits(payload.Fleet)
		for id, quantity := range documentUnits(payload.Defenses) {
			inventory[id] += quantity
		}
		levels := documentResearch(payload.Research)
		if !researchKnown {
			known.warnings = append(known.warnings, "Technologies ennemies inconnues : le scénario suppose les niveaux 0.")
		}
		known.defenders = []defenderBlueprint{{
			playerID: 2, units: inventory,
			technologies: combat.Factors{Weapons: levels.WeaponsFactor(), Shield: levels.ShieldingFactor(), Armour: levels.ArmourFactor()},
		}}
		known.resources = payload.Resources
		if payload.Resources == nil {
			known.warnings = append(known.warnings, "Ressources inconnues : le butin et le bilan net ne peuvent pas être estimés.")
		}
		return known, validateUnits(inventory, units)
	case report.CombatPayload:
		if detail.Summary.Kind != report.CombatAttack {
			return intelligence{}, ErrUnsupportedReport
		}
		if len(payload.Defenders) == 0 {
			return intelligence{missing: []string{"camp défenseur"}}, nil
		}
		known := intelligence{warnings: []string{
			"Le scénario repart des survivants du combat ; les mouvements et constructions ultérieurs restent inconnus.",
			"Ressources inconnues : le butin et le bilan net ne peuvent pas être estimés.",
		}}
		for index, participant := range payload.Defenders {
			blueprint := defenderBlueprint{
				playerID: int64(index + 2), units: documentUnits(participant.Survivors),
				technologies: combat.Factors{
					Weapons: factor(participant.Weapons), Shield: factor(participant.Shielding), Armour: factor(participant.Armour),
				},
			}
			if err := validateUnits(blueprint.units, units); err != nil {
				return intelligence{}, err
			}
			known.defenders = append(known.defenders, blueprint)
		}
		return known, nil
	default:
		return intelligence{}, ErrUnsupportedReport
	}
}

func documentUnits(document map[string]int64) map[unit.ID]int64 {
	result := make(map[unit.ID]int64, len(document))
	for id, quantity := range document {
		if quantity > 0 {
			result[unit.ID(id)] = quantity
		}
	}
	return result
}

func documentResearch(document map[string]int) research.Levels {
	levels := research.Levels{}
	for id, level := range document {
		if level >= 0 {
			levels[research.ID(id)] = level
		}
	}
	return levels
}

func factor(level int) float64 { return 1 + float64(max(level, 0))/10 }

func validateUnits(inventory map[unit.ID]int64, catalogue unit.Catalogue) error {
	for id := range inventory {
		if _, found := catalogue.Definition(id); !found {
			return fmt.Errorf("%w: unknown unit %s", ErrInvalidRequest, id)
		}
	}
	return nil
}

func sampledDefenders(blueprints []defenderBlueprint) []combat.Party {
	parties := make([]combat.Party, 0, len(blueprints))
	for _, blueprint := range blueprints {
		inventory := cloneUnits(blueprint.units)
		parties = append(parties, combat.Party{PlayerID: blueprint.playerID, Units: inventory, Technologies: blueprint.technologies})
	}
	return parties
}

func survivorCapacity(survivors map[unit.ID]int64, cargo economy.Resources, catalogue unit.Catalogue) (int64, error) {
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
	return max(0, hold-resourceTotal(cargo)), nil
}

func lossValue(parties []combat.PartyResult, configured rules.Ruleset, catalogue unit.Catalogue) (economy.Resources, error) {
	value := economy.Resources{}
	for _, party := range parties {
		for id, quantity := range party.Losses {
			cost, err := catalogue.UnitCost(id, configured)
			if err != nil {
				return economy.Resources{}, fmt.Errorf("battle simulation: price %s: %w", id, err)
			}
			total, err := unit.TotalCost(cost, quantity)
			if err != nil {
				return economy.Resources{}, fmt.Errorf("battle simulation: price %d %s: %w", quantity, id, err)
			}
			value = value.Plus(total)
		}
	}
	return value, nil
}

// unitLossSamples starts with every unit present in the reported battle. Each
// later sample appends a value for every key, including zero, so a unit lost in
// one battle out of ten has an average of one tenth rather than one.
func unitLossSamples[V ~int64](inventory map[unit.ID]V) map[unit.ID][]int64 {
	samples := make(map[unit.ID][]int64, len(inventory))
	for id, quantity := range inventory {
		if quantity > 0 {
			samples[id] = nil
		}
	}
	return samples
}

func defenderLossSamples(defenders []defenderBlueprint) map[unit.ID][]int64 {
	samples := map[unit.ID][]int64{}
	for _, defender := range defenders {
		for id, quantity := range defender.units {
			if quantity > 0 {
				samples[id] = nil
			}
		}
	}
	return samples
}

func appendUnitLossSample(samples map[unit.ID][]int64, parties []combat.PartyResult, sample int) {
	losses := map[unit.ID]int64{}
	for _, party := range parties {
		for id, quantity := range party.Losses {
			losses[id] += quantity
			if _, known := samples[id]; !known {
				samples[id] = make([]int64, sample)
			}
		}
	}
	for id, values := range samples {
		samples[id] = append(values, losses[id])
	}
}

func classifiedUnitLosses(samples map[unit.ID][]int64, catalogue unit.Catalogue) (ships, defenses []UnitLossMetric) {
	for _, id := range sortedIDs(samples) {
		values := samples[id]
		if len(values) == 0 {
			continue
		}
		minimum, maximum := values[0], values[0]
		var sum int64
		for _, value := range values {
			minimum = min(minimum, value)
			maximum = max(maximum, value)
			sum += value
		}
		if maximum == 0 {
			continue
		}
		loss := UnitLossMetric{
			ID: id, Minimum: minimum, Average: float64(sum) / float64(len(values)), Maximum: maximum,
		}
		definition, _ := catalogue.Definition(id)
		if definition.Family == unit.Defense {
			defenses = append(defenses, loss)
		} else {
			ships = append(ships, loss)
		}
	}
	return ships, defenses
}

func resourceMetrics(values []economy.Resources) ResourceMetrics {
	metal := make([]int64, 0, len(values))
	crystal := make([]int64, 0, len(values))
	deuterium := make([]int64, 0, len(values))
	total := make([]int64, 0, len(values))
	for _, value := range values {
		metal = append(metal, value.Metal)
		crystal = append(crystal, value.Crystal)
		deuterium = append(deuterium, value.Deuterium)
		total = append(total, resourceTotal(value))
	}
	return ResourceMetrics{Metal: metric(metal), Crystal: metric(crystal), Deuterium: metric(deuterium), Total: metric(total)}
}

func metric(values []int64) Metric {
	if len(values) == 0 {
		return Metric{}
	}
	ordered := slices.Clone(values)
	slices.Sort(ordered)
	var sum int64
	for _, value := range ordered {
		sum += value
	}
	return Metric{Minimum: ordered[0], Average: sum / int64(len(ordered)), Maximum: ordered[len(ordered)-1]}
}

func resourceTotal(value economy.Resources) int64 {
	return value.Metal + value.Crystal + value.Deuterium
}

func sampleCount(attacker domainfleet.Composition, defenders []defenderBlueprint) int {
	units := battleUnits(attacker, defenders)
	if units <= 0 {
		return 64
	}
	samples := int(int64(500_000) / units)
	return max(1, min(64, samples))
}

func battleUnits(attacker domainfleet.Composition, defenders []defenderBlueprint) int64 {
	var units int64
	for _, quantity := range attacker {
		if quantity > 500_000-units {
			return 500_001
		}
		units += quantity
	}
	for _, defender := range defenders {
		for _, quantity := range defender.units {
			if quantity > 500_000-units {
				return 500_001
			}
			units += quantity
		}
	}
	return units
}

func sampleSeed(request Request, sample int) uint64 {
	hasher := fnv.New64a()
	_, _ = fmt.Fprintf(hasher, "%d:%d:%s:%d", request.ReportID, request.OriginPlanet, request.Target, sample)
	for _, id := range sortedIDs(request.Composition) {
		_, _ = fmt.Fprintf(hasher, ":%s=%d", id, request.Composition[id])
	}
	return hasher.Sum64()
}

func cloneUnits[V ~int64](source map[unit.ID]V) map[unit.ID]int64 {
	cloned := make(map[unit.ID]int64, len(source))
	for id, quantity := range source {
		cloned[id] = int64(quantity)
	}
	return cloned
}

func sortedIDs[V any](inventory map[unit.ID]V) []unit.ID {
	identifiers := make([]unit.ID, 0, len(inventory))
	for id := range inventory {
		identifiers = append(identifiers, id)
	}
	slices.Sort(identifiers)
	return identifiers
}

func percentage(count, total int) float64 {
	if total <= 0 {
		return 0
	}
	return float64(count) * 100 / float64(total)
}
