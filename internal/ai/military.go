package ai

import (
	"context"
	"fmt"
	"time"

	appai "universeatwar/internal/app/ai"
	appauth "universeatwar/internal/app/authentication"
	appeconomy "universeatwar/internal/app/economy"
	appfleet "universeatwar/internal/app/fleet"
	appgalaxy "universeatwar/internal/app/galaxy"
	appreports "universeatwar/internal/app/reports"
	domainai "universeatwar/internal/domain/ai"
	"universeatwar/internal/domain/building"
	domaineconomy "universeatwar/internal/domain/economy"
	domainfleet "universeatwar/internal/domain/fleet"
	"universeatwar/internal/domain/random"
	"universeatwar/internal/domain/report"
	"universeatwar/internal/domain/unit"
	"universeatwar/internal/domain/universe"
)

// Reports is the report shelf of a player: the only place an artificial player
// ever learns anything about somebody else.
type Reports interface {
	List(context.Context, appauth.Principal, appreports.Filter) ([]appreports.Summary, error)
	Get(context.Context, appauth.Principal, int64) (appreports.Detail, error)
	Share(context.Context, appauth.Principal, int64, bool) error
}

// Galaxy is the public map, which says who lives where and nothing more.
type Galaxy interface {
	System(context.Context, appauth.Principal, int, int) (appgalaxy.View, error)
}

// Fleet is the fleet page of a player.
type Fleet interface {
	Overview(context.Context, appauth.Principal, int64) (appfleet.Overview, error)
	Preview(context.Context, appauth.Principal, int64, appfleet.LaunchRequest) (domainfleet.Plan, error)
	Launch(context.Context, appauth.Principal, int64, appfleet.LaunchRequest, string) (appfleet.Fleet, error)
}

// publicDebrisGrace is deliberately real server time. Universe speed must not
// turn artificial players into permanent galaxy-page watchers.
const publicDebrisGrace = 20 * time.Minute

// campaign runs the operational layer: look, then strike, or put the fleet out
// of reach before the night.
func (b *Brain) campaign(ctx context.Context, principal appauth.Principal, profile domainai.Profile,
	planets []appeconomy.Planet, observations []observation, team friends, plan mission) []domainai.Decision {
	if b.Fleet == nil {
		return []domainai.Decision{domainai.Skip(domainai.Operational, "campaign", "no fleet service")}
	}
	home := planets[0]
	overview, err := b.Fleet.Overview(ctx, principal, home.ID)
	if err != nil {
		return []domainai.Decision{failure(domainai.Operational, "campaign", err)}
	}
	now := b.Clock.Now().UTC()
	if profile.Window.LastBefore(now, profile.Interval) {
		return []domainai.Decision{b.fleetsave(ctx, principal, profile, planets, overview)}
	}
	if decision, sent := b.expand(ctx, principal, profile, planets, overview); sent {
		return []domainai.Decision{decision}
	}
	// What the alliance asks comes before what one would do alone, as often as
	// the universe asked its members to act as one. A member sitting this call
	// out still leaves the target of the alliance alone: not marching is not the
	// same as getting in the way.
	if domainai.Cooperates(profile.Seed, profile.Tick, home.Rules.AI.Coordination) {
		if decision, taken := b.serve(ctx, principal, profile, home, overview, plan); taken {
			return []domainai.Decision{decision}
		}
	}
	targets, memories := b.survey(profile, home, observations)
	if len(memories) > 0 && b.Thinking != nil {
		if err := b.Thinking.Remember(ctx, profile.PlayerID, memories); err != nil {
			return []domainai.Decision{failure(domainai.Operational, "remember", err)}
		}
	}
	raids := b.recentRaids(ctx, principal)
	observations = invalidateRaidedIntelligence(observations, raids, now, profile.RaidCooldown())
	targets, _ = b.survey(profile, home, observations)
	targets = targetsOffCooldown(targets, raids, now, profile.RaidCooldown())
	best, stale, found := domainai.BestTarget(reserve(targets, plan.reserved), profile.Preferences())
	behaviour := profile.Behaviour()
	if found && behaviour.AttackEnabled {
		return []domainai.Decision{b.raid(ctx, principal, profile, home, overview, best)}
	}
	if behaviour.RecycleEnabled {
		if decision, sent := b.recycle(ctx, principal, profile, home, overview); sent {
			return []domainai.Decision{decision}
		}
	}
	if found && !behaviour.AttackEnabled {
		decision := skip(domainai.Operational, "raid "+best.Coordinate.String(), "attacks are disabled for this character", home.ID)
		coordinate := best.Coordinate
		decision.Target = &coordinate
		return []domainai.Decision{decision}
	}
	if !behaviour.EspionageEnabled {
		return []domainai.Decision{skip(domainai.Operational, "spy", "espionage is disabled for this character", home.ID)}
	}
	return []domainai.Decision{b.spy(ctx, principal, profile, home, overview, stale, observations, team)}
}

