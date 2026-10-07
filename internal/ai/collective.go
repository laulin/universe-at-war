package ai

import (
	"context"
	"fmt"
	"time"

	appai "universeatwar/internal/app/ai"
	appauth "universeatwar/internal/app/authentication"
	appeconomy "universeatwar/internal/app/economy"
	appfleet "universeatwar/internal/app/fleet"
	appreports "universeatwar/internal/app/reports"
	domainai "universeatwar/internal/domain/ai"
	"universeatwar/internal/domain/report"
	"universeatwar/internal/domain/universe"
)

// Teamwork is the common memory of an alliance of artificial players.
type Teamwork interface {
	Alliance(context.Context, int64) (appai.Alliance, bool, error)
	Publish(context.Context, int64, int64, []domainai.Knowledge) error
	Recall(context.Context, int64, time.Time) ([]domainai.Knowledge, error)
	AssignRoles(context.Context, int64, map[int64]domainai.Role, time.Time) error
	Roles(context.Context, int64) (map[int64]domainai.Role, error)
	OpenObjective(context.Context, int64, domainai.Objective) (domainai.Objective, error)
	Objective(context.Context, int64) (domainai.Objective, bool, error)
	AdvanceObjective(context.Context, domainai.Objective, domainai.ObjectiveState, int64, string, time.Time) error
	AttachGroup(context.Context, int64, int64) error
}

// contribute tells the alliance what this member has seen. Reports are shared
// through the ordinary use case, so the sharing stays visible and reversible,
// and every belief published carries the report it came from.
func (b *Brain) contribute(ctx context.Context, principal appauth.Principal, profile domainai.Profile,
	alliance appai.Alliance, home appeconomy.Planet, overview appfleet.Overview,
	observations []observation) domainai.Decision {
	now := b.Clock.Now().UTC()
	recent := time.Duration(home.Rules.Espionage.RecentReportSeconds) * time.Second

	knowledge := []domainai.Knowledge{b.declare(profile, alliance, home, overview, now, recent)}
	shared := 0
	for _, seen := range observations {
		confidence, expires := domainai.Believe(seen.intel.ObservedAt, now, recent)
		if confidence <= 0 || !seen.intel.Complete {
			continue
		}
		if !seen.summary.Shared {
			if err := b.Reports.Share(ctx, principal, seen.summary.ID, true); err != nil {
				// Sharing may be closed in this universe: the belief then stays
				// at home, which is exactly what the rules intend.
				continue
			}
		}
		shared++
		knowledge = append(knowledge, domainai.Knowledge{
			Kind: domainai.TargetKnowledge, Coordinate: seen.intel.Coordinate,
			ObservedAt: seen.intel.ObservedAt, ExpiresAt: expires, Confidence: confidence,
			ReportID: seen.summary.ID, Plunder: seen.intel.Plunder, Defence: seen.intel.Defence,
			Complete: true,
			Summary: fmt.Sprintf("%s, butin %d, défense %d", seen.payload.TargetPlayerName,
				plunderOf(seen.intel.Plunder), seen.intel.Defence),
		})
	}
	knowledge = append(knowledge, b.alarms(ctx, principal, now, recent)...)
	if err := b.Teamwork.Publish(ctx, alliance.ID, profile.PlayerID, knowledge); err != nil {
		return failure(domainai.Operational, "share", err)
	}
	return domainai.Decision{
		Layer: domainai.Operational, Action: fmt.Sprintf("share %d beliefs", len(knowledge)),
		Outcome: domainai.Done, BodyID: home.ID,
		Reason: fmt.Sprintf("%d reports opened to %s", shared, alliance.Tag),
	}
}

// declare is what a member honestly tells its allies about itself.
func (b *Brain) declare(profile domainai.Profile, alliance appai.Alliance, home appeconomy.Planet,
	overview appfleet.Overview, now time.Time, recent time.Duration) domainai.Knowledge {
	capability := domainai.Assess(overview.Stationed, b.Catalogues.Units)
	capability.PlayerID = profile.PlayerID
	capability.PlayerName = profile.Name
	capability.Coordinate = home.Coordinate
	capability.BodyID = home.ID
	capability.Awake = profile.Window.Awake(now)
	return domainai.Knowledge{
		Kind: domainai.CapabilityKnowledge, Coordinate: home.Coordinate,
		ObservedAt: now, ExpiresAt: now.Add(recent * domainai.StaleFactor), Confidence: 1,
		Defence: capability.GroundDefence, Complete: capability.Awake, Capability: capability,
		Summary: fmt.Sprintf("corps %d, sondes %d, recycleurs %d, flotte %d, sol %d",
			home.ID, capability.Probes, capability.Recyclers, capability.WarStrength, capability.GroundDefence),
	}
}

