package ai

import (
	"context"
	"errors"
	"strconv"
	"time"

	appeconomy "universeatwar/internal/app/economy"
	domainai "universeatwar/internal/domain/ai"
	domainclock "universeatwar/internal/domain/clock"
	"universeatwar/internal/domain/rules"
	"universeatwar/internal/domain/universe"
)

// nameAttempts is how many names a birth tries before giving up on its slot. A
// name is only ever refused because somebody already wears it, and the next
// name in the list is free far more often than not.
const nameAttempts = 8

// Population is the standing order of a universe, read through what this build
// can actually run. It is a projection of the ruleset and never the ruleset
// itself: the wizard accepts an end hour of twenty-four that no profile could
// hold, and alliances that the team rules may have closed since.
type Population struct {
	Total         int
	AllianceCount int
	AllianceSize  int
	Window        domainai.Window
	Interval      time.Duration
	Limits        universe.Limits
}

// PopulationFrom reads the standing order out of a ruleset and settles what the
// ruleset allows but this build refuses.
func PopulationFrom(configured rules.Ruleset) Population {
	order := Population{
		Total:         max(0, configured.AI.Total),
		AllianceCount: max(0, configured.AI.AllianceCount),
		AllianceSize:  max(0, configured.AI.AllianceSize),
		// An end hour of twenty-four is the end of the day, which a window
		// spells zero. Refusing it in the ruleset instead would make the active
		// document of a running universe undecodable, and take the universe
		// down with it.
		Window: domainai.Window{
			Start: modulo(configured.AI.ActivityStartHour, 24),
			End:   modulo(configured.AI.ActivityEndHour, 24),
		},
		Interval: max(time.Second, time.Duration(configured.AI.ThinkIntervalSeconds)*time.Second),
		Limits: universe.Limits{
			Galaxies:  configured.Topology.Galaxies,
			Systems:   configured.Topology.SystemsPerGalaxy,
			Positions: configured.Topology.PositionsPerSystem,
		},
	}
	// A universe that closed its alliances would refuse every enrolment, and a
	// team larger than the alliances allow would be refused its last members.
	if !configured.Team.AlliancesEnabled {
		order.AllianceCount = 0
	}
	if maximum := configured.Team.MaximumAllianceSize; maximum > 0 {
		order.AllianceSize = min(order.AllianceSize, maximum)
	}
	if order.AllianceSize == 0 {
		order.AllianceCount = 0
	}
	if order.AllianceCount*order.AllianceSize > order.Total {
		order.AllianceCount = order.Total / order.AllianceSize
	}
	return order
}

// Census is the state the population is reconciled against.
type Census struct {
	Running bool
	Rules   rules.Ruleset
	// Provisioned counts every artificial player this universe has already
	// spent a slot on, the retired ones included. Retiring is a decision an
	// administrator took, and nothing here undoes it.
	Provisioned int
}

// TeamCensus is the strength of one alliance of the population.
type TeamCensus struct {
	Tag     string
	Members int
}

// Unfinished is a birth that stopped between the empire and the character: the
// player owns its world and nothing has ever thought for it.
type Unfinished struct {
	AccountID int64
	Name      string
}

// Censuses reads what the reconciler compares the standing order against.
type Censuses interface {
	Census(context.Context) (Census, error)
	// Unfinished lists births that stopped after their empire was founded.
	Unfinished(context.Context, int) ([]Unfinished, error)
	// Teams reports the strength of the named alliances, one entry per tag and
	// in the order asked for, along with active artificial players belonging to
	// no alliance at all.
	Teams(context.Context, []string, int) ([]TeamCensus, []int64, error)
}

// Populating brings a universe up to the population its ruleset ordered. It
// hooks onto nothing: it compares what was asked for with what is there and
// closes a little of the gap, so it fills a fresh universe and repairs one
// started before any of this existed by exactly the same means.
type Populating struct {
	Clock   domainclock.Clock
	Service Service
	Census  Censuses
}