func (b *Brain) recentRaids(ctx context.Context, principal appauth.Principal) map[universe.Coordinate]time.Time {
	recent := map[universe.Coordinate]time.Time{}
	if b.Reports == nil {
		return recent
	}
	summaries, err := b.Reports.List(ctx, principal, appreports.Filter{Kind: report.CombatAttack, Page: 1})
	if err != nil {
		return recent
	}
	for _, summary := range summaries {
		if previous, found := recent[summary.Coordinate]; !found || summary.OccurredAt.After(previous) {
			recent[summary.Coordinate] = summary.OccurredAt
		}
	}
	return recent
}

func invalidateRaidedIntelligence(observations []observation,
	raids map[universe.Coordinate]time.Time, now time.Time, cooldown time.Duration) []observation {
	updated := make([]observation, len(observations))
	copy(updated, observations)
	for index := range updated {
		if raidedAt, found := raids[updated[index].intel.Coordinate]; found &&
			raidedAt.After(updated[index].intel.ObservedAt) && now.Sub(raidedAt) >= cooldown {
			updated[index].intel.Complete = false
			updated[index].payload.Probes = 0
		}
	}
	return updated
}

func targetsOffCooldown(targets []domainai.Target, raids map[universe.Coordinate]time.Time,
	now time.Time, cooldown time.Duration) []domainai.Target {
	available := make([]domainai.Target, 0, len(targets))
	for _, target := range targets {
		if raidedAt, found := raids[target.Coordinate]; found && now.Sub(raidedAt) >= 0 && now.Sub(raidedAt) < cooldown {
			continue
		}
		available = append(available, target)
	}
	return available
}

// expand founds the next ordinary colony when astrophysics, a colony ship and
// a fleet slot all say the empire is ready. It discovers the empty position
// through the same public galaxy view a human uses.
func (b *Brain) expand(ctx context.Context, principal appauth.Principal, profile domainai.Profile,
	planets []appeconomy.Planet, homeOverview appfleet.Overview) (domainai.Decision, bool) {
	if b.Galaxy == nil || len(planets) == 0 {
		return domainai.Decision{}, false
	}
	planetCount := 0
	for _, planet := range planets {
		if planet.Kind == building.OnPlanet {
			planetCount++
		}
	}
	home := planets[0]
	if planetCount-1 >= home.Researches.ColonySlots(home.Rules.Progression.MaximumColonies) {
		return domainai.Decision{}, false
	}
	for _, fleet := range homeOverview.Fleets {
		if fleet.Mission == domainfleet.MissionColonize {
			return skip(domainai.Strategic, "colonize", "a colony ship is already underway", home.ID), true
		}
	}
	for _, origin := range planets {
		if origin.Kind != building.OnPlanet {
			continue
		}
		overview := homeOverview
		if origin.ID != home.ID {
			var err error
			overview, err = b.Fleet.Overview(ctx, principal, origin.ID)
			if err != nil {
				continue
			}
		}
		if overview.Stationed[unit.ColonyShip] <= 0 {
			continue
		}
		target, found := b.emptyPosition(ctx, principal, profile, origin)
		if !found {
			return skip(domainai.Strategic, "colonize", "no empty position in exploration range", origin.ID), true
		}
		request := appfleet.LaunchRequest{
			Target: target, TargetKind: domainfleet.TargetEmpty, Mission: domainfleet.MissionColonize,
			Composition: domainfleet.Composition{unit.ColonyShip: 1}, Percent: 100,
		}
		if _, err := b.Fleet.Launch(ctx, principal, origin.ID, request,
			commandKey(profile, "colonize", target.String())); err != nil {
			return failure(domainai.Strategic, "colonize "+target.String(), err), true
		}
		coordinate := target
		return domainai.Decision{
			Layer: domainai.Strategic, Action: "colonize " + target.String(), Outcome: domainai.Done,
			Reason: "astrophysics opened a colony slot", BodyID: origin.ID, Target: &coordinate,
		}, true
	}
	return domainai.Decision{}, false
}

