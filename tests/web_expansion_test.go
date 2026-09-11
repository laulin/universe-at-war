package tests

import (
	"context"
	"fmt"
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

func TestWebEmpireListsMoonsAndOffersTheirPages(t *testing.T) {
	handler, database, _, bodies, session, csrfCookie := expansionHandler(t)
	ctx := context.Background()
	_ = database
	_ = ctx

	overview := getPage(t, handler, "/", session, csrfCookie)
	if !strings.Contains(overview, "Lune") || !strings.Contains(overview, "Planète") {
		t.Fatalf("the empire view does not tell moons from planets: %q", overview)
	}
	planetPage := getPage(t, handler, fmt.Sprintf("/planets/%d", bodies.AliceHome), session, csrfCookie)
	if strings.Contains(planetPage, fmt.Sprintf(`href="/planets/%d/phalanx"`, bodies.AliceHome)) {
		t.Fatalf("a planet offers a phalanx: %q", planetPage)
	}
	moonPage := getPage(t, handler, fmt.Sprintf("/planets/%d/jump", bodies.Moon), session, csrfCookie)
	if !strings.Contains(moonPage, fmt.Sprintf(`href="/planets/%d/phalanx"`, bodies.Moon)) || !strings.Contains(moonPage, "Porte de saut") {
		t.Fatalf("the moon page misses its lunar pages: %q", moonPage)
	}
}

func TestWebPhalanxScanShowsTimingsOnly(t *testing.T) {
	handler, database, universeWorld, bodies, session, csrfCookie := expansionHandler(t)
	ctx := context.Background()
	setResources(t, ctx, database, bodies.Moon, 0, 0, 20000)
	setUnits(t, ctx, database, bodies.BobHome, "small_cargo", 4)
	setResources(t, ctx, database, bodies.BobHome, 5000, 5000, 5000)
	launchTransportTowards(t, ctx, universeWorld, bodies.BobHome, coordinateOf(t, 1, 1, 8))

	page := getPage(t, handler, fmt.Sprintf("/planets/%d/phalanx", bodies.Moon), session, csrfCookie)
	if !strings.Contains(page, "Balayer") {
		t.Fatalf("the phalanx page offers no sweep: %q", page)
	}
	scan := postFormRequest(fmt.Sprintf("/planets/%d/phalanx", bodies.Moon), url.Values{
		"csrf_token": {"csrf-token"}, "galaxy": {"1"}, "system": {"1"}, "position": {"8"},
	})
	scan.AddCookie(session)
	scan.AddCookie(csrfCookie)
	swept := httptest.NewRecorder()
	handler.ServeHTTP(swept, scan)
	if swept.Code != http.StatusOK {
		t.Fatalf("POST phalanx = %d %q", swept.Code, swept.Body.String())
	}
	body := swept.Body.String()
	if !strings.Contains(body, "Missions détectées") || !strings.Contains(body, "Transport") {
		t.Fatalf("the sweep found nothing: %q", body)
	}
	for _, secret := range []string{"small_cargo", "Petit transporteur", "cargo"} {
		if strings.Contains(body, secret) {
			t.Fatalf("the sweep revealed %q", secret)
		}
	}
	assertSingleValue(t, database, "SELECT deuterium FROM planet_resources WHERE planet_id = ?", 15000, bodies.Moon)

	tooFar := postFormRequest(fmt.Sprintf("/planets/%d/phalanx", bodies.Moon), url.Values{
		"csrf_token": {"csrf-token"}, "galaxy": {"1"}, "system": {"60"}, "position": {"8"},
	})
	tooFar.AddCookie(session)
	tooFar.AddCookie(csrfCookie)
	refused := httptest.NewRecorder()
	handler.ServeHTTP(refused, tooFar)
	if refused.Code != http.StatusBadRequest || !strings.Contains(refused.Body.String(), "hors de portée") {
		t.Fatalf("a distant sweep = %d %q", refused.Code, refused.Body.String())
	}
}

func TestWebJumpGateShowsItsCooldown(t *testing.T) {
	handler, database, _, bodies, session, csrfCookie := expansionHandler(t)
	ctx := context.Background()
	setUnits(t, ctx, database, bodies.Moon, "light_fighter", 10)

	page := getPage(t, handler, fmt.Sprintf("/planets/%d/jump", bodies.Moon), session, csrfCookie)
	if !strings.Contains(page, "prête") || !strings.Contains(page, "Chasseur léger") {
		t.Fatalf("jump gate page = %q", page)
	}
	jump := postFormRequest(fmt.Sprintf("/planets/%d/jump", bodies.Moon), url.Values{
		"csrf_token": {"csrf-token"}, "destination": {fmt.Sprint(bodies.FarMoon)},
		"composition[light_fighter]": {"4"}, "idempotency_key": {"jump"},
	})
	jump.AddCookie(session)
	jump.AddCookie(csrfCookie)
	jumped := httptest.NewRecorder()
	handler.ServeHTTP(jumped, jump)
	if jumped.Code != http.StatusSeeOther {
		t.Fatalf("POST jump = %d %q", jumped.Code, jumped.Body.String())
	}
	assertSingleValue(t, database, "SELECT quantity FROM planet_units WHERE planet_id = ? AND unit_id = 'light_fighter'", 4, bodies.FarMoon)

	cooling := getPage(t, handler, fmt.Sprintf("/planets/%d/jump", bodies.Moon), session, csrfCookie)
	if !strings.Contains(cooling, "en recharge") || !strings.Contains(cooling, "data-countdown") {
		t.Fatalf("the page does not show the cooldown: %q", cooling)
	}
}

// expansionHandler gives Alice a planet, its moon with both lunar sensors, a
// colony and a second moon with a gate.
func expansionHandler(t *testing.T) (http.Handler, *storagesqlite.Database, *world, expansionBodies, *http.Cookie, *http.Cookie) {
	t.Helper()
	ctx := context.Background()
	database := economyDatabase(t, ctx, 2)
	universeWorld := newWorld(t, database, appclock.NewFake(time.Date(2042, time.September, 10, 11, 12, 13, 0, time.UTC)))
	if _, err := universeWorld.Economy.CreateEmpire(ctx, appauth.Principal{AccountID: 1}, "Alice"); err != nil {
		t.Fatalf("CreateEmpire() error = %v", err)
	}
	if _, err := universeWorld.Economy.CreateEmpire(ctx, appauth.Principal{AccountID: 2}, "Bob"); err != nil {
		t.Fatalf("CreateEmpire() error = %v", err)
	}
	moon := insertMoon(t, ctx, database, 1, 1, 1, 1, 8)
	colony := insertColony(t, ctx, database, 1, "Colonie", 1, 2, 4)
	far := insertMoon(t, ctx, database, 1, colony, 1, 2, 4)
	for _, body := range []int64{moon, far} {
		setBuilding(t, ctx, database, body, "lunar_base", 1)
		setBuilding(t, ctx, database, body, "jump_gate", 1)
	}
	setBuilding(t, ctx, database, moon, "sensor_phalanx", 3)
	setResources(t, ctx, database, moon, 0, 0, 20000)

	handler, err := webhandler.New(webhandler.Dependencies{
		Authentication: webAuthenticationStub{principal: appauth.Principal{AccountID: 1, Username: "player1"}},
		ServerState:    runningStateStub{}, CSRFSecrets: sequenceSecret{value: "csrf-token"},
		Economy: universeWorld.Economy, Research: universeWorld.Research, Shipyard: universeWorld.Shipyard,
		Fleet: universeWorld.Fleet, Galaxy: universeWorld.Galaxy,
		Reports: universeWorld.Reports, Phalanx: universeWorld.Phalanx, JumpGate: universeWorld.JumpGate,
	})
	if err != nil {
		t.Fatal(err)
	}
	bodies := expansionBodies{AliceHome: 1, BobHome: 2, Moon: moon, Colony: colony, FarMoon: far}
	return handler, database, universeWorld, bodies,
		&http.Cookie{Name: "uaw_session", Value: "session"}, &http.Cookie{Name: "uaw_csrf", Value: "csrf-token"}
}

// expansionBodies names the celestial bodies the expansion pages act on.
type expansionBodies struct {
	AliceHome int64
	BobHome   int64
	Moon      int64
	Colony    int64
	FarMoon   int64
}