// alarms turns the recent defence reports of a member into a threat its allies
// can act on. A player only ever raises the alarm about itself.
func (b *Brain) alarms(ctx context.Context, principal appauth.Principal, now time.Time,
	recent time.Duration) []domainai.Knowledge {
	if b.Reports == nil {
		return nil
	}
	summaries, err := b.Reports.List(ctx, principal, appreports.Filter{Kind: report.CombatDefense, Page: 1})
	if err != nil {
		return nil
	}
	seen := map[universe.Coordinate]bool{}
	var threats []domainai.Knowledge
	for _, summary := range summaries {
		confidence, expires := domainai.Believe(summary.OccurredAt, now, recent)
		if confidence <= 0 || seen[summary.Coordinate] {
			continue
		}
		seen[summary.Coordinate] = true
		if !summary.Shared {
			if err := b.Reports.Share(ctx, principal, summary.ID, true); err != nil {
				continue
			}
		}
		threats = append(threats, domainai.Knowledge{
			Kind: domainai.ThreatKnowledge, Coordinate: summary.Coordinate,
			ObservedAt: summary.OccurredAt, ExpiresAt: expires, Confidence: confidence,
			ReportID: summary.ID, Summary: "attaqué à " + summary.Coordinate.String(),
		})
	}
	return threats
}

// team resolves the alliance of a player, if it has one.
func (b *Brain) team(ctx context.Context, profile domainai.Profile) (appai.Alliance, bool) {
	if b.Teamwork == nil {
		return appai.Alliance{}, false
	}
	alliance, found, err := b.Teamwork.Alliance(ctx, profile.PlayerID)
	if err != nil || !found {
		return appai.Alliance{}, false
	}
	return alliance, true
}

// lead is what the leader of an alliance does on top of its own reflection: it
// hands out the roles and keeps the single collective plan moving. It reasons
// on the common memory alone.
func (b *Brain) lead(ctx context.Context, principal appauth.Principal, profile domainai.Profile,
	alliance appai.Alliance, beliefs []domainai.Knowledge) []domainai.Decision {
	now := b.Clock.Now().UTC()
	capabilities := declarationsOf(beliefs)
	roles := domainai.AssignRoles(capabilities)
	if err := b.Teamwork.AssignRoles(ctx, alliance.ID, roles, now); err != nil {
		return []domainai.Decision{failure(domainai.Strategic, "assign roles", err)}
	}
	decisions := []domainai.Decision{{
		Layer: domainai.Strategic, Action: "assign roles", Outcome: domainai.Done,
		Reason: fmt.Sprintf("%d members ranked by what they declared", len(roles)),
	}}
	objective, running, err := b.Teamwork.Objective(ctx, alliance.ID)
	if err != nil {
		return append(decisions, failure(domainai.Strategic, "objective", err))
	}
	if running {
		return append(decisions, b.steer(ctx, principal, profile, objective, beliefs, now))
	}
	kind, at, found := domainai.ChooseObjective(beliefs, profile.Preferences(), now)
	if !found {
		return append(decisions, domainai.Skip(domainai.Strategic, "objective",
			"the common memory holds nothing worth a plan"))
	}
	opened, err := b.Teamwork.OpenObjective(ctx, alliance.ID, domainai.Objective{
		Kind: kind, Coordinate: at, Quorum: domainai.Quorum(awakeCount(capabilities)),
		OpenedAt: now, DeadlineAt: now.Add(objectiveWindow * profile.Interval),
		Reason: "chosen from the shared memory",
	})
	if err != nil {
		return append(decisions, failure(domainai.Strategic, "objective", err))
	}
	coordinate := opened.Coordinate
	return append(decisions, domainai.Decision{
		Layer: domainai.Strategic, Action: string(opened.Kind) + " on " + coordinate.String(),
		Outcome: domainai.Done, Reason: "opened for the alliance", Target: &coordinate,
	})
}