func (b *Brain) emptyPosition(ctx context.Context, principal appauth.Principal, profile domainai.Profile,
	origin appeconomy.Planet) (universe.Coordinate, bool) {
	radius := max(2, min(profile.Behaviour().SearchRadius, origin.Rules.Topology.SystemsPerGalaxy))
	var candidates []universe.Coordinate
	seen := map[int]bool{}
	for distance := 0; distance < radius; distance++ {
		for _, offset := range symmetricOffsets(distance) {
			system := wrappedSystem(origin, offset)
			if system == 0 || seen[system] {
				continue
			}
			seen[system] = true
			view, err := b.Galaxy.System(ctx, principal, origin.Coordinate.Galaxy, system)
			if err != nil {
				continue
			}
			for _, row := range view.Rows {
				if row.PlanetID == 0 {
					candidates = append(candidates, universe.Coordinate{
						Galaxy: view.Galaxy, System: view.System, Position: row.Position,
					})
				}
			}
		}
	}
	if len(candidates) == 0 {
		return universe.Coordinate{}, false
	}
	source := random.NewSeeded(uint64(profile.Seed) ^ uint64(profile.Tick) ^ 0x434f4c4f4e59)
	return candidates[source.IntN(len(candidates))], true
}

func symmetricOffsets(distance int) []int {
	if distance == 0 {
		return []int{0}
	}
	return []int{distance, -distance}
}

func wrappedSystem(origin appeconomy.Planet, offset int) int {
	system := origin.Coordinate.System + offset
	if origin.Rules.Topology.CircularSystems {
		systems := origin.Rules.Topology.SystemsPerGalaxy
		return ((system-1)%systems+systems)%systems + 1
	}
	if system < 1 || system > origin.Rules.Topology.SystemsPerGalaxy {
		return 0
	}
	return system
}

// observation is one report of the player, read once and used by everything
// that follows: what it decides alone, and what it tells its allies.
type observation struct {
	summary appreports.Summary
	payload report.EspionagePayload
	intel   domainai.Intel
}

// friends are the players an artificial one never looks at and never strikes:
// itself and everybody of its own alliance.
type friends struct {
	names       map[string]bool
	coordinates map[universe.Coordinate]bool
}

// covers reports whether a name or a position belongs to the team.
func (f friends) covers(name string, at universe.Coordinate) bool {
	return f.names[name] || f.coordinates[at]
}

// observe reads the espionage reports of the player and turns them into what
// the planner reasons about. Nothing else feeds this: no report, no opinion.
func (b *Brain) observe(ctx context.Context, principal appauth.Principal,
	home appeconomy.Planet, team friends) []observation {
	if b.Reports == nil {
		return nil
	}
	summaries, err := b.Reports.List(ctx, principal, appreports.Filter{Kind: report.Espionage, Page: 1})
	if err != nil {
		return nil
	}
	seen := map[universe.Coordinate]bool{}
	var observations []observation
	for _, summary := range summaries {
		if !summary.Own || seen[summary.Coordinate] {
			continue
		}
		detail, err := b.Reports.Get(ctx, principal, summary.ID)
		if err != nil {
			continue
		}
		payload, ok := detail.Payload.(report.EspionagePayload)
		if !ok {
			continue
		}
		seen[summary.Coordinate] = true
		if team.covers(payload.TargetPlayerName, summary.Coordinate) {
			// One does not raid one's own alliance, so one does not weigh it.
			continue
		}
		observations = append(observations, observation{
			summary: summary, payload: payload, intel: b.intelOf(payload, summary, home),
		})
	}
	return observations
}

