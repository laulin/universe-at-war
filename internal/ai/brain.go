// Package ai drives the server-controlled players. It reaches the world only
// through the very use cases a human goes through, which is why it imports no
// repository and can read nobody else's truth.
package ai

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	appai "universeatwar/internal/app/ai"
	appauth "universeatwar/internal/app/authentication"
	appeconomy "universeatwar/internal/app/economy"
	appresearch "universeatwar/internal/app/research"
	appshipyard "universeatwar/internal/app/shipyard"
	domainai "universeatwar/internal/domain/ai"
	"universeatwar/internal/domain/building"
	"universeatwar/internal/domain/catalogue"
	domainclock "universeatwar/internal/domain/clock"
	"universeatwar/internal/domain/random"
	"universeatwar/internal/domain/research"
	"universeatwar/internal/domain/unit"
	"universeatwar/internal/domain/universe"
)

// Thinking hands out the players that owe a reflection and takes their
// conclusions back.
type Thinking interface {
	Due(context.Context, int) ([]domainai.Profile, error)
	Complete(context.Context, int64, []domainai.Decision) error
	Remember(context.Context, int64, []appai.Memory) error
}

// Economy is the empire page of a player.
type Economy interface {
	Planets(context.Context, appauth.Principal) ([]appeconomy.Planet, error)
	Buildings(context.Context, appauth.Principal, int64) (appeconomy.Planet, []appeconomy.BuildingChoice, error)
	EnqueueBuilding(context.Context, appauth.Principal, int64, building.ID, string) (appeconomy.Queue, error)
}

// Research is the laboratory page of a player.
type Research interface {
	Overview(context.Context, appauth.Principal, int64) (appresearch.Overview, error)
	EnqueueResearch(context.Context, appauth.Principal, int64, research.ID, string) (appresearch.Queue, error)
}

// Shipyard is the yard and the defence page of a player.
type Shipyard interface {
	Ships(context.Context, appauth.Principal, int64) (appshipyard.Overview, error)
	Defenses(context.Context, appauth.Principal, int64) (appshipyard.Overview, error)
	OrderFamily(context.Context, appauth.Principal, int64, unit.ID, unit.Family, int64, string) (appshipyard.Order, error)
}

// Brain runs the reflections of the artificial players.
type Brain struct {
	Clock      domainclock.Clock
	Thinking   Thinking
	Economy    Economy
	Research   Research
	Shipyard   Shipyard
	Fleet      Fleet
	Reports    Reports
	Galaxy     Galaxy
	Teamwork   Teamwork
	Operations Operations
	Catalogues catalogue.Set
	Logger     *slog.Logger
}

// ThinkDue runs one reflection for each artificial player that owes one, at
// most `limit` of them, and reports how many thought.
func (b *Brain) ThinkDue(ctx context.Context, limit int) (int, error) {
	if b.Thinking == nil {
		return 0, nil
	}
	if b.Clock == nil || b.Economy == nil {
		return 0, errors.New("ai: incomplete brain dependencies")
	}
	profiles, err := b.Thinking.Due(ctx, limit)
	if err != nil {
		return 0, err
	}
	thought := 0
	for _, profile := range profiles {
		decisions := b.think(ctx, profile)
		if err := b.Thinking.Complete(ctx, profile.PlayerID, decisions); err != nil {
			return thought, err
		}
		thought++
	}
	return thought, nil
}

