package ai

import (
	"context"
	"fmt"
	"time"

	appacs "universeatwar/internal/app/acs"
	appai "universeatwar/internal/app/ai"
	appauth "universeatwar/internal/app/authentication"
	appeconomy "universeatwar/internal/app/economy"
	appfleet "universeatwar/internal/app/fleet"
	domainacs "universeatwar/internal/domain/acs"
	domainai "universeatwar/internal/domain/ai"
	domainfleet "universeatwar/internal/domain/fleet"
	"universeatwar/internal/domain/unit"
	"universeatwar/internal/domain/universe"
)

const (
	// defenceWatch is how long a defender stands over a threatened ally.
	defenceWatch = 3 * time.Hour
	// openingSpeed is the pace of the fleet that opens a grouped operation. It
	// crawls so that the rest of the alliance has time to fall in behind it.
	openingSpeed = 10
)

// Operations is the grouped operation surface of a player, exactly as the
// alliance pages offer it.
type Operations interface {
	Create(context.Context, appauth.Principal, int64, appfleet.LaunchRequest, string) (appacs.Group, error)
	Join(context.Context, appauth.Principal, int64, int64, appfleet.LaunchRequest, string) (appacs.Group, error)
	Group(context.Context, appauth.Principal, int64) (appacs.Group, error)
	Withdraw(context.Context, appauth.Principal, int64, string) error
}

// mission is what the alliance currently asks of one member.
type mission struct {
	alliance  appai.Alliance
	objective domainai.Objective
	beliefs   []domainai.Knowledge
	role      domainai.Role
	// reserved is the position the alliance has in hand: a member never opens
	// its own raid on it.
	reserved universe.Coordinate
	running  bool
	leader   bool
}

// serve is what a member does for its alliance before it does anything for
// itself. It returns false when the alliance asks nothing of it right now.
func (b *Brain) serve(ctx context.Context, principal appauth.Principal, profile domainai.Profile,
	home appeconomy.Planet, overview appfleet.Overview, plan mission) (domainai.Decision, bool) {
	if !plan.running || b.Operations == nil {
		return domainai.Decision{}, false
	}
	switch plan.objective.Kind {
	case domainai.DefenceObjective:
		return b.defend(ctx, principal, profile, home, overview, plan)
	case domainai.RaidObjective:
		return b.strike(ctx, principal, profile, home, overview, plan)
	default:
		return domainai.Decision{}, false
	}
}

// defend sends a fleet to stand over a threatened ally. Every awake member is
// expected to come; one with nothing to send says so and stays home. An
// alliance without the means does not pretend to have them.
func (b *Brain) defend(ctx context.Context, principal appauth.Principal, profile domainai.Profile,
	home appeconomy.Planet, overview appfleet.Overview, plan mission) (domainai.Decision, bool) {
	at := plan.objective.Coordinate
	if at == home.Coordinate {
		return skip(domainai.Operational, "defend "+at.String(),
			"the threatened body is my own", home.ID), true
	}
	composition := domainai.ComposeFleetsave(overview.Stationed, b.Catalogues.Units)
	if len(composition) == 0 {
		return skip(domainai.Operational, "defend "+at.String(), "no ship to send", home.ID), true
	}
	now := b.Clock.Now().UTC()
	request := appfleet.LaunchRequest{
		Target: at, TargetKind: domainfleet.TargetPlanet, Mission: domainfleet.MissionHold,
		Composition: domainfleet.Composition(composition), Percent: 100,
		HoldUntil: now.Add(defenceWatch),
	}
	key := commandKey(profile, "defend", at.String())
	if _, err := b.Fleet.Launch(ctx, principal, home.ID, request, key); err != nil {
		return failure(domainai.Operational, "defend "+at.String(), err), true
	}
	coordinate := at
	return domainai.Decision{
		Layer: domainai.Operational, Action: "defend " + coordinate.String(), Outcome: domainai.Done,
		Reason: "an ally raised the alarm", BodyID: home.ID, Target: &coordinate,
	}, true
}