// survey grades what the reports say for the character of the player.
func (b *Brain) survey(profile domainai.Profile, home appeconomy.Planet,
	observations []observation) ([]domainai.Target, []appai.Memory) {
	now := b.Clock.Now().UTC()
	recent := time.Duration(home.Rules.Espionage.RecentReportSeconds) * time.Second
	var targets []domainai.Target
	var memories []appai.Memory
	for _, seen := range observations {
		target := domainai.ScoreTarget(seen.intel, now, recent, profile.Preferences())
		targets = append(targets, target)
		memories = append(memories, appai.Memory{
			Kind: "target", Coordinate: seen.intel.Coordinate, ObservedAt: seen.intel.ObservedAt,
			Score: target.Score,
			Summary: fmt.Sprintf("%s, butin %d, défense %d",
				seen.payload.TargetPlayerName, plunderOf(seen.intel.Plunder), seen.intel.Defence),
		})
	}
	return targets, memories
}

// intelOf turns one espionage report into what the planner reasons about. The
// plunder is what the rules of this universe allow, applied to what was seen.
func (b *Brain) intelOf(payload report.EspionagePayload, summary appreports.Summary,
	home appeconomy.Planet) domainai.Intel {
	intel := domainai.Intel{
		Coordinate: summary.Coordinate, ReportID: summary.ID, OwnerName: payload.TargetPlayerName,
		ObservedAt: summary.OccurredAt,
		// A report is complete when its level reached the rank that reveals the
		// defences: an empty section then really means an empty planet.
		Complete: payload.Level >= home.Rules.Espionage.DefensesThreshold,
	}
	if payload.Resources != nil {
		ratio := home.Rules.Economy.PillageRatio
		intel.Plunder = domaineconomy.Resources{
			Metal:     int64(float64(payload.Resources.Metal) * ratio),
			Crystal:   int64(float64(payload.Resources.Crystal) * ratio),
			Deuterium: int64(float64(payload.Resources.Deuterium) * ratio),
		}
	}
	intel.Debris = b.expectedDebris(payload, home)
	intel.Defence = domainai.Strength(inventoryOf(payload.Fleet), b.Catalogues.Units) +
		domainai.Strength(inventoryOf(payload.Defenses), b.Catalogues.Units)
	if distance, err := domainfleet.Distance(home.Coordinate, summary.Coordinate, home.Rules.Topology); err == nil {
		intel.Distance = distance
	}
	return intel
}

// expectedDebris prices only units revealed by the report, at the configured
// production cost and debris ratios. It deliberately assumes every revealed
// unit could be lost: combat, not the planner, decides the actual wreckage.
func (b *Brain) expectedDebris(payload report.EspionagePayload, home appeconomy.Planet) domaineconomy.Resources {
	ships := b.wreckageValue(inventoryOf(payload.Fleet), home, home.Rules.Combat.ShipsToDebris)
	defenses := b.wreckageValue(inventoryOf(payload.Defenses), home, home.Rules.Combat.DefensesToDebris)
	return ships.Plus(defenses)
}

func (b *Brain) wreckageValue(inventory map[unit.ID]int64, home appeconomy.Planet,
	ratio float64) domaineconomy.Resources {
	if ratio <= 0 {
		return domaineconomy.Resources{}
	}
	var metal, crystal int64
	for id, quantity := range inventory {
		if quantity <= 0 {
			continue
		}
		cost, err := b.Catalogues.Units.UnitCost(id, home.Rules)
		if err != nil {
			continue
		}
		total, err := unit.TotalCost(cost, quantity)
		if err != nil {
			continue
		}
		metal += total.Metal
		crystal += total.Crystal
	}
	return domaineconomy.Resources{
		Metal: int64(float64(metal) * ratio), Crystal: int64(float64(crystal) * ratio),
	}
}

