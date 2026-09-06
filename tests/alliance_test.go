package tests

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	appalliance "universeatwar/internal/app/alliance"
	appauth "universeatwar/internal/app/authentication"
	appclock "universeatwar/internal/clock"
	domainalliance "universeatwar/internal/domain/alliance"
	storagesqlite "universeatwar/internal/storage/sqlite"
)

func TestAllianceLifeFromFoundingToDissolution(t *testing.T) {
	ctx := context.Background()
	database, universeWorld, players := allianceUniverse(t, 3)
	founder, guest, third := players[0], players[1], players[2]

	profile, err := universeWorld.Alliance.Create(ctx, founder, "Les Corsaires", "cor", "Nous volons haut")
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if profile.Tag != "COR" || profile.Role != domainalliance.Founder || len(profile.Members) != 1 {
		t.Fatalf("profile = %+v", profile)
	}
	if _, err := universeWorld.Alliance.Create(ctx, guest, "Les Corsaires", "abc", ""); !errors.Is(err, appalliance.ErrNameTaken) {
		t.Fatalf("a duplicate name error = %v", err)
	}
	if _, err := universeWorld.Alliance.Create(ctx, founder, "Autre", "aut", ""); !errors.Is(err, appalliance.ErrAlreadyAMember) {
		t.Fatalf("founding twice error = %v", err)
	}

	if err := universeWorld.Alliance.Invite(ctx, founder, "player2"); err != nil {
		t.Fatalf("Invite() error = %v", err)
	}
	invitations, err := universeWorld.Alliance.Invitations(ctx, guest)
	if err != nil || len(invitations) != 1 {
		t.Fatalf("Invitations() = %d %v", len(invitations), err)
	}
	if err := universeWorld.Alliance.Accept(ctx, guest, invitations[0].ID); err != nil {
		t.Fatalf("Accept() error = %v", err)
	}
	// Accepting again changes nothing at all.
	if err := universeWorld.Alliance.Accept(ctx, guest, invitations[0].ID); err != nil {
		t.Fatalf("Accept() replay error = %v", err)
	}
	assertSingleValue(t, database, "SELECT COUNT(*) FROM alliance_members", 2)

	// A plain member may neither invite nor expel.
	if err := universeWorld.Alliance.Invite(ctx, guest, "player3"); !errors.Is(err, appalliance.ErrForbidden) {
		t.Fatalf("a member inviting error = %v", err)
	}
	if err := universeWorld.Alliance.Promote(ctx, guest, 1, domainalliance.Officer); !errors.Is(err, appalliance.ErrForbidden) {
		t.Fatalf("a member promoting error = %v", err)
	}
	if err := universeWorld.Alliance.Promote(ctx, founder, 2, domainalliance.Officer); err != nil {
		t.Fatalf("Promote() error = %v", err)
	}
	if err := universeWorld.Alliance.Invite(ctx, guest, "player3"); err != nil {
		t.Fatalf("an officer inviting error = %v", err)
	}

	pending, err := universeWorld.Alliance.Invitations(ctx, third)
	if err != nil || len(pending) != 1 {
		t.Fatalf("Invitations() = %d %v", len(pending), err)
	}
	if err := universeWorld.Alliance.Decline(ctx, third, pending[0].ID); err != nil {
		t.Fatalf("Decline() error = %v", err)
	}
	if err := universeWorld.Alliance.Decline(ctx, third, pending[0].ID); err != nil {
		t.Fatalf("Decline() replay error = %v", err)
	}
	assertSingleValue(t, database, "SELECT COUNT(*) FROM alliance_members", 2)

	// A founder hands the charge over before leaving.
	if err := universeWorld.Alliance.Leave(ctx, founder); !errors.Is(err, appalliance.ErrLastFounder) {
		t.Fatalf("a founder leaving error = %v", err)
	}
	if err := universeWorld.Alliance.Promote(ctx, founder, 2, domainalliance.Founder); err != nil {
		t.Fatalf("Promote(founder) error = %v", err)
	}
	if err := universeWorld.Alliance.Leave(ctx, founder); err != nil {
		t.Fatalf("Leave() error = %v", err)
	}
	assertSingleValue(t, database, "SELECT COUNT(*) FROM alliance_members", 1)
	assertSingleText(t, database, "SELECT role FROM alliance_members WHERE player_id = 2", "founder")

	// The last member leaving dissolves the alliance.
	if err := universeWorld.Alliance.Leave(ctx, guest); err != nil {
		t.Fatalf("Leave() error = %v", err)
	}
	assertSingleValue(t, database, "SELECT COUNT(*) FROM alliances", 0)
}