// strike is the part a member takes in a grouped attack: look first, then
// engage. Only the leader opens the operation; the others join it.
func (b *Brain) strike(ctx context.Context, principal appauth.Principal, profile domainai.Profile,
	home appeconomy.Planet, overview appfleet.Overview, plan mission) (domainai.Decision, bool) {
	at := plan.objective.Coordinate
	if plan.objective.State == domainai.Scouting {
		if plan.role != domainai.ScoutRole {
			return domainai.Decision{}, false
		}
		return b.scoutFor(ctx, principal, profile, home, overview, at), true
	}
	if plan.role != domainai.FleeterRole && !plan.leader {
		return domainai.Decision{}, false
	}
	belief, found := beliefAt(plan.beliefs, at)
	if !found {
		return skip(domainai.Tactical, "raid "+at.String(), "the alliance no longer knows this target", home.ID), true
	}
	composition, ok := domainai.ComposeRaid(overview.Stationed, b.Catalogues.Units,
		belief.Plunder, belief.Defence, profile.Preferences())
	if !ok {
		return skip(domainai.Tactical, "raid "+at.String(),
			"not strong enough for what the alliance saw", home.ID), true
	}
	coordinate := at
	if plan.objective.GroupID == 0 {
		if !plan.leader {
			return skip(domainai.Tactical, "raid "+at.String(), "waiting for the operation to open", home.ID), true
		}
		// The fleet that opens an operation flies slowly on purpose: that is
		// what gives the allies the time to join it.
		opening := appfleet.LaunchRequest{
			Target: at, TargetKind: domainfleet.TargetPlanet, Mission: domainfleet.MissionAttack,
			Composition: domainfleet.Composition(composition), Percent: openingSpeed,
		}
		group, err := b.Operations.Create(ctx, principal, home.ID, opening,
			commandKey(profile, "operation", at.String()))
		if err != nil {
			return failure(domainai.Tactical, "open operation on "+at.String(), err), true
		}
		if err := b.Teamwork.AttachGroup(ctx, plan.objective.ID, group.ID); err != nil {
			return failure(domainai.Tactical, "open operation on "+at.String(), err), true
		}
		return domainai.Decision{
			Layer: domainai.Tactical, Action: "open operation on " + coordinate.String(),
			Outcome: domainai.Done, Reason: "the alliance gathers", BodyID: home.ID, Target: &coordinate,
		}, true
	}
	group, err := b.Operations.Group(ctx, principal, plan.objective.GroupID)
	if err != nil {
		return failure(domainai.Tactical, "join operation on "+at.String(), err), true
	}
	for _, participant := range group.Participants {
		if participant.Own {
			return skip(domainai.Tactical, "join operation on "+at.String(),
				"already engaged in the operation", home.ID), true
		}
	}
	if !group.State.Open() {
		return skip(domainai.Tactical, "join operation on "+at.String(),
			"the operation no longer takes fleets", home.ID), true
	}
	joining := appfleet.LaunchRequest{
		Target: at, TargetKind: domainfleet.TargetPlanet, Mission: domainfleet.MissionAttack,
		Composition: domainfleet.Composition(composition), Percent: 100,
	}
	if _, err := b.Operations.Join(ctx, principal, plan.objective.GroupID, home.ID, joining,
		commandKey(profile, "join", at.String())); err != nil {
		return failure(domainai.Tactical, "join operation on "+at.String(), err), true
	}
	return domainai.Decision{
		Layer: domainai.Tactical, Action: "join operation on " + coordinate.String(),
		Outcome: domainai.Done, Reason: "the alliance gathers", BodyID: home.ID, Target: &coordinate,
	}, true
}