// spy sends probes at the most promising body it cannot yet judge, or at a
// neighbour it has never looked at.
func (b *Brain) spy(ctx context.Context, principal appauth.Principal, profile domainai.Profile,
	home appeconomy.Planet, overview appfleet.Overview, stale domainai.Target,
	observations []observation, team friends) domainai.Decision {
	probes := overview.Stationed[unit.EspionageProbe]
	if probes <= 0 {
		return skip(domainai.Operational, "spy", "no probe on the ground", home.ID)
	}
	interval := profile.ReconnaissanceInterval()
	now := b.Clock.Now().UTC()
	active := 0
	var latest time.Time
	for _, fleet := range overview.Fleets {
		if fleet.Mission == domainfleet.MissionEspionage {
			active++
			if fleet.DepartedAt.After(latest) {
				latest = fleet.DepartedAt
			}
		}
	}
	if active >= profile.ConcurrentScouts() {
		return skip(domainai.Operational, "spy", "the reconnaissance flight budget is in use", home.ID)
	}
	wanted := profile.Preferences().Probes
	if wanted > probes {
		wanted = probes
	}
	recent := time.Duration(home.Rules.Espionage.RecentReportSeconds) * time.Second
	for _, seen := range observations {
		if seen.intel.ObservedAt.After(latest) {
			latest = seen.intel.ObservedAt
		}
	}
	if !latest.IsZero() {
		age := now.Sub(latest)
		if age >= 0 && age < interval {
			return skip(domainai.Operational, "spy", "the reconnaissance budget is cooling down", home.ID)
		}
	}
	covered := scoutingCoverage(observations, now, recent, wanted)
	target := stale.Coordinate
	if covered[target] {
		target = universe.Coordinate{}
	}
	if target.Galaxy == 0 {
		found, ok := b.neighbour(ctx, principal, profile, home, team, covered)
		if !ok {
			return skip(domainai.Operational, "spy", "nearby bodies are already covered by useful intelligence", home.ID)
		}
		target = found
	}
	request := appfleet.LaunchRequest{
		Target: target, TargetKind: domainfleet.TargetPlanet, Mission: domainfleet.MissionEspionage,
		Composition: domainfleet.Composition{unit.EspionageProbe: wanted}, Percent: 100,
	}
	key := commandKey(profile, "spy", target.String())
	if _, err := b.Fleet.Launch(ctx, principal, home.ID, request, key); err != nil {
		return failure(domainai.Operational, "spy "+target.String(), err)
	}
	decision := domainai.Decision{
		Layer: domainai.Operational, Action: "spy " + target.String(), Outcome: domainai.Done,
		Reason: "a raid needs a fresh and complete report", BodyID: home.ID,
	}
	decision.Target = &target
	return decision
}

// scoutingCoverage marks every body for which another identical mission would
// add no useful information. Complete reports remain useful while their score
// has not fully decayed. A partial report can be retried sooner only when the
// player can now send more probes than it did last time.
func scoutingCoverage(observations []observation, now time.Time, recent time.Duration,
	probes int64) map[universe.Coordinate]bool {
	covered := make(map[universe.Coordinate]bool, len(observations))
	for _, seen := range observations {
		if domainai.Fresh(seen.intel.ObservedAt, now, recent) <= 0 {
			continue
		}
		if seen.intel.Complete || probes <= seen.payload.Probes {
			covered[seen.intel.Coordinate] = true
		}
	}
	return covered
}

// recycle visits the public map like a human would. A newly changed field is
// left alone for a real-time grace period, and a fleet already flying there
// reserves it so one artificial player never races itself.
func (b *Brain) recycle(ctx context.Context, principal appauth.Principal, profile domainai.Profile,
	home appeconomy.Planet, overview appfleet.Overview) (domainai.Decision, bool) {
	if b.Galaxy == nil || overview.Stationed[unit.Recycler] <= 0 {
		return domainai.Decision{}, false
	}
	view, err := b.Galaxy.System(ctx, principal, home.Coordinate.Galaxy, home.Coordinate.System)
	if err != nil {
		return domainai.Decision{}, false
	}
	reserved := make(map[universe.Coordinate]bool)
	for _, fleet := range overview.Fleets {
		if fleet.Mission == domainfleet.MissionRecycle && fleet.State == domainfleet.Outbound {
			reserved[fleet.Target] = true
		}
	}
	now := b.Clock.Now().UTC()
	var field domaineconomy.Resources
	var at universe.Coordinate
	for _, row := range view.Rows {
		if row.Debris == nil {
			continue
		}
		candidateAt := universe.Coordinate{Galaxy: view.Galaxy, System: view.System, Position: row.Position}
		age := now.Sub(row.DebrisUpdatedAt)
		if row.DebrisUpdatedAt.IsZero() || age < publicDebrisGrace || reserved[candidateAt] {
			continue
		}
		candidate := row.Debris.Resources()
		if candidate.Metal+candidate.Crystal <= field.Metal+field.Crystal {
			continue
		}
		field = candidate
		at = candidateAt
	}
	composition, ok := domainai.ComposeRecycling(overview.Stationed, field, home.Rules.Combat.RecyclerCapacity)
	if !ok {
		return domainai.Decision{}, false
	}
	request := appfleet.LaunchRequest{
		Target: at, TargetKind: domainfleet.TargetDebris, Mission: domainfleet.MissionRecycle,
		Composition: domainfleet.Composition(composition), Percent: 100,
	}
	key := commandKey(profile, "recycle", at.String())
	if _, err := b.Fleet.Launch(ctx, principal, home.ID, request, key); err != nil {
		return failure(domainai.Tactical, "recycle "+at.String(), err), true
	}
	coordinate := at
	return domainai.Decision{
		Layer: domainai.Tactical, Action: "recycle " + coordinate.String(), Outcome: domainai.Done,
		Reason: "a public debris field remained available after the observation delay", BodyID: home.ID, Target: &coordinate,
	}, true
}

