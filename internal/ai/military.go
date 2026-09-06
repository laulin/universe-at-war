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
	domaineconomy "universeatwar/internal/domain/economy"
	domainfleet "universeatwar/internal/domain/fleet"
	"universeatwar/internal/domain/report"
	"universeatwar/internal/domain/unit"
	"universeatwar/internal/domain/universe"
)

// Reports is the report shelf of a player: the only place an artificial player
// ever learns anything about somebody else.
type Reports interface {
	List(context.Context, appauth.Principal, appreports.Filter) ([]appreports.Summary, error)
	Get(context.Context, appauth.Principal, int64) (appreports.Detail, error)
}

// Galaxy is the public map, which says who lives where and nothing more.
type Galaxy interface {
	System(context.Context, appauth.Principal, int, int) (appgalaxy.View, error)
}

// Fleet is the fleet page of a player.
type Fleet interface {
	Overview(context.Context, appauth.Principal, int64) (appfleet.Overview, error)
	Launch(context.Context, appauth.Principal, int64, appfleet.LaunchRequest, string) (appfleet.Fleet, error)
}

// campaign runs the operational layer: look, then strike, or put the fleet out
// of reach before the night.
func (b *Brain) campaign(ctx context.Context, principal appauth.Principal, profile domainai.Profile,
	planets []appeconomy.Planet) []domainai.Decision {
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
	targets, memories := b.survey(ctx, principal, profile, home)
	if len(memories) > 0 && b.Thinking != nil {
		if err := b.Thinking.Remember(ctx, profile.PlayerID, memories); err != nil {
			return []domainai.Decision{failure(domainai.Operational, "remember", err)}
		}
	}
	best, stale, found := domainai.BestTarget(targets, profile.Preferences())
	if found {
		return []domainai.Decision{b.raid(ctx, principal, profile, home, overview, best)}
	}
	return []domainai.Decision{b.spy(ctx, principal, profile, home, overview, stale)}
}

// survey reads the espionage reports of the player and grades what they say.
// Nothing else feeds this: no report, no opinion.
func (b *Brain) survey(ctx context.Context, principal appauth.Principal, profile domainai.Profile,
	home appeconomy.Planet) ([]domainai.Target, []appai.Memory) {
	if b.Reports == nil {
		return nil, nil
	}
	summaries, err := b.Reports.List(ctx, principal, appreports.Filter{Kind: report.Espionage, Page: 1})
	if err != nil {
		return nil, nil
	}
	now := b.Clock.Now().UTC()
	recent := time.Duration(home.Rules.Espionage.RecentReportSeconds) * time.Second
	seen := map[universe.Coordinate]bool{}
	var targets []domainai.Target
	var memories []appai.Memory
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
		intel := b.intelOf(payload, summary, home)
		target := domainai.ScoreTarget(intel, now, recent, profile.Preferences())
		targets = append(targets, target)
		memories = append(memories, appai.Memory{
			Kind: "target", Coordinate: intel.Coordinate, ObservedAt: intel.ObservedAt, Score: target.Score,
			Summary: fmt.Sprintf("%s, butin %d, défense %d",
				payload.TargetPlayerName, plunderOf(intel.Plunder), intel.Defence),
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
	intel.Defence = domainai.Strength(inventoryOf(payload.Fleet), b.Catalogues.Units) +
		domainai.Strength(inventoryOf(payload.Defenses), b.Catalogues.Units)
	if distance, err := domainfleet.Distance(home.Coordinate, summary.Coordinate, home.Rules.Topology); err == nil {
		intel.Distance = distance
	}
	return intel
}

// spy sends probes at the most promising body it cannot yet judge, or at a
// neighbour it has never looked at.
func (b *Brain) spy(ctx context.Context, principal appauth.Principal, profile domainai.Profile,
	home appeconomy.Planet, overview appfleet.Overview, stale domainai.Target) domainai.Decision {
	probes := overview.Stationed[unit.EspionageProbe]
	if probes <= 0 {
		return skip(domainai.Operational, "spy", "no probe on the ground", home.ID)
	}
	target := stale.Coordinate
	if target.Galaxy == 0 {
		found, ok := b.neighbour(ctx, principal, home)
		if !ok {
			return skip(domainai.Operational, "spy", "nobody to look at nearby", home.ID)
		}
		target = found
	}
	wanted := profile.Preferences().Probes
	if wanted > probes {
		wanted = probes
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

// neighbour picks the closest body of somebody else in the home system.
func (b *Brain) neighbour(ctx context.Context, principal appauth.Principal, home appeconomy.Planet) (universe.Coordinate, bool) {
	if b.Galaxy == nil {
		return universe.Coordinate{}, false
	}
	view, err := b.Galaxy.System(ctx, principal, home.Coordinate.Galaxy, home.Coordinate.System)
	if err != nil {
		return universe.Coordinate{}, false
	}
	for _, row := range view.Rows {
		if row.PlanetID == 0 || row.Own {
			continue
		}
		return universe.Coordinate{
			Galaxy: home.Coordinate.Galaxy, System: home.Coordinate.System, Position: row.Position,
		}, true
	}
	return universe.Coordinate{}, false
}

// raid sends the fleet at a target the reports say is worth it.
func (b *Brain) raid(ctx context.Context, principal appauth.Principal, profile domainai.Profile,
	home appeconomy.Planet, overview appfleet.Overview, target domainai.Target) domainai.Decision {
	composition, ok := domainai.ComposeRaid(overview.Stationed, b.Catalogues.Units,
		target.Plunder, target.Defence, profile.Preferences())
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
	key := commandKey(profile, "raid", target.Coordinate.String())
	if _, err := b.Fleet.Launch(ctx, principal, home.ID, request, key); err != nil {
		return failure(domainai.Tactical, "raid "+target.Coordinate.String(), err)
	}
	coordinate := target.Coordinate
	return domainai.Decision{
		Layer: domainai.Tactical, Action: "raid " + coordinate.String(), Outcome: domainai.Done,
		Reason: "the report is fresh, complete and worth the trip", Score: target.Score,
		BodyID: home.ID, Target: &coordinate,
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
	request := appfleet.LaunchRequest{
		Target: shelter.Coordinate, TargetKind: domainfleet.TargetPlanet, Mission: domainfleet.MissionTransport,
		Composition: domainfleet.Composition(composition), Percent: 10,
	}
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
