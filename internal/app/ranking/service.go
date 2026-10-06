// Package ranking computes the public standings from the assets players still
// own. One point represents one thousand resources invested, as in the classic
// game: stock and unfinished orders are not points, while ships in flight are.
package ranking

import (
	"context"
	"errors"
	"fmt"
	"math"
	"slices"
	"strings"

	appauth "universeatwar/internal/app/authentication"
	"universeatwar/internal/domain/building"
	"universeatwar/internal/domain/catalogue"
	"universeatwar/internal/domain/economy"
	"universeatwar/internal/domain/research"
	"universeatwar/internal/domain/rules"
	"universeatwar/internal/domain/unit"
)

type Category string

const (
	Total    Category = "total"
	Economy  Category = "economy"
	Research Category = "research"
	Military Category = "military"
)

var (
	ErrForbidden       = errors.New("ranking: authenticated account required")
	ErrUnknownCategory = errors.New("ranking: unknown category")
)

// Level is one completed building or research level. Buildings are deliberately
// not grouped by identifier: two level-5 mines on two planets cost twice as
// much as one and must both contribute.
type Level struct {
	ID    string
	Level int
}

// Quantity is one currently owned unit quantity, whether stationed or flying.
type Quantity struct {
	ID       string
	Quantity int64
}

// PlayerAssets is the public identity and score-bearing inventory of a player.
type PlayerAssets struct {
	PlayerID    int64
	AccountID   int64
	Name        string
	Artificial  bool
	Alliance    string
	AllianceTag string
	Buildings   []Level
	Research    []Level
	Units       []Quantity
}

// Snapshot captures all players under the rules currently active in the
// universe. Ranking is a projection, so it needs no persisted score table.
type Snapshot struct {
	Rules   rules.Ruleset
	Players []PlayerAssets
}

type Repository interface {
	Snapshot(context.Context) (Snapshot, error)
}

// Score exposes all four counters even when the list is sorted by only one.
type Score struct {
	Total    int64
	Economy  int64
	Research int64
	Military int64
}

// Entry is one ranked player.
type Entry struct {
	Position    int
	PlayerID    int64
	Name        string
	Artificial  bool
	Alliance    string
	AllianceTag string
	Own         bool
	Score       Score
	Points      int64
}

type Service struct {
	Repository Repository
	Catalogues catalogue.Set
}

// List returns every player ordered by the requested score, with deterministic
// positions for equal investments.
func (s Service) List(ctx context.Context, principal appauth.Principal, category Category) ([]Entry, error) {
	if s.Repository == nil || s.Catalogues.Version == "" {
		return nil, errors.New("ranking: incomplete service dependencies")
	}
	if principal.AccountID <= 0 || principal.MustChangePassword {
		return nil, ErrForbidden
	}
	if !category.Valid() {
		return nil, ErrUnknownCategory
	}
	snapshot, err := s.Repository.Snapshot(ctx)
	if err != nil {
		return nil, err
	}
	if err := s.Catalogues.Matches(snapshot.Rules); err != nil {
		return nil, err
	}

	type scored struct {
		entry      Entry
		investment investments
	}
	rows := make([]scored, 0, len(snapshot.Players))
	for _, player := range snapshot.Players {
		invested, err := s.investments(player, snapshot.Rules)
		if err != nil {
			return nil, fmt.Errorf("ranking: score player %d: %w", player.PlayerID, err)
		}
		score := Score{
			Total: points(invested.total()), Economy: points(invested.economy),
			Research: points(invested.research), Military: points(invested.military),
		}
		rows = append(rows, scored{
			entry: Entry{
				PlayerID: player.PlayerID, Name: player.Name, Artificial: player.Artificial,
				Alliance: player.Alliance, AllianceTag: player.AllianceTag,
				Own: player.AccountID == principal.AccountID, Score: score,
			},
			investment: invested,
		})
	}
	slices.SortStableFunc(rows, func(left, right scored) int {
		leftValue, rightValue := left.investment.forCategory(category), right.investment.forCategory(category)
		if leftValue != rightValue {
			if leftValue > rightValue {
				return -1
			}
			return 1
		}
		if compared := strings.Compare(strings.ToLower(left.entry.Name), strings.ToLower(right.entry.Name)); compared != 0 {
			return compared
		}
		if left.entry.PlayerID < right.entry.PlayerID {
			return -1
		}
		if left.entry.PlayerID > right.entry.PlayerID {
			return 1
		}
		return 0
	})

	entries := make([]Entry, len(rows))
	for index := range rows {
		rows[index].entry.Position = index + 1
		rows[index].entry.Points = rows[index].entry.Score.forCategory(category)
		entries[index] = rows[index].entry
	}
	return entries, nil
}

func (category Category) Valid() bool {
	switch category {
	case Total, Economy, Research, Military:
		return true
	default:
		return false
	}
}

func (score Score) forCategory(category Category) int64 {
	switch category {
	case Economy:
		return score.Economy
	case Research:
		return score.Research
	case Military:
		return score.Military
	default:
		return score.Total
	}
}

type investments struct {
	economy  int64
	research int64
	military int64
}

func (i investments) total() int64 {
	return i.economy + i.research + i.military
}

func (i investments) forCategory(category Category) int64 {
	switch category {
	case Economy:
		return i.economy
	case Research:
		return i.research
	case Military:
		return i.military
	default:
		return i.total()
	}
}

func (s Service) investments(player PlayerAssets, configured rules.Ruleset) (investments, error) {
	var result investments
	for _, asset := range player.Buildings {
		if asset.Level < 0 {
			return result, errors.New("negative building level")
		}
		for level := 1; level <= asset.Level; level++ {
			cost, err := s.Catalogues.Buildings.Cost(building.ID(asset.ID), level, configured.Progression.BuildingCostMultiplier)
			if err != nil {
				return result, err
			}
			if err := addCost(&result.economy, cost); err != nil {
				return result, err
			}
		}
	}
	for _, asset := range player.Research {
		if asset.Level < 0 {
			return result, errors.New("negative research level")
		}
		for level := 1; level <= asset.Level; level++ {
			cost, _, err := s.Catalogues.Research.Cost(research.ID(asset.ID), level, configured.Progression.ResearchCostMultiplier)
			if err != nil {
				return result, err
			}
			if err := addCost(&result.research, cost); err != nil {
				return result, err
			}
		}
	}
	for _, asset := range player.Units {
		if asset.Quantity < 0 {
			return result, errors.New("negative unit quantity")
		}
		if asset.Quantity == 0 {
			continue
		}
		unitCost, err := s.Catalogues.Units.UnitCost(unit.ID(asset.ID), configured)
		if err != nil {
			return result, err
		}
		cost, err := unit.TotalCost(unitCost, asset.Quantity)
		if err != nil {
			return result, err
		}
		if err := addCost(&result.military, cost); err != nil {
			return result, err
		}
	}
	if result.economy > math.MaxInt64-result.research || result.economy+result.research > math.MaxInt64-result.military {
		return result, errors.New("total investment overflow")
	}
	return result, nil
}

func addCost(total *int64, cost economy.Resources) error {
	if cost.Metal > math.MaxInt64-cost.Crystal || cost.Metal+cost.Crystal > math.MaxInt64-cost.Deuterium {
		return errors.New("resource investment overflow")
	}
	value := cost.Metal + cost.Crystal + cost.Deuterium
	if *total > math.MaxInt64-value {
		return errors.New("score investment overflow")
	}
	*total += value
	return nil
}

func points(investment int64) int64 { return investment / 1000 }