// neighbour picks the closest body of somebody else within the configured
// exploration radius.
func (b *Brain) neighbour(ctx context.Context, principal appauth.Principal, profile domainai.Profile,
	home appeconomy.Planet, team friends, covered map[universe.Coordinate]bool) (universe.Coordinate, bool) {
	if b.Galaxy == nil {
		return universe.Coordinate{}, false
	}
	radius := min(profile.Behaviour().SearchRadius, home.Rules.Topology.SystemsPerGalaxy)
	seen := map[int]bool{}
	var candidates []universe.Coordinate
	for distance := 0; distance < radius; distance++ {
		offsets := symmetricOffsets(distance)
		if distance > 0 {
			// Half the characters walk the other direction first, keeping nearby
			// targets shared without making every scout choose the same one.
			if (profile.Seed+profile.Tick)&1 != 0 {
				offsets[0], offsets[1] = offsets[1], offsets[0]
			}
		}
		for _, offset := range offsets {
			system := wrappedSystem(home, offset)
			if system == 0 || seen[system] {
				continue
			}
			seen[system] = true
			view, err := b.Galaxy.System(ctx, principal, home.Coordinate.Galaxy, system)
			if err != nil {
				continue
			}
			for _, row := range view.Rows {
				if row.PlanetID == 0 || row.Own {
					continue
				}
				at := universe.Coordinate{Galaxy: home.Coordinate.Galaxy, System: system, Position: row.Position}
				if !team.covers(row.OwnerName, at) && !covered[at] {
					candidates = append(candidates, at)
				}
			}
		}
	}
	if len(candidates) == 0 {
		return universe.Coordinate{}, false
	}
	source := random.NewSeeded(uint64(profile.Seed) ^ uint64(profile.Tick) ^ 0x544152474554)
	return candidates[source.IntN(len(candidates))], true
}

// raid sends the fleet at a target the reports say is worth it.
func (b *Brain) raid(ctx context.Context, principal appauth.Principal, profile domainai.Profile,
	home appeconomy.Planet, overview appfleet.Overview, target domainai.Target) domainai.Decision {
	for _, fleet := range overview.Fleets {
		if fleet.Mission == domainfleet.MissionAttack && fleet.Target == target.Coordinate {
			return skip(domainai.Tactical, "raid "+target.Coordinate.String(),
				"an attack is already underway to this target", home.ID)
		}
	}
	sizing := raidSizing(profile, target.Coordinate)
	composition, ok := domainai.ComposeRaidSized(overview.Stationed, b.Catalogues.Units,
		target.Plunder, target.Defence, profile.Preferences(), sizing)
	if !ok {
		decision := skip(domainai.Tactical, "raid "+target.Coordinate.String(),
			"not clearly stronger than what the report showed", home.ID)
		decision.Score = target.Score
		coordinate := target.Coordinate
		decision.Target = &coordinate
		return decision
	}
	request := appfleet.LaunchRequest{
		Target: target.Coordinate, TargetKind: domainfleet.TargetPlanet, Mission: domainfleet.MissionAttack,
		Composition: domainfleet.Composition(composition), Percent: 100,
	}
	salvage, planned := b.planSalvage(ctx, principal, profile, home, overview, target, request)
	key := commandKey(profile, "raid", target.Coordinate.String())
	attack, err := b.Fleet.Launch(ctx, principal, home.ID, request, key)
	if err != nil {
		return failure(domainai.Tactical, "raid "+target.Coordinate.String(), err)
	}
	reason := raidReason(sizing, "the report is fresh, complete and worth the trip")
	if planned {
		salvageKey := commandKey(profile, "planned-recycle", fmt.Sprintf("%d", attack.ID))
		if _, err := b.Fleet.Launch(ctx, principal, home.ID, salvage, salvageKey); err == nil {
			reason = "the target is worth attacking for its wreckage; recyclers were timed behind the battle"
		} else {
			reason = "the attack left, but its planned recycling convoy could not be launched"
		}
	}
	coordinate := target.Coordinate
	return domainai.Decision{
		Layer: domainai.Tactical, Action: "raid " + coordinate.String(), Outcome: domainai.Done,
		Reason: reason, Score: target.Score,
		BodyID: home.ID, Target: &coordinate,
	}
}

