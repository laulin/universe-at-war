package tests

import (
	"context"
	"testing"

	appauth "universeatwar/internal/app/authentication"
	domainalliance "universeatwar/internal/domain/alliance"
	"universeatwar/internal/domain/rules"
)

// declaredBy reads what one alliance has declared about another.
func declaredBy(t *testing.T, ctx context.Context, universeWorld *world, from, about string) string {
	t.Helper()
	var relation string
	err := universeWorld.Database.Read().QueryRowContext(ctx, `
		SELECT r.relation FROM alliance_relations r
		JOIN alliances mine ON mine.id = r.alliance_id
		JOIN alliances other ON other.id = r.other_alliance_id
		WHERE mine.tag = ? AND other.tag = ?
	`, from, about).Scan(&relation)
	if err != nil {
		return ""
	}
	return relation
}

// humanAllianceDeclaring gives Alice an alliance of her own and has her declare
// something about the machines.
func humanAllianceDeclaring(t *testing.T, ctx context.Context, universeWorld *world,
	relation domainalliance.Relation) appauth.Principal {
	t.Helper()
	alice := appauth.Principal{AccountID: 1}
	if _, err := universeWorld.Alliance.Create(ctx, alice, "Les Humains", "HUM", ""); err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if err := universeWorld.Alliance.Declare(ctx, alice, "MCH", relation); err != nil {
		t.Fatalf("Declare() error = %v", err)
	}
	return alice
}

// TestAnArtificialAllianceAnswersWhatIsDeclaredAboutIt proves the rule of
// diplomacy is no longer inert, and that answering rests on nothing but what was
// announced: the machines are told of a war and they answer one.
func TestAnArtificialAllianceAnswersWhatIsDeclaredAboutIt(t *testing.T) {
	ctx := context.Background()
	database, universeWorld, _, _ := alliedArtificials(t)
	setRules(t, ctx, database, func(configured *rules.Ruleset) { configured.AI.Diplomacy = "dynamic" })
	humanAllianceDeclaring(t, ctx, universeWorld, domainalliance.War)

	think(t, ctx, universeWorld)

	if got := declaredBy(t, ctx, universeWorld, "MCH", "HUM"); got != string(domainalliance.War) {
		t.Fatalf("the machines answered %q, want a war", got)
	}
	assertSingleValue(t, database,
		"SELECT COUNT(*) > 0 FROM ai_decisions WHERE action LIKE 'declare war on HUM%'", 1)
}

// TestAnAllianceThatNeverDeclaresAnswersNothing is the other end of the setting.
func TestAnAllianceThatNeverDeclaresAnswersNothing(t *testing.T) {
	ctx := context.Background()
	database, universeWorld, _, _ := alliedArtificials(t)
	setRules(t, ctx, database, func(configured *rules.Ruleset) { configured.AI.Diplomacy = "none" })
	humanAllianceDeclaring(t, ctx, universeWorld, domainalliance.War)

	think(t, ctx, universeWorld)

	if got := declaredBy(t, ctx, universeWorld, "MCH", "HUM"); got != "" {
		t.Fatalf("a silent alliance declared %q", got)
	}
}

// TestAFirstImpressionSticksOrFollowsTheTurn walks the difference between the
// two rules that do speak, against a human alliance that changes its mind.
func TestAFirstImpressionSticksOrFollowsTheTurn(t *testing.T) {
	for _, this := range []struct {
		mode string
		want domainalliance.Relation
	}{
		{"static", domainalliance.War},
		{"dynamic", domainalliance.Pact},
	} {
		t.Run(this.mode, func(t *testing.T) {
			ctx := context.Background()
			database, universeWorld, _, _ := alliedArtificials(t)
			setRules(t, ctx, database, func(configured *rules.Ruleset) { configured.AI.Diplomacy = this.mode })
			alice := humanAllianceDeclaring(t, ctx, universeWorld, domainalliance.War)

			think(t, ctx, universeWorld)
			if got := declaredBy(t, ctx, universeWorld, "MCH", "HUM"); got != string(domainalliance.War) {
				t.Fatalf("the first answer was %q, want a war", got)
			}

			// The humans change their mind, and the machines either follow or
			// keep the impression they formed.
			if err := universeWorld.Alliance.Declare(ctx, alice, "MCH", domainalliance.Pact); err != nil {
				t.Fatalf("Declare() error = %v", err)
			}
			think(t, ctx, universeWorld)
			if got := declaredBy(t, ctx, universeWorld, "MCH", "HUM"); got != string(this.want) {
				t.Fatalf("a %s alliance settled on %q, want %q", this.mode, got, this.want)
			}
		})
	}
}
