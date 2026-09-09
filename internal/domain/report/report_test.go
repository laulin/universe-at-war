package report

import (
	"errors"
	"strings"
	"testing"
	"time"

	"universeatwar/internal/domain/economy"
	"universeatwar/internal/domain/universe"
)

func TestFreshnessClassifiesByAge(t *testing.T) {
	now := time.Date(2042, time.September, 10, 12, 0, 0, 0, time.UTC)
	recent := time.Hour
	if got := FreshnessOf(now.Add(-30*time.Minute), now, recent); got != Recent {
		t.Fatalf("FreshnessOf(recent) = %q", got)
	}
	if got := FreshnessOf(now.Add(-2*time.Hour), now, recent); got != Old {
		t.Fatalf("FreshnessOf(old) = %q", got)
	}
	if got := FreshnessOf(time.Time{}, now, recent); got != Unknown {
		t.Fatalf("FreshnessOf(never) = %q", got)
	}
}

func TestPayloadRoundTripsAndRefusesOtherVersions(t *testing.T) {
	payload := EspionagePayload{
		Target:           universe.Coordinate{Galaxy: 1, System: 2, Position: 3},
		TargetPlayerName: "Bob", TargetPlanetName: "Planète mère", Probes: 3, Level: 2,
		Resources: &economy.Resources{Metal: 100},
		Fleet:     map[string]int64{"light_fighter": 4},
	}
	version, document, err := Marshal(Espionage, payload)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	if version != CurrentPayloadVersion {
		t.Fatalf("version = %d", version)
	}
	decoded, err := Unmarshal(Espionage, version, document)
	if err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	restored, ok := decoded.(EspionagePayload)
	if !ok || restored.Fleet["light_fighter"] != 4 || restored.Resources.Metal != 100 {
		t.Fatalf("decoded payload = %#v", decoded)
	}
	if _, err := Unmarshal(Espionage, version+1, document); !errors.Is(err, ErrUnsupportedPayload) {
		t.Fatalf("Unmarshal(future) error = %v", err)
	}
}

func TestUnrevealedSectionsAreAbsentFromTheDocument(t *testing.T) {
	_, document, err := Marshal(Espionage, EspionagePayload{
		TargetPlayerName: "Bob", Probes: 1, Level: 1,
		Resources: &economy.Resources{Metal: 100},
	})
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	stored := string(document)
	for _, forbidden := range []string{"fleet", "defenses", "buildings", "research"} {
		if strings.Contains(stored, `"`+forbidden+`"`) {
			t.Fatalf("an unrevealed section leaked into the document: %s", stored)
		}
	}
	if !strings.Contains(stored, `"resources"`) {
		t.Fatalf("the revealed section is missing: %s", stored)
	}
}

func TestMarshalRefusesAMismatchedPayload(t *testing.T) {
	if _, _, err := Marshal(CombatAttack, EspionagePayload{}); err == nil {
		t.Fatal("Marshal() accepted a payload of another kind")
	}
	if _, _, err := Marshal("gossip", EspionagePayload{}); err == nil {
		t.Fatal("Marshal() accepted an unknown kind")
	}
}

func TestHostileKinds(t *testing.T) {
	if !CombatDefense.Hostile() || !EspionageDetected.Hostile() {
		t.Fatal("a defense report and a detection are hostile")
	}
	if CombatAttack.Hostile() || Espionage.Hostile() || Recycling.Hostile() {
		t.Fatal("only reports about being attacked are hostile")
	}
}

// A section revealed on a planet that holds nothing is written as an empty one.
// Folded back into an absent section, it would say "nobody looked" about the
// very fact the mission established: that there is nothing there.
func TestARevealedEmptySectionStaysInTheDocument(t *testing.T) {
	_, document, err := Marshal(Espionage, EspionagePayload{
		TargetPlayerName: "Bob", Probes: 10, Level: 9,
		Resources: &economy.Resources{Metal: 100},
		Fleet:     map[string]int64{},
		Defenses:  map[string]int64{},
		Buildings: map[string]int{"metal_mine": 11},
		Research:  map[string]int{},
	})
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	stored := string(document)
	for _, section := range []string{"fleet", "defenses", "research"} {
		if !strings.Contains(stored, `"`+section+`":{}`) {
			t.Fatalf("a revealed empty section was dropped: %s", stored)
		}
	}

	decoded, err := Unmarshal(Espionage, CurrentPayloadVersion, document)
	if err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	restored := decoded.(EspionagePayload)
	for name, section := range map[string]bool{
		"fleet": restored.Fleet == nil, "defenses": restored.Defenses == nil, "research": restored.Research == nil,
	} {
		if section {
			t.Fatalf("the %s section came back as never revealed", name)
		}
	}
	if len(restored.Fleet) != 0 || len(restored.Research) != 0 {
		t.Fatalf("a revealed empty section came back with contents: %#v", restored)
	}
}