// planSalvage recognizes an actual recycling attack rather than relabelling
// every raid after the fact. The expected wreckage must outweigh the plunder,
// no older public field may already be present, and the convoy must be able to
// arrive no earlier than combat. Its hold never exceeds the wreckage estimate.
func (b *Brain) planSalvage(ctx context.Context, principal appauth.Principal, profile domainai.Profile,
	home appeconomy.Planet, overview appfleet.Overview, target domainai.Target,
	attack appfleet.LaunchRequest) (appfleet.LaunchRequest, bool) {
	capacity := home.Rules.Combat.RecyclerCapacity
	debrisTotal := target.Debris.Metal + target.Debris.Crystal
	plunderTotal := target.Plunder.Metal + target.Plunder.Crystal + target.Plunder.Deuterium
	available := overview.Stationed[unit.Recycler] - attack.Composition[unit.Recycler]
	if !profile.Behaviour().RecycleEnabled || b.Galaxy == nil || capacity <= 0 ||
		debrisTotal < capacity || debrisTotal < plunderTotal || available <= 0 ||
		overview.Slots-overview.Used < 2 {
		return appfleet.LaunchRequest{}, false
	}
	for _, fleet := range overview.Fleets {
		if fleet.Mission == domainfleet.MissionRecycle && fleet.State == domainfleet.Outbound &&
			fleet.Target == target.Coordinate {
			return appfleet.LaunchRequest{}, false
		}
	}
	view, err := b.Galaxy.System(ctx, principal, target.Coordinate.Galaxy, target.Coordinate.System)
	if err != nil || target.Coordinate.Position < 1 || target.Coordinate.Position > len(view.Rows) ||
		view.Rows[target.Coordinate.Position-1].Debris != nil {
		return appfleet.LaunchRequest{}, false
	}
	wanted := debrisTotal / capacity
	if wanted > available {
		wanted = available
	}
	if wanted <= 0 {
		return appfleet.LaunchRequest{}, false
	}
	attackPlan, err := b.Fleet.Preview(ctx, principal, home.ID, attack)
	if err != nil {
		return appfleet.LaunchRequest{}, false
	}
	for _, percent := range []int{100, 90, 80, 70, 60, 50, 40, 30, 20, 10} {
		request := appfleet.LaunchRequest{
			Target: target.Coordinate, TargetKind: domainfleet.TargetDebris, Mission: domainfleet.MissionRecycle,
			Composition: domainfleet.Composition{unit.Recycler: wanted}, Percent: percent,
		}
		plan, err := b.Fleet.Preview(ctx, principal, home.ID, request)
		if err != nil || plan.ArrivesAt.Before(attackPlan.ArrivesAt) {
			continue
		}
		if !overview.Planet.Stock.Covers(attackPlan.Debit.Plus(plan.Debit)) {
			continue
		}
		return request, true
	}
	return appfleet.LaunchRequest{}, false
}