// scoutFor sends the probes of the alliance at the body it wants to know.
func (b *Brain) scoutFor(ctx context.Context, principal appauth.Principal, profile domainai.Profile,
	home appeconomy.Planet, overview appfleet.Overview, at universe.Coordinate) domainai.Decision {
	probes := overview.Stationed[unit.EspionageProbe]
	if probes <= 0 {
		return skip(domainai.Operational, "scout "+at.String(), "no probe on the ground", home.ID)
	}
	wanted := profile.Preferences().Probes
	if wanted > probes {
		wanted = probes
	}
	request := appfleet.LaunchRequest{
		Target: at, TargetKind: domainfleet.TargetPlanet, Mission: domainfleet.MissionEspionage,
		Composition: domainfleet.Composition{unit.EspionageProbe: wanted}, Percent: 100,
	}
	key := commandKey(profile, "scout", at.String())
	if _, err := b.Fleet.Launch(ctx, principal, home.ID, request, key); err != nil {
		return failure(domainai.Operational, "scout "+at.String(), err)
	}
	coordinate := at
	return domainai.Decision{
		Layer: domainai.Operational, Action: "scout " + coordinate.String(), Outcome: domainai.Done,
		Reason: "the alliance needs to see before it strikes", BodyID: home.ID, Target: &coordinate,
	}
}

// superviseOperation is what the leader does with a gathering it has opened:
// close it when it lands, give it up when nobody comes.
func (b *Brain) superviseOperation(ctx context.Context, principal appauth.Principal, profile domainai.Profile,
	objective domainai.Objective, now time.Time) domainai.Decision {
	coordinate := objective.Coordinate
	if objective.GroupID == 0 {
		return domainai.Skip(domainai.Strategic, "operation", "the operation is not open yet")
	}
	group, err := b.Operations.Group(ctx, principal, objective.GroupID)
	if err != nil {
		return failure(domainai.Strategic, "operation", err)
	}
	switch group.State {
	case domainacs.Resolved:
		if err := b.Teamwork.AdvanceObjective(ctx, objective, domainai.Achieved, 0,
			"the operation landed", now); err != nil {
			return failure(domainai.Strategic, "operation", err)
		}
		return domainai.Decision{
			Layer: domainai.Strategic, Action: "close " + coordinate.String(), Outcome: domainai.Done,
			Reason: "the operation landed", Target: &coordinate,
		}
	case domainacs.Cancelled:
		if err := b.Teamwork.AdvanceObjective(ctx, objective, domainai.Abandoned, 0,
			"the operation fell apart", now); err != nil {
			return failure(domainai.Strategic, "operation", err)
		}
		return domainai.Decision{
			Layer: domainai.Strategic, Action: "abandon " + coordinate.String(), Outcome: domainai.Done,
			Reason: "the operation fell apart", Target: &coordinate,
		}
	}
	if len(group.Participants) >= objective.Quorum || group.ArrivesAt.After(now.Add(profile.Interval)) {
		return domainai.Skip(domainai.Strategic, "operation",
			fmt.Sprintf("%d of %d fleets engaged", len(group.Participants), objective.Quorum))
	}
	// The last reflection before the landing, and the team is too thin: the
	// leader pulls its own fleet out rather than throw it away alone.
	for _, participant := range group.Participants {
		if !participant.Own {
			continue
		}
		if err := b.Operations.Withdraw(ctx, principal, participant.FleetID,
			commandKey(profile, "withdraw", coordinate.String())); err != nil {
			return failure(domainai.Strategic, "abandon "+coordinate.String(), err)
		}
	}
	if err := b.Teamwork.AdvanceObjective(ctx, objective, domainai.Abandoned, 0,
		"the quorum was never met", now); err != nil {
		return failure(domainai.Strategic, "operation", err)
	}
	return domainai.Decision{
		Layer: domainai.Strategic, Action: "abandon " + coordinate.String(), Outcome: domainai.Done,
		Reason: fmt.Sprintf("only %d of %d fleets came", len(group.Participants), objective.Quorum),
		Target: &coordinate,
	}
}

// beliefAt finds what the alliance believes about one position.
func beliefAt(beliefs []domainai.Knowledge, at universe.Coordinate) (domainai.Knowledge, bool) {
	for _, belief := range beliefs {
		if belief.Kind == domainai.TargetKnowledge && belief.Coordinate == at {
			return belief, true
		}
	}
	return domainai.Knowledge{}, false
}