// think runs the three layers of one reflection. Each layer engages at most one
// action, and every refusal of the game is recorded rather than retried.
func (b *Brain) think(ctx context.Context, profile domainai.Profile) []domainai.Decision {
	principal := appauth.Principal{AccountID: profile.AccountID, Roles: []appauth.Role{appauth.RolePlayer}}
	planets, err := b.Economy.Planets(ctx, principal)
	if err != nil {
		return []domainai.Decision{failure(domainai.Strategic, "survey", err)}
	}
	if len(planets) == 0 {
		return []domainai.Decision{domainai.Skip(domainai.Strategic, "survey", "no body to act from")}
	}
	home := planets[0]
	decisions := []domainai.Decision{b.build(ctx, principal, profile, home.ID)}
	decisions = append(decisions, b.research(ctx, principal, profile, home.ID))
	decisions = append(decisions, b.produce(ctx, principal, profile, home.ID))
	team := friends{names: map[string]bool{}, coordinates: map[universe.Coordinate]bool{}}
	alliance, allied := b.team(ctx, profile)
	var beliefs []domainai.Knowledge
	if allied {
		for _, member := range alliance.Members {
			team.names[member.Name] = true
		}
		recalled, err := b.Teamwork.Recall(ctx, alliance.ID, b.Clock.Now().UTC())
		if err != nil {
			decisions = append(decisions, failure(domainai.Operational, "recall", err))
		}
		beliefs = recalled
		for _, belief := range beliefs {
			if belief.Kind == domainai.CapabilityKnowledge {
				team.coordinates[belief.Coordinate] = true
			}
		}
	}
	observations := b.observe(ctx, principal, home, team)
	if allied && b.Fleet != nil && b.Reports != nil {
		if overview, err := b.Fleet.Overview(ctx, principal, home.ID); err == nil {
			decisions = append(decisions,
				b.contribute(ctx, principal, profile, alliance, home, overview, observations))
			// The declaration of this very reflection is part of what the
			// leader now reasons on.
			if refreshed, err := b.Teamwork.Recall(ctx, alliance.ID, b.Clock.Now().UTC()); err == nil {
				beliefs = refreshed
			}
		}
		if leader, ok := alliance.Leader(); ok && leader.PlayerID == profile.PlayerID {
			decisions = append(decisions, b.lead(ctx, principal, profile, alliance, beliefs)...)
		}
	}
	plan := b.assignment(ctx, profile, alliance, beliefs, allied)
	return append(decisions, b.campaign(ctx, principal, profile, planets, observations, team, plan)...)
}

// build raises the one building the body wants most and can pay for.
func (b *Brain) build(ctx context.Context, principal appauth.Principal, profile domainai.Profile, bodyID int64) domainai.Decision {
	planet, choices, err := b.Economy.Buildings(ctx, principal, bodyID)
	if err != nil {
		return failure(domainai.Strategic, "build", err)
	}
	// An artificial player orders one level at a time and waits for it: queues
	// are a convenience offered to human players, not a way to spend faster.
	if len(planet.Queue) > 0 {
		return skip(domainai.Strategic, "build", "the site is already busy", bodyID)
	}
	body := bodyOf(planet, choices)
	chosen, blocked := domainai.Pick(body.Options, domainai.BuildingPriorities(body))
	if chosen == "" {
		reason := "nothing worth building"
		if blocked != "" {
			reason = "cannot afford " + blocked
		}
		return skip(domainai.Strategic, "build", reason, bodyID)
	}
	key := commandKey(profile, "build", chosen)
	if _, err := b.Economy.EnqueueBuilding(ctx, principal, bodyID, building.ID(chosen), key); err != nil {
		return failure(domainai.Strategic, "build "+chosen, err)
	}
	return domainai.Decision{
		Layer: domainai.Strategic, Action: "build " + chosen, Outcome: domainai.Done,
		Reason: buildReason(blocked), BodyID: bodyID,
	}
}

func buildReason(blocked string) string {
	if blocked == "" {
		return "next on the priority list"
	}
	return "while saving for " + blocked
}

// research follows the fixed line of the archetype-independent technology plan.
func (b *Brain) research(ctx context.Context, principal appauth.Principal, profile domainai.Profile, bodyID int64) domainai.Decision {
	if b.Research == nil {
		return domainai.Skip(domainai.Strategic, "research", "no laboratory service")
	}
	overview, err := b.Research.Overview(ctx, principal, bodyID)
	if err != nil {
		return failure(domainai.Strategic, "research", err)
	}
	// One research at a time, as before: the queue is a convenience offered to
	// human players, not a way for an artificial one to spend faster.
	if len(overview.Queue) > 0 {
		return skip(domainai.Strategic, "research", "a research is already running", bodyID)
	}
	options := make(map[string]domainai.Option, len(overview.Choices))
	for _, choice := range overview.Choices {
		options[string(choice.Definition.ID)] = domainai.Option{
			ID: string(choice.Definition.ID), Level: choice.Level, Cost: choice.Plan.Cost,
			Available: choice.Available, Affordable: choice.Affordable,
		}
	}
	chosen, blocked := domainai.Pick(options, domainai.ResearchPriorities())
	if chosen == "" {
		reason := "nothing to learn yet"
		if blocked != "" {
			reason = "cannot afford " + blocked
		}
		return skip(domainai.Strategic, "research", reason, bodyID)
	}
	key := commandKey(profile, "research", chosen)
	if _, err := b.Research.EnqueueResearch(ctx, principal, bodyID, research.ID(chosen), key); err != nil {
		return failure(domainai.Strategic, "research "+chosen, err)
	}
	return domainai.Decision{
		Layer: domainai.Strategic, Action: "research " + chosen, Outcome: domainai.Done,
		Reason: "next on the technology line", BodyID: bodyID,
	}
}

