package rules

import (
	"errors"
	"testing"
)

func TestEveryProfileIsValidAndDistinct(t *testing.T) {
	profiles := Profiles()
	if len(profiles) != 6 {
		t.Fatalf("profiles = %d, want the six of the specification", len(profiles))
	}
	seen := map[string]bool{}
	for _, profile := range profiles {
		if profile.ID == "" || profile.Name == "" || profile.Description == "" {
			t.Fatalf("profile %+v is not presentable", profile)
		}
		if seen[profile.ID] {
			t.Fatalf("two profiles share the identifier %q", profile.ID)
		}
		seen[profile.ID] = true
		if err := profile.Rules.Validate(); err != nil {
			t.Fatalf("profile %q is invalid: %v", profile.ID, err)
		}
		if profile.Rules.SchemaVersion != CurrentSchemaVersion {
			t.Fatalf("profile %q carries schema %d", profile.ID, profile.Rules.SchemaVersion)
		}
		// A profile survives a round trip through its own document.
		document, err := Encode(profile.Rules)
		if err != nil {
			t.Fatal(err)
		}
		decoded, err := Decode(document)
		if err != nil {
			t.Fatalf("profile %q does not decode: %v", profile.ID, err)
		}
		if decoded != profile.Rules {
			t.Fatalf("profile %q changed through its document", profile.ID)
		}
	}
	found, err := ProfileByID("classic")
	if err != nil || found.Rules != Default() {
		t.Fatalf("ProfileByID(classic) = %+v %v", found.ID, err)
	}
	if _, err := ProfileByID("nowhere"); !errors.Is(err, ErrUnknownProfile) {
		t.Fatalf("ProfileByID(nowhere) error = %v", err)
	}
}

func TestComparisonListsWhatChangesAndNothingElse(t *testing.T) {
	if differences, err := Compare(Default(), Default()); err != nil || len(differences) != 0 {
		t.Fatalf("Compare(same) = %d %v", len(differences), err)
	}
	slow, err := ProfileByID("slow")
	if err != nil {
		t.Fatal(err)
	}
	differences, err := Compare(Default(), slow.Rules)
	if err != nil {
		t.Fatal(err)
	}
	if len(differences) == 0 {
		t.Fatal("a slower universe differs in nothing")
	}
	paths := map[string]Difference{}
	for _, difference := range differences {
		paths[difference.Path] = difference
	}
	economy, found := paths["time.economy_speed"]
	if !found || economy.Before != "1" || economy.After != "0.5" {
		t.Fatalf("the economy speed reads %+v", economy)
	}
	// The list is ordered, so two runs read the same.
	for index := 1; index < len(differences); index++ {
		if differences[index-1].Path >= differences[index].Path {
			t.Fatalf("the differences are not ordered: %v", differences)
		}
	}
	// Nothing untouched shows up.
	if _, found := paths["identity.name"]; found {
		t.Fatal("an untouched setting was listed as a difference")
	}
}