func raidSizing(profile domainai.Profile, at universe.Coordinate) float64 {
	coordinateSeed := uint64(at.Galaxy)*73856093 ^ uint64(at.System)*19349663 ^ uint64(at.Position)*83492791
	source := random.NewSeeded(uint64(profile.Seed) ^ uint64(profile.Tick) ^ coordinateSeed ^ 0x5241494453495a45)
	return domainai.RaidSizingFactor(profile.Difficulty, source)
}

func raidReason(sizing float64, accurate string) string {
	switch {
	case sizing < .75:
		return "a rough estimate made a lean force look sufficient"
	case sizing > 1.05:
		return "a cautious estimate called for extra ships"
	default:
		return accurate
	}
}

// fleetsave puts the fleet and what it can carry out of reach before the night.
func (b *Brain) fleetsave(ctx context.Context, principal appauth.Principal, profile domainai.Profile,
	planets []appeconomy.Planet, overview appfleet.Overview) domainai.Decision {
	home := planets[0]
	if profile.Preferences().Fleetsave == domainai.SaveNever {
		return skip(domainai.Operational, "fleetsave", "this character never runs", home.ID)
	}
	if len(planets) < 2 {
		return skip(domainai.Operational, "fleetsave", "no second body to fly to", home.ID)
	}
	composition := domainai.ComposeFleetsave(overview.Stationed, b.Catalogues.Units)
	if len(composition) == 0 {
		return skip(domainai.Operational, "fleetsave", "no ship to hide", home.ID)
	}
	shelter := farthest(home, planets)
	if shelter.ID == home.ID {
		return skip(domainai.Operational, "fleetsave", "no reachable body to fly to", home.ID)
	}
	request := appfleet.LaunchRequest{
		Target: shelter.Coordinate, TargetKind: domainfleet.TargetPlanet, Mission: domainfleet.MissionTransport,
		Composition: domainfleet.Composition(composition), Percent: 10,
	}
	plan, err := b.Fleet.Preview(ctx, principal, home.ID, request)
	if err != nil {
		return failure(domainai.Operational, "fleetsave", err)
	}
	cargo := domainai.FleetsaveCargoUpTo(overview.Planet.Stock, plan.Capacity)
	if profile.Preferences().Fleetsave == domainai.SaveLoaded &&
		cargo.Metal == 0 && cargo.Crystal == 0 && cargo.Deuterium == 0 {
		return skip(domainai.Operational, "fleetsave", "nothing worth loading", home.ID)
	}
	request.Cargo = cargo
	key := commandKey(profile, "fleetsave", shelter.Coordinate.String())
	if _, err := b.Fleet.Launch(ctx, principal, home.ID, request, key); err != nil {
		return failure(domainai.Operational, "fleetsave", err)
	}
	coordinate := shelter.Coordinate
	return domainai.Decision{
		Layer: domainai.Operational, Action: "fleetsave to " + coordinate.String(), Outcome: domainai.Done,
		Reason: "the night is coming", BodyID: home.ID, Target: &coordinate,
	}
}

// farthest is the body of the player that takes longest to reach, which is
// exactly what a fleet running from the night wants.
func farthest(home appeconomy.Planet, planets []appeconomy.Planet) appeconomy.Planet {
	shelter := home
	var best int64
	for _, planet := range planets {
		if planet.ID == home.ID {
			continue
		}
		distance, err := domainfleet.Distance(home.Coordinate, planet.Coordinate, home.Rules.Topology)
		if err != nil {
			continue
		}
		if distance > best {
			best, shelter = distance, planet
		}
	}
	return shelter
}

func inventoryOf(document map[string]int64) map[unit.ID]int64 {
	units := make(map[unit.ID]int64, len(document))
	for id, quantity := range document {
		units[unit.ID(id)] = quantity
	}
	return units
}

func plunderOf(resources domaineconomy.Resources) int64 {
	return resources.Metal + resources.Crystal + resources.Deuterium
}

// reserve keeps out of a private plan whatever the alliance already has in
// hand: two fleets of the same team do not race each other to the same body.
func reserve(targets []domainai.Target, at universe.Coordinate) []domainai.Target {
	if at.Galaxy == 0 {
		return targets
	}
	kept := make([]domainai.Target, 0, len(targets))
	for _, target := range targets {
		if target.Coordinate != at {
			kept = append(kept, target)
		}
	}
	return kept
}