// produce commits part of what is left to the yard, ships or defences
// depending on the character.
func (b *Brain) produce(ctx context.Context, principal appauth.Principal, profile domainai.Profile, bodyID int64) domainai.Decision {
	if b.Shipyard == nil {
		return domainai.Skip(domainai.Tactical, "produce", "no yard service")
	}
	ships, err := b.Shipyard.Ships(ctx, principal, bodyID)
	if err != nil {
		return failure(domainai.Tactical, "produce", err)
	}
	if ships.Active != nil {
		return skip(domainai.Tactical, "produce", "the yard is already busy", bodyID)
	}
	defenses, err := b.Shipyard.Defenses(ctx, principal, bodyID)
	if err != nil {
		return failure(domainai.Tactical, "produce", err)
	}
	options := map[string]domainai.Option{}
	families := map[string]unit.Family{}
	for _, overview := range []appshipyard.Overview{ships, defenses} {
		for _, choice := range overview.Choices {
			id := string(choice.Definition.ID)
			options[id] = domainai.Option{
				ID: id, Owned: choice.Owned, Cost: choice.UnitCost,
				Available: choice.Available, Affordable: choice.MaximumAffordable > 0,
				Capacity: choice.MaximumAffordable,
			}
			families[id] = overview.Family
		}
	}
	wanted := domainai.ProductionPriorities(profile.Preferences(),
		random.NewSeeded(uint64(profile.Seed)^uint64(profile.Tick)))
	chosen, blocked := domainai.Pick(options, wanted)
	if chosen == "" {
		reason := "nothing worth producing"
		if blocked != "" {
			reason = "cannot afford " + blocked
		}
		return skip(domainai.Tactical, "produce", reason, bodyID)
	}
	quantity := domainai.OrderSize(options[chosen].Capacity)
	key := commandKey(profile, "produce", fmt.Sprintf("%s:%d", chosen, quantity))
	if _, err := b.Shipyard.OrderFamily(ctx, principal, bodyID, unit.ID(chosen), families[chosen], quantity, key); err != nil {
		return failure(domainai.Tactical, "produce "+chosen, err)
	}
	return domainai.Decision{
		Layer: domainai.Tactical, Action: fmt.Sprintf("produce %d %s", quantity, chosen),
		Outcome: domainai.Done, Reason: "keeps the yard busy", BodyID: bodyID,
	}
}

// bodyOf turns the pages of a planet into what the planner reasons about.
func bodyOf(planet appeconomy.Planet, choices []appeconomy.BuildingChoice) domainai.Body {
	body := domainai.Body{
		ID: planet.ID, Coordinate: planet.Coordinate,
		FreeFields: planet.TotalFields - planet.UsedFields,
		Busy:       len(planet.Queue) > 0,
		Stock:      planet.Stock, Capacity: planet.Capacity, Energy: planet.Energy,
		Levels:  make(map[string]int, len(planet.Levels)),
		Options: make(map[string]domainai.Option, len(choices)),
	}
	for id, level := range planet.Levels {
		body.Levels[string(id)] = level
	}
	for _, choice := range choices {
		body.Options[string(choice.Definition.ID)] = domainai.Option{
			ID: string(choice.Definition.ID), Level: choice.Level, Cost: choice.Plan.Cost,
			Available: choice.Available, Affordable: choice.Affordable,
		}
	}
	return body
}

// commandKey makes every command of a reflection unique and replayable: the
// same tick asking twice for the same thing gets the same answer.
func commandKey(profile domainai.Profile, kind, subject string) string {
	return fmt.Sprintf("ai:%d:%d:%s:%s", profile.PlayerID, profile.Tick, kind, subject)
}

func skip(layer domainai.Layer, action, reason string, bodyID int64) domainai.Decision {
	decision := domainai.Skip(layer, action, reason)
	decision.BodyID = bodyID
	return decision
}

func failure(layer domainai.Layer, action string, err error) domainai.Decision {
	return domainai.Decision{
		Layer: layer, Action: action, Outcome: domainai.Failed, Reason: err.Error(),
	}
}
