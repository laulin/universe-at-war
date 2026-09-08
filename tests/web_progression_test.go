package tests

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	appauth "universeatwar/internal/app/authentication"
	appclock "universeatwar/internal/clock"
	storagesqlite "universeatwar/internal/storage/sqlite"
	webhandler "universeatwar/internal/web"
)

func TestWebResearchFlow(t *testing.T) {
	handler, database, session, csrfCookie := progressionHandler(t)
	ctx := context.Background()
	setBuilding(t, ctx, database, 1, "research_lab", 1)
	setResources(t, ctx, database, 1, 5000, 5000, 5000)

	page := getPage(t, handler, "/planets/1/research", session, csrfCookie)
	if !strings.Contains(page, "Technologie énergétique") {
		t.Fatalf("research page = %q", page)
	}
	if !strings.Contains(page, "Laboratoire de recherche") {
		t.Fatalf("research page hides the missing prerequisites: %q", page)
	}
	if !strings.Contains(page, `aria-current="page"`) || !strings.Contains(page, `href="/planets/1/shipyard"`) {
		t.Fatalf("research page is missing the shared navigation: %q", page)
	}

	key := hiddenValue(t, page, "idempotency_key")
	withoutCSRF := postFormRequest("/planets/1/research/energy_technology", url.Values{"idempotency_key": {key}})
	withoutCSRF.AddCookie(session)
	denied := httptest.NewRecorder()
	handler.ServeHTTP(denied, withoutCSRF)
	if denied.Code != http.StatusForbidden {
		t.Fatalf("POST research without CSRF = %d, want 403", denied.Code)
	}

	started := postFormRequest("/planets/1/research/energy_technology", url.Values{"csrf_token": {"csrf-token"}, "idempotency_key": {key}})
	started.AddCookie(session)
	started.AddCookie(csrfCookie)
	accepted := httptest.NewRecorder()
	handler.ServeHTTP(accepted, started)
	if accepted.Code != http.StatusSeeOther || accepted.Header().Get("Location") != "/planets/1/research" {
		t.Fatalf("POST research = %d %q", accepted.Code, accepted.Header().Get("Location"))
	}

	running := getPage(t, handler, "/planets/1/research", session, csrfCookie)
	if !strings.Contains(running, "Recherche en cours") || !strings.Contains(running, "data-countdown") {
		t.Fatalf("running research page = %q", running)
	}

	// A second research joins the queue instead of being refused, and the panel
	// shows it waiting with the forecast of its turn.
	second := postFormRequest("/planets/1/research/computer_technology", url.Values{"csrf_token": {"csrf-token"}, "idempotency_key": {"another"}})
	second.AddCookie(session)
	second.AddCookie(csrfCookie)
	queued := httptest.NewRecorder()
	handler.ServeHTTP(queued, second)
	if queued.Code != http.StatusSeeOther {
		t.Fatalf("second research = %d %q", queued.Code, queued.Body.String())
	}
	queuePage := getPage(t, handler, "/planets/1/research", session, csrfCookie)
	if !strings.Contains(queuePage, "Technologie ordinateur") || !strings.Contains(queuePage, "queue-entry--waiting") {
		t.Fatalf("research queue page = %q", queuePage)
	}
}

func TestWebShipyardAndDefensePages(t *testing.T) {
	handler, database, session, csrfCookie := progressionHandler(t)
	ctx := context.Background()
	setBuilding(t, ctx, database, 1, "shipyard", 1)
	setResearch(t, ctx, database, 1, "combustion_drive", 1)
	setResources(t, ctx, database, 1, 9000, 3000, 0)

	// A page is judged on the cards it offers, not on the names it happens to
	// print: a card names units of the other family in its rapid fire table.
	shipyard := getPage(t, handler, "/planets/1/shipyard", session, csrfCookie)
	if !strings.Contains(shipyard, cardTitle("Chasseur léger")) || strings.Contains(shipyard, cardTitle("Lanceur de missiles")) {
		t.Fatalf("shipyard page = %q", shipyard)
	}
	defense := getPage(t, handler, "/planets/1/defense", session, csrfCookie)
	if !strings.Contains(defense, cardTitle("Lanceur de missiles")) || strings.Contains(defense, cardTitle("Chasseur léger")) {
		t.Fatalf("defense page = %q", defense)
	}

	key := hiddenValue(t, shipyard, "idempotency_key")
	order := postFormRequest("/planets/1/shipyard/light_fighter", url.Values{"csrf_token": {"csrf-token"}, "idempotency_key": {key}, "quantity": {"2"}})
	order.AddCookie(session)
	order.AddCookie(csrfCookie)
	accepted := httptest.NewRecorder()
	handler.ServeHTTP(accepted, order)
	if accepted.Code != http.StatusSeeOther || accepted.Header().Get("Location") != "/planets/1/shipyard" {
		t.Fatalf("POST order = %d %q", accepted.Code, accepted.Header().Get("Location"))
	}

	running := getPage(t, handler, "/planets/1/shipyard", session, csrfCookie)
	if !strings.Contains(running, "Production en cours") || !strings.Contains(running, "0 / 2") {
		t.Fatalf("running shipyard page = %q", running)
	}

	invalid := postFormRequest("/planets/1/shipyard/light_fighter", url.Values{"csrf_token": {"csrf-token"}, "idempotency_key": {"zero"}, "quantity": {"0"}})
	invalid.AddCookie(session)
	invalid.AddCookie(csrfCookie)
	refused := httptest.NewRecorder()
	handler.ServeHTTP(refused, invalid)
	if refused.Code != http.StatusBadRequest || !strings.Contains(refused.Body.String(), "quantité") {
		t.Fatalf("zero quantity = %d %q", refused.Code, refused.Body.String())
	}

	wrongFamily := postFormRequest("/planets/1/defense/light_fighter", url.Values{"csrf_token": {"csrf-token"}, "idempotency_key": {"wrong"}, "quantity": {"1"}})
	wrongFamily.AddCookie(session)
	wrongFamily.AddCookie(csrfCookie)
	mismatch := httptest.NewRecorder()
	handler.ServeHTTP(mismatch, wrongFamily)
	if mismatch.Code != http.StatusBadRequest {
		t.Fatalf("ordering a ship from the defense page = %d, want 400", mismatch.Code)
	}
}