// Populate creates at most `limit` artificial players and reports how many were
// born. Nought means the universe already holds what it was configured for, or
// that it is not running yet, which is the same silence.
func (p Populating) Populate(ctx context.Context, limit int) (int, error) {
	if limit <= 0 || p.Clock == nil || p.Census == nil {
		return 0, nil
	}
	census, err := p.Census.Census(ctx)
	if err != nil {
		return 0, err
	}
	if !census.Running {
		return 0, nil
	}
	order := PopulationFrom(census.Rules)
	if order.Total <= 0 {
		return 0, nil
	}
	var teams []TeamCensus
	var spares []int64
	if order.AllianceCount > 0 {
		tags := make([]string, order.AllianceCount)
		for index := range tags {
			tags[index] = teamTag(index)
		}
		if teams, spares, err = p.Census.Teams(ctx, tags, limit); err != nil {
			return 0, err
		}
	}
	created := 0
	// A birth that stopped between the empire and the character left a player
	// owning a world that nothing thinks for. Its slot is already spent, so
	// finishing it comes before founding anybody new.
	stalled, err := p.Census.Unfinished(ctx, limit)
	if err != nil {
		return 0, err
	}
	for _, birth := range stalled {
		if created >= limit {
			return created, nil
		}
		if err := p.finish(ctx, order, birth); err != nil {
			return created, err
		}
		created++
	}
	slot := census.Provisioned
	for created < limit {
		team := shortTeam(order, teams)
		// A player already there and belonging to nobody joins before one is
		// born for the purpose, which is what makes an enrolment that failed
		// last pass recoverable without remembering anything.
		if team >= 0 && len(spares) > 0 {
			if err := p.Service.enlist(ctx, spares[0], teamName(team), teams[team].Tag); err != nil {
				return created, err
			}
			teams[team].Members++
			spares = spares[1:]
			continue
		}
		if slot >= order.Total {
			break
		}
		profile, err := p.born(ctx, order, slot)
		if err != nil {
			return created, err
		}
		created, slot = created+1, slot+1
		if team >= 0 {
			if err := p.Service.enlist(ctx, profile.PlayerID, teamName(team), teams[team].Tag); err != nil {
				// The player stays where it is, belonging to nobody. The next
				// pass finds it among the spares and tries again.
				return created, err
			}
			teams[team].Members++
		}
	}
	return created, nil
}

// finish gives a character to a player whose birth stopped once its empire was
// founded. Everything the reflection needs comes from the standing order, and
// the character follows from the account, so the same interrupted birth is
// always finished the same way.
func (p Populating) finish(ctx context.Context, order Population, birth Unfinished) error {
	archetypes := domainai.Archetypes()
	seed, err := p.Service.Seeds.Seed()
	if err != nil {
		return err
	}
	profile := domainai.Profile{
		Name:      birth.Name,
		Archetype: archetypes[int(birth.AccountID)%len(archetypes)],
		Window:    order.Window,
		Interval:  order.Interval,
		Seed:      seed,
	}
	if err := profile.Validate(); err != nil {
		return err
	}
	now := p.Clock.Now().UTC()
	_, err = p.Service.Repository.CreateProfile(ctx, birth.AccountID, profile,
		domainai.NextThink(now, profile.Interval, seed, 0), now)
	return err
}

// born creates the player of one slot. Its name, its character and the corner
// of the map it aims for all follow from the slot alone, so two universes
// configured alike are populated alike.
func (p Populating) born(ctx context.Context, order Population, slot int) (Profile, error) {
	archetypes := domainai.Archetypes()
	for attempt := range nameAttempts {
		profile, err := p.Service.create(ctx, Request{
			Name:      domainai.Name(slot + attempt),
			Archetype: archetypes[slot%len(archetypes)],
			Window:    order.Window,
			Interval:  order.Interval,
			Home:      universe.Spread(order.Limits, slot, order.Total),
		})
		switch {
		case err == nil:
			return profile, nil
		case errors.Is(err, ErrNameTaken), errors.Is(err, appeconomy.ErrNameTaken):
			continue
		default:
			return Profile{}, err
		}
	}
	return Profile{}, ErrNameTaken
}

// shortTeam is the first alliance of the population still missing a member.
func shortTeam(order Population, teams []TeamCensus) int {
	for index := range min(order.AllianceCount, len(teams)) {
		if teams[index].Members < order.AllianceSize {
			return index
		}
	}
	return -1
}

// teamTag and teamName name the nth alliance of the population. Both follow
// from the rank alone, so a pass that runs after a restart addresses the very
// alliances the previous one founded.
func teamTag(index int) string  { return "IA" + strconv.Itoa(index+1) }
func teamName(index int) string { return "Coalition IA " + strconv.Itoa(index+1) }

func modulo(value, base int) int {
	remainder := value % base
	if remainder < 0 {
		remainder += base
	}
	return remainder
}
