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
	return domainai.Knowledge{
		Kind: domainai.CapabilityKnowledge, Coordinate: home.Coordinate,
		ObservedAt: now, ExpiresAt: now.Add(recent * domainai.StaleFactor), Confidence: 1,
		Plunder: home.Stock, Defence: capability.GroundDefence, Complete: profile.Window.Awake(now),
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
