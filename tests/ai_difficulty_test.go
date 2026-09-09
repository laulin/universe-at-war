package tests

import (
	"context"
	"testing"

	domainai "universeatwar/internal/domain/ai"
	"universeatwar/internal/domain/rules"
)

// TestTheDifficultyOfTheUniverseReachesThePlayersThatThink proves the setting is
// no longer inert: what the wizard asked for arrives in the very profiles the
// brain is about to reason with, and changes what they prefer.
func TestTheDifficultyOfTheUniverseReachesThePlayersThatThink(t *testing.T) {
	for _, this := range []struct {
		name string
		want domainai.Difficulty
	}{
		{"easy", domainai.Easy},
		{"normal", domainai.Normal},
		{"hard", domainai.Hard},
	} {
		t.Run(this.name, func(t *testing.T) {
			ctx := context.Background()
			universeWorld := populationWorld(t, ctx, 1, artificialPopulation(func(configured *rules.Ruleset) {
				configured.AI.Total = 3
				configured.AI.AllianceCount = 0
				configured.AI.Difficulty = string(this.want)
			}))
			settle(t, ctx, universeWorld)

			// Everybody owes a reflection right now.
			if _, err := universeWorld.Database.Write().ExecContext(ctx,
				"UPDATE ai_profiles SET due_think_at = next_think_at WHERE state = 'active'"); err != nil {
				t.Fatal(err)
			}
			due, err := universeWorld.Thinking.Due(ctx, 10)
			if err != nil {
				t.Fatalf("Due() error = %v", err)
			}
			if len(due) != 3 {
				t.Fatalf("profiles owing a reflection = %d, want 3", len(due))
			}
			for _, profile := range due {
				if profile.Difficulty != this.want {
					t.Fatalf("%s carries difficulty %q, want %q", profile.Name, profile.Difficulty, this.want)
				}
				if profile.Preferences() != profile.Archetype.Preferences().At(this.want) {
					t.Fatalf("%s does not prefer what its universe asked for", profile.Name)
				}
			}
		})
	}
}

// TestAHarderUniverseStrikesOnThinnerMargins reads the two ends against each
// other on the same character, which is what an administrator changing the
// setting is actually buying.
func TestAHarderUniverseStrikesOnThinnerMargins(t *testing.T) {
	margins := map[domainai.Difficulty]float64{}
	for _, difficulty := range domainai.Difficulties() {
		ctx := context.Background()
		universeWorld := populationWorld(t, ctx, 1, artificialPopulation(func(configured *rules.Ruleset) {
			configured.AI.Total = 1
			configured.AI.AllianceCount = 0
			configured.AI.Difficulty = string(difficulty)
		}))
		settle(t, ctx, universeWorld)
		if _, err := universeWorld.Database.Write().ExecContext(ctx,
			"UPDATE ai_profiles SET due_think_at = next_think_at WHERE state = 'active'"); err != nil {
			t.Fatal(err)
		}
		due, err := universeWorld.Thinking.Due(ctx, 1)
		if err != nil || len(due) != 1 {
			t.Fatalf("Due() = %d, %v", len(due), err)
		}
		margins[difficulty] = due[0].Preferences().SafetyMargin
	}
	if !(margins[domainai.Easy] > margins[domainai.Normal] && margins[domainai.Normal] > margins[domainai.Hard]) {
		t.Fatalf("the safety margins do not fall with the difficulty: %v", margins)
	}
}