func TestOfficersCannotExpelTheirPeers(t *testing.T) {
	ctx := context.Background()
	database, universeWorld, players := allianceUniverse(t, 3)
	founder, first, second := players[0], players[1], players[2]
	joinAlliance(t, ctx, universeWorld, founder, first, "player2")
	joinAlliance(t, ctx, universeWorld, founder, second, "player3")

	if err := universeWorld.Alliance.Promote(ctx, founder, 2, domainalliance.Officer); err != nil {
		t.Fatalf("Promote() error = %v", err)
	}
	if err := universeWorld.Alliance.Promote(ctx, founder, 3, domainalliance.Officer); err != nil {
		t.Fatalf("Promote() error = %v", err)
	}
	if err := universeWorld.Alliance.Expel(ctx, first, 3); !errors.Is(err, appalliance.ErrForbidden) {
		t.Fatalf("an officer expelling a peer error = %v", err)
	}
	if err := universeWorld.Alliance.Expel(ctx, first, 1); !errors.Is(err, appalliance.ErrForbidden) {
		t.Fatalf("an officer expelling the founder error = %v", err)
	}
	if err := universeWorld.Alliance.Promote(ctx, founder, 3, domainalliance.Member); err != nil {
		t.Fatalf("Promote(member) error = %v", err)
	}
	if err := universeWorld.Alliance.Expel(ctx, first, 3); err != nil {
		t.Fatalf("an officer expelling a member error = %v", err)
	}
	assertSingleValue(t, database, "SELECT COUNT(*) FROM alliance_members", 2)
	assertSingleValue(t, database, "SELECT COUNT(*) FROM alliance_history WHERE action = 'member_expelled'", 1)
}

func TestConcurrentAcceptancesJoinOnlyOneAlliance(t *testing.T) {
	ctx := context.Background()
	database, universeWorld, players := allianceUniverse(t, 3)
	firstFounder, secondFounder, guest := players[0], players[1], players[2]

	if _, err := universeWorld.Alliance.Create(ctx, firstFounder, "Premiers", "prm", ""); err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if _, err := universeWorld.Alliance.Create(ctx, secondFounder, "Seconds", "sec", ""); err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	for _, host := range []appauth.Principal{firstFounder, secondFounder} {
		if err := universeWorld.Alliance.Invite(ctx, host, "player3"); err != nil {
			t.Fatalf("Invite() error = %v", err)
		}
	}
	invitations, err := universeWorld.Alliance.Invitations(ctx, guest)
	if err != nil || len(invitations) != 2 {
		t.Fatalf("Invitations() = %d %v", len(invitations), err)
	}

	start := make(chan struct{})
	results := make(chan error, 2)
	var wait sync.WaitGroup
	for _, invitation := range invitations {
		wait.Add(1)
		go func(id int64) {
			defer wait.Done()
			<-start
			results <- universeWorld.Alliance.Accept(ctx, guest, id)
		}(invitation.ID)
	}
	close(start)
	wait.Wait()
	close(results)

	successes := 0
	for err := range results {
		switch {
		case err == nil:
			successes++
		case errors.Is(err, appalliance.ErrAlreadyAMember):
		default:
			t.Fatalf("unexpected concurrent error: %v", err)
		}
	}
	if successes < 1 {
		t.Fatal("no acceptance succeeded")
	}
	assertSingleValue(t, database, "SELECT COUNT(*) FROM alliance_members WHERE player_id = 3", 1)
	assertSingleValue(t, database, "SELECT COUNT(*) FROM alliance_invitations WHERE player_id = 3 AND status = 'pending'", 0)
}

func TestExpiredInvitationsCannotBeAccepted(t *testing.T) {
	ctx := context.Background()
	database, universeWorld, players := allianceUniverse(t, 2)
	founder, guest := players[0], players[1]
	if _, err := universeWorld.Alliance.Create(ctx, founder, "Les Corsaires", "cor", ""); err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if err := universeWorld.Alliance.Invite(ctx, founder, "player2"); err != nil {
		t.Fatalf("Invite() error = %v", err)
	}
	invitations, err := universeWorld.Alliance.Invitations(ctx, guest)
	if err != nil || len(invitations) != 1 {
		t.Fatalf("Invitations() = %d %v", len(invitations), err)
	}
	universeWorld.Clock.Advance(200 * time.Hour)

	if err := universeWorld.Alliance.Accept(ctx, guest, invitations[0].ID); !errors.Is(err, appalliance.ErrInvitationEnded) {
		t.Fatalf("accepting an expired invitation error = %v", err)
	}
	assertSingleValue(t, database, "SELECT COUNT(*) FROM alliance_members", 1)
	expired, err := universeWorld.Alliance.Invitations(ctx, guest)
	if err != nil || len(expired) != 1 || !expired[0].Expired {
		t.Fatalf("an expired invitation is not marked: %+v", expired)
	}
}