func TestWebProgressionPagesOfAnotherAccountAreNotFound(t *testing.T) {
	ctx := context.Background()
	database := economyDatabase(t, ctx, 2)
	universe := newWorld(t, database, appclock.NewFake(time.Date(2042, time.September, 10, 11, 12, 13, 0, time.UTC)))
	if _, err := universe.Economy.CreateEmpire(ctx, appauth.Principal{AccountID: 1}, "Alpha"); err != nil {
		t.Fatalf("CreateEmpire() error = %v", err)
	}
	if _, err := universe.Economy.CreateEmpire(ctx, appauth.Principal{AccountID: 2}, "Beta"); err != nil {
		t.Fatalf("CreateEmpire() error = %v", err)
	}
	setUnits(t, ctx, database, 1, "light_fighter", 42)
	handler, err := webhandler.New(webhandler.Dependencies{
		Authentication: webAuthenticationStub{principal: appauth.Principal{AccountID: 2, Username: "player2"}},
		ServerState:    runningStateStub{}, CSRFSecrets: sequenceSecret{value: "csrf-token"},
		Economy: universe.Economy, Research: universe.Research, Shipyard: universe.Shipyard,
	})
	if err != nil {
		t.Fatal(err)
	}
	session := &http.Cookie{Name: "uaw_session", Value: "session"}
	csrfCookie := &http.Cookie{Name: "uaw_csrf", Value: "csrf-token"}

	for _, target := range []string{"/planets/1/research", "/planets/1/shipyard", "/planets/1/defense"} {
		request := httptest.NewRequest(http.MethodGet, target, nil)
		request.AddCookie(session)
		request.AddCookie(csrfCookie)
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, request)
		if recorder.Code != http.StatusNotFound {
			t.Fatalf("GET %s of a foreign planet = %d, want 404", target, recorder.Code)
		}
		if strings.Contains(recorder.Body.String(), "42") {
			t.Fatalf("GET %s leaked the inventory of another player", target)
		}
	}
}

func progressionHandler(t *testing.T) (http.Handler, *storagesqlite.Database, *http.Cookie, *http.Cookie) {
	t.Helper()
	ctx := context.Background()
	database := economyDatabase(t, ctx, 1)
	universe := newWorld(t, database, appclock.NewFake(time.Date(2042, time.September, 10, 11, 12, 13, 0, time.UTC)))
	if _, err := universe.Economy.CreateEmpire(ctx, appauth.Principal{AccountID: 1}, "Captain"); err != nil {
		t.Fatalf("CreateEmpire() error = %v", err)
	}
	handler, err := webhandler.New(webhandler.Dependencies{
		Authentication: webAuthenticationStub{principal: appauth.Principal{AccountID: 1, Username: "player1"}},
		ServerState:    runningStateStub{}, CSRFSecrets: sequenceSecret{value: "csrf-token"},
		Economy: universe.Economy, Research: universe.Research, Shipyard: universe.Shipyard,
	})
	if err != nil {
		t.Fatal(err)
	}
	return handler, database, &http.Cookie{Name: "uaw_session", Value: "session"}, &http.Cookie{Name: "uaw_csrf", Value: "csrf-token"}
}

// cardTitle is how a page says it offers a unit, as opposed to merely naming it.
func cardTitle(name string) string {
	return `<h3 class="card__title">` + name + `</h3>`
}
