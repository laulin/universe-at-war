package ai

import (
	"testing"

	domainalliance "universeatwar/internal/domain/alliance"
)

func TestAnAllianceThatNeverDeclaresStaysSilent(t *testing.T) {
	for _, heard := range []domainalliance.Relation{domainalliance.War, domainalliance.Pact} {
		if _, say := Answer(NeverDeclares, heard, "", false); say {
			t.Fatalf("a silent alliance answered %q", heard)
		}
	}
	// A rule this build cannot read says nothing rather than guessing.
	if _, say := Answer("negotiated", domainalliance.War, "", false); say {
		t.Fatal("an unreadable rule produced a declaration")
	}
}

func TestADeclarationIsAnsweredInKind(t *testing.T) {
	for _, mode := range []Diplomacy{DeclaresOnce, AnswersAlways} {
		for _, heard := range []domainalliance.Relation{domainalliance.War, domainalliance.Pact} {
			answer, say := Answer(mode, heard, "", false)
			if !say || answer != heard {
				t.Fatalf("%s answered %q with %q (%t)", mode, heard, answer, say)
			}
		}
	}
}

// TestAFirstImpressionSticks is the whole difference between the two rules that
// do speak: one revises its mind and the other does not.
func TestAFirstImpressionSticks(t *testing.T) {
	// The pact this alliance answered turns into a war.
	if _, say := Answer(DeclaresOnce, domainalliance.War, domainalliance.Pact, true); say {
		t.Fatal("a static alliance revised its mind")
	}
	answer, say := Answer(AnswersAlways, domainalliance.War, domainalliance.Pact, true)
	if !say || answer != domainalliance.War {
		t.Fatalf("a dynamic alliance did not follow the turn: %q (%t)", answer, say)
	}
}

func TestNothingIsDeclaredTwice(t *testing.T) {
	if _, say := Answer(AnswersAlways, domainalliance.War, domainalliance.War, true); say {
		t.Fatal("a settled relation was declared again")
	}
}

func TestOnlyTheThreeKnownDiplomaciesAreValid(t *testing.T) {
	for _, mode := range Diplomacies() {
		if !mode.Valid() {
			t.Fatalf("Diplomacies() offers %q, which Valid() refuses", mode)
		}
	}
	for _, unknown := range []Diplomacy{"", "negotiated", "NONE"} {
		if unknown.Valid() {
			t.Fatalf("Valid() accepted %q", unknown)
		}
	}
}