func TestDiplomacyIsDeclaredAndRecorded(t *testing.T) {
	ctx := context.Background()
	database, universeWorld, players := allianceUniverse(t, 2)
	first, second := players[0], players[1]
	if _, err := universeWorld.Alliance.Create(ctx, first, "Premiers", "prm", ""); err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if _, err := universeWorld.Alliance.Create(ctx, second, "Seconds", "sec", ""); err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	if err := universeWorld.Alliance.Declare(ctx, first, "sec", domainalliance.War); err != nil {
		t.Fatalf("Declare() error = %v", err)
	}
	if err := universeWorld.Alliance.Declare(ctx, first, "sec", domainalliance.Pact); err != nil {
		t.Fatalf("Declare() again error = %v", err)
	}
	if err := universeWorld.Alliance.Declare(ctx, first, "prm", domainalliance.Pact); !errors.Is(err, appalliance.ErrForbidden) {
		t.Fatalf("declaring towards itself error = %v", err)
	}
	if err := universeWorld.Alliance.Declare(ctx, first, "zzz", domainalliance.Pact); !errors.Is(err, appalliance.ErrNotFound) {
		t.Fatalf("declaring towards nobody error = %v", err)
	}
	assertSingleValue(t, database, "SELECT COUNT(*) FROM alliance_relations", 1)
	assertSingleText(t, database, "SELECT relation FROM alliance_relations", "pact")
	assertSingleValue(t, database, "SELECT COUNT(*) FROM alliance_history WHERE action = 'relation_declared'", 2)

	// The declaration is one-sided and never reaches the other team's profile.
	profile, err := universeWorld.Alliance.Profile(ctx, second)
	if err != nil {
		t.Fatalf("Profile() error = %v", err)
	}
	if len(profile.Relations) != 0 {
		t.Fatalf("the target of a declaration sees it in its own profile: %+v", profile.Relations)
	}
}

func TestNonMembersReadNothingOfAnAlliance(t *testing.T) {
	ctx := context.Background()
	_, universeWorld, players := allianceUniverse(t, 2)
	founder, stranger := players[0], players[1]
	if _, err := universeWorld.Alliance.Create(ctx, founder, "Les Corsaires", "cor", ""); err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if _, err := universeWorld.Alliance.Profile(ctx, stranger); !errors.Is(err, appalliance.ErrNotAMember) {
		t.Fatalf("Profile() for a stranger error = %v", err)
	}
	if err := universeWorld.Alliance.Invite(ctx, stranger, "player1"); !errors.Is(err, appalliance.ErrNotAMember) {
		t.Fatalf("Invite() by a stranger error = %v", err)
	}
	if err := universeWorld.Alliance.Expel(ctx, stranger, 1); !errors.Is(err, appalliance.ErrNotAMember) {
		t.Fatalf("Expel() by a stranger error = %v", err)
	}
}

// allianceUniverse gives every account an empire, which alliances require.
func allianceUniverse(t *testing.T, players int) (*storagesqlite.Database, *world, []appauth.Principal) {
	t.Helper()
	ctx := context.Background()
	clock := appclock.NewFake(time.Date(2042, time.September, 10, 11, 12, 13, 0, time.UTC))
	database := economyDatabase(t, ctx, players)
	universeWorld := newWorld(t, database, clock)
	principals := make([]appauth.Principal, 0, players)
	for index := 1; index <= players; index++ {
		principal := appauth.Principal{AccountID: int64(index)}
		if _, err := universeWorld.Economy.CreateEmpire(ctx, principal, playerDisplayName(index)); err != nil {
			t.Fatalf("CreateEmpire() error = %v", err)
		}
		principals = append(principals, principal)
	}
	return database, universeWorld, principals
}

func playerDisplayName(index int) string {
	return "player" + string(rune('0'+index))
}

// joinAlliance invites a player and makes them accept, which most tests need as
// a starting point rather than as a subject.
func joinAlliance(t *testing.T, ctx context.Context, universeWorld *world, host, guest appauth.Principal, guestName string) {
	t.Helper()
	if _, err := universeWorld.Alliance.Profile(ctx, host); err != nil {
		if _, err := universeWorld.Alliance.Create(ctx, host, "Les Corsaires", "cor", ""); err != nil {
			t.Fatalf("Create() error = %v", err)
		}
	}
	if err := universeWorld.Alliance.Invite(ctx, host, guestName); err != nil {
		t.Fatalf("Invite() error = %v", err)
	}
	invitations, err := universeWorld.Alliance.Invitations(ctx, guest)
	if err != nil || len(invitations) == 0 {
		t.Fatalf("Invitations() = %d %v", len(invitations), err)
	}
	if err := universeWorld.Alliance.Accept(ctx, guest, invitations[0].ID); err != nil {
		t.Fatalf("Accept() error = %v", err)
	}
}
