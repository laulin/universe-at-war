package tests

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	appai "universeatwar/internal/app/ai"
	appauth "universeatwar/internal/app/authentication"
	appranking "universeatwar/internal/app/ranking"
	appclock "universeatwar/internal/clock"
	domainai "universeatwar/internal/domain/ai"
	webhandler "universeatwar/internal/web"
)

// The standings are public game information: human and artificial empires are
// scored by the same catalogue, and the reader can switch category without a
// second source of truth.
func TestRankingIncludesHumansAndArtificialPlayers(t *testing.T) {
	ctx := context.Background()
	database := economyDatabase(t, ctx, 2)
	universe := newWorld(t, database, appclock.NewFake(time.Date(2042, time.September, 10, 11, 12, 13, 0, time.UTC)))
	if _, err := universe.Economy.CreateEmpire(ctx, appauth.Principal{AccountID: 1}, "Alice"); err != nil {
		t.Fatal(err)
	}
	if _, err := universe.Economy.CreateEmpire(ctx, appauth.Principal{AccountID: 2}, "Bob"); err != nil {
		t.Fatal(err)
	}
	if _, err := universe.Alliance.Create(ctx, appauth.Principal{AccountID: 2}, "Alliance Beta", "BETA", ""); err != nil {
		t.Fatal(err)
	}
	profile, err := universe.AI.Create(ctx, appauth.Principal{AccountID: 1, Roles: []appauth.Role{appauth.RoleAdmin}}, appai.Request{
		Name: "Kepler", Archetype: domainai.CautiousMiner,
		Window: domainai.Window{Start: 0, End: 0}, Interval: 5 * time.Minute,
	})
	if err != nil {
		t.Fatalf("create artificial player: %v", err)
	}
	var artificialPlanet int64
	if err := database.Read().QueryRowContext(ctx,
		"SELECT id FROM planets WHERE owner_player_id = ?", profile.PlayerID).Scan(&artificialPlanet); err != nil {
		t.Fatalf("find artificial planet: %v", err)
	}
	setBuilding(t, ctx, database, 1, "metal_storage", 1)
	setResearch(t, ctx, database, 2, "energy_technology", 1)
	setUnits(t, ctx, database, artificialPlanet, "light_fighter", 2)

	handler, err := webhandler.New(webhandler.Dependencies{
		Authentication: webAuthenticationStub{principal: appauth.Principal{AccountID: 1, Username: "player1", Roles: []appauth.Role{appauth.RolePlayer}}},
		ServerState:    runningStateStub{}, CSRFSecrets: sequenceSecret{value: "csrf-token"},
		Economy: universe.Economy, Ranking: universe.Ranking,
	})
	if err != nil {
		t.Fatal(err)
	}
	session := &http.Cookie{Name: "uaw_session", Value: "session"}
	csrf := &http.Cookie{Name: "uaw_csrf", Value: "csrf-token"}

	page := getPage(t, handler, "/ranking", session, csrf)
	for _, want := range []string{"Classement Général", "Alice", "Bob", "Kepler", "Joueur artificiel", "(vous)", "[BETA] Alliance Beta", "Économie", "Recherche", "Militaire"} {
		if !strings.Contains(page, want) {
			t.Fatalf("general ranking misses %q: %q", want, page)
		}
	}
	if strings.Index(page, "Kepler") > strings.Index(page, "Alice") {
		t.Fatalf("the artificial player with 8 points is not ahead of Alice: %q", page)
	}
	if !strings.Contains(page, `href="/ranking" aria-current="page"`) {
		t.Fatalf("the navigation does not mark ranking as current: %q", page)
	}

	economy := getPage(t, handler, "/ranking?category=economy", session, csrf)
	if !strings.Contains(economy, "Classement Économie") || strings.Index(economy, "Alice") > strings.Index(economy, "Kepler") {
		t.Fatalf("economy ranking does not put Alice first: %q", economy)
	}
}

// A launch moves ships out of the planet inventory into fleet_ships. It must
// not make the owner lose military points for the duration of the flight.
func TestRankingKeepsShipsInFlight(t *testing.T) {
	ctx := context.Background()
	_, universe, alice, _ := launchedTransport(t, ctx,
		appclock.NewFake(time.Date(2042, time.September, 10, 11, 12, 13, 0, time.UTC)))
	entries, err := universe.Ranking.List(ctx, alice, appranking.Military)
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	for _, entry := range entries {
		if entry.Own {
			// Two small cargos cost 4,000 resources each.
			if entry.Points != 8 {
				t.Fatalf("military points while flying = %d, want 8", entry.Points)
			}
			return
		}
	}
	t.Fatal("the fleet owner is absent from the ranking")
}