// objectiveWindow is how many reflections of the leader a plan is given before
// it is given up.
const objectiveWindow = 6

// steer keeps a running plan honest: it moves to gathering once somebody has
// actually looked, and gives up when the window closes or the reason is gone.
func (b *Brain) steer(ctx context.Context, principal appauth.Principal, profile domainai.Profile,
	objective domainai.Objective, beliefs []domainai.Knowledge, now time.Time) domainai.Decision {
	coordinate := objective.Coordinate
	// Once the fleets are on their way the operation carries its own clock: the
	// window only bounds how long the alliance waits for somebody to look.
	if objective.State == domainai.Assembling {
		if objective.Kind == domainai.DefenceObjective {
			return b.superviseDefence(ctx, objective, now)
		}
		if b.Operations == nil {
			return domainai.Skip(domainai.Strategic, "operation", "no operation service")
		}
		return b.superviseOperation(ctx, principal, profile, objective, now)
	}
	if !now.Before(objective.DeadlineAt) {
		if err := b.Teamwork.AdvanceObjective(ctx, objective, domainai.Abandoned, 0,
			"the window closed", now); err != nil {
			return failure(domainai.Strategic, "objective", err)
		}
		return domainai.Decision{
			Layer: domainai.Strategic, Action: "abandon " + coordinate.String(),
			Outcome: domainai.Done, Reason: "the window closed", Target: &coordinate,
		}
	}
	if !seenProperly(beliefs, objective, now) {
		return domainai.Skip(domainai.Strategic, "objective", "nobody has looked at it yet")
	}
	if err := b.Teamwork.AdvanceObjective(ctx, objective, domainai.Assembling, 0,
		"the intelligence is in", now); err != nil {
		return failure(domainai.Strategic, "objective", err)
	}
	return domainai.Decision{
		Layer: domainai.Strategic, Action: "gather on " + coordinate.String(),
		Outcome: domainai.Done, Reason: "a fresh and complete report was shared", Target: &coordinate,
	}
}

// seenProperly reports whether somebody actually looked at the target of a
// plan and shared what they saw. A defence needs no report: the victim is the
// one raising the alarm.
func seenProperly(beliefs []domainai.Knowledge, objective domainai.Objective, now time.Time) bool {
	if objective.Kind == domainai.DefenceObjective {
		return true
	}
	for _, belief := range beliefs {
		if belief.Kind != domainai.TargetKnowledge || belief.Coordinate != objective.Coordinate {
			continue
		}
		if belief.Complete && belief.Fresh(now) {
			return true
		}
	}
	return false
}

// declarationsOf keeps the declarations out of the common memory.
func declarationsOf(beliefs []domainai.Knowledge) []domainai.Capability {
	var capabilities []domainai.Capability
	for _, belief := range beliefs {
		if belief.Kind == domainai.CapabilityKnowledge && belief.Capability.PlayerID != 0 {
			capabilities = append(capabilities, belief.Capability)
		}
	}
	return capabilities
}

func awakeCount(capabilities []domainai.Capability) int {
	awake := 0
	for _, capability := range capabilities {
		if capability.Awake {
			awake++
		}
	}
	return awake
}

// assignment is what the alliance asks of this member for this reflection.
func (b *Brain) assignment(ctx context.Context, profile domainai.Profile, alliance appai.Alliance,
	beliefs []domainai.Knowledge, allied bool) mission {
	if !allied || b.Teamwork == nil {
		return mission{}
	}
	objective, running, err := b.Teamwork.Objective(ctx, alliance.ID)
	if err != nil || !running {
		return mission{alliance: alliance}
	}
	roles, err := b.Teamwork.Roles(ctx, alliance.ID)
	if err != nil {
		return mission{alliance: alliance}
	}
	leader, hasLeader := alliance.Leader()
	return mission{
		alliance: alliance, objective: objective, beliefs: beliefs, running: true,
		reserved: objective.Coordinate,
		role:     roles[profile.PlayerID],
		leader:   hasLeader && leader.PlayerID == profile.PlayerID,
	}
}
