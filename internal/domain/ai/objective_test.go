package ai

import (
	"testing"
	"time"

	"universeatwar/internal/domain/economy"
	"universeatwar/internal/domain/universe"
)

func TestRolesGoToWhoDeclaredTheMost(t *testing.T) {
	capabilities := []Capability{
		{PlayerID: 1, Awake: true, Probes: 2, WarStrength: 500, Recyclers: 0, GroundDefence: 100, Hauling: 5000},
		{PlayerID: 2, Awake: true, Probes: 9, WarStrength: 100, Recyclers: 1, GroundDefence: 50, Hauling: 100},
		{PlayerID: 3, Awake: true, Probes: 0, WarStrength: 50, Recyclers: 8, GroundDefence: 20, Hauling: 200},
		{PlayerID: 4, Awake: false, Probes: 40, WarStrength: 9000, Recyclers: 40, GroundDefence: 9000, Hauling: 90000},
		{PlayerID: 5, Awake: true, Probes: 0, WarStrength: 0, Recyclers: 0, GroundDefence: 0, Hauling: 0},
	}
	roles := AssignRoles(capabilities)
	if roles[2] != ScoutRole || roles[1] != FleeterRole || roles[3] != RecyclerRole {
		t.Fatalf("roles = %v", roles)
	}
	// A sleeping member carries nothing, however well equipped it is.
	if roles[4] != MinerRole {
		t.Fatalf("a sleeping member was given %s", roles[4])
	}
	// A member with nothing to offer stays a miner.
	if roles[5] != MinerRole {
		t.Fatalf("an empty member was given %s", roles[5])
	}
	// The same declarations always give the same team.
	for playerID, role := range AssignRoles(capabilities) {
		if roles[playerID] != role {
			t.Fatalf("player %d went from %s to %s", playerID, roles[playerID], role)
		}
	}
	if len(AssignRoles(nil)) != 0 {
		t.Fatal("an empty alliance was given roles")
	}
}

func TestQuorumNeverFallsBelowTwo(t *testing.T) {
	cases := map[int]int{0: 2, 1: 2, 2: 2, 4: 2, 7: 3, 10: 5}
	for awake, want := range cases {
		if got := Quorum(awake); got != want {
			t.Fatalf("Quorum(%d) = %d, want %d", awake, got, want)
		}
	}
}

func TestObjectiveComesFromTheCommonMemoryAlone(t *testing.T) {
	now := time.Date(2042, time.September, 10, 12, 0, 0, 0, time.UTC)
	rich := Knowledge{
		Kind: TargetKnowledge, AuthorID: 1, Coordinate: universe.Coordinate{Galaxy: 1, System: 1, Position: 4},
		Confidence: 1, ExpiresAt: now.Add(time.Hour), Plunder: economy.Resources{Metal: 40000}, Complete: true,
	}
	preferences := Raider.Preferences()
	kind, at, found := ChooseObjective([]Knowledge{rich}, preferences, now)
	if !found || kind != RaidObjective || at != rich.Coordinate {
		t.Fatalf("ChooseObjective() = %v %v %v", kind, at, found)
	}
	// Coming to the rescue of a member comes first.
	threat := Knowledge{
		Kind: ThreatKnowledge, AuthorID: 2, Coordinate: universe.Coordinate{Galaxy: 1, System: 1, Position: 9},
		Confidence: 1, ObservedAt: now, ExpiresAt: now.Add(time.Hour),
	}
	kind, at, found = ChooseObjective([]Knowledge{rich, threat}, preferences, now)
	if !found || kind != DefenceObjective || at != threat.Coordinate {
		t.Fatalf("a member under attack was left alone: %v %v %v", kind, at, found)
	}
	// A belief nobody can trust any more decides nothing.
	stale := rich
	stale.Confidence = 0
	if _, _, found := ChooseObjective([]Knowledge{stale}, preferences, now); found {
		t.Fatal("a worthless belief opened an objective")
	}
	// And a careful character asks far more of the same belief.
	if _, _, found := ChooseObjective([]Knowledge{rich}, Turtle.Preferences(), now); found {
		t.Fatal("a turtle opened a raid on this")
	}
	if _, _, found := ChooseObjective(nil, preferences, now); found {
		t.Fatal("an objective came out of an empty memory")
	}
}

func TestObjectiveFollowsOnlyDocumentedTransitions(t *testing.T) {
	allowed := map[[2]ObjectiveState]bool{
		{Scouting, Assembling}: true, {Scouting, Abandoned}: true,
		{Assembling, Achieved}: true, {Assembling, Abandoned}: true,
	}
	states := []ObjectiveState{Scouting, Assembling, Achieved, Abandoned}
	for _, from := range states {
		for _, to := range states {
			if CanAdvance(from, to) != allowed[[2]ObjectiveState{from, to}] {
				t.Fatalf("CanAdvance(%s, %s) = %v", from, to, CanAdvance(from, to))
			}
		}
	}
	if !Scouting.Open() || !Assembling.Open() || Achieved.Open() || Abandoned.Open() {
		t.Fatal("the open states are wrong")
	}
}
