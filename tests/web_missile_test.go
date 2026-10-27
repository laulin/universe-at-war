package tests

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	appauth "universeatwar/internal/app/authentication"
	webhandler "universeatwar/internal/web"
)

func missileHandler(t *testing.T, w *world, accountID int64) (http.Handler, *http.Cookie, *http.Cookie) {
	t.Helper()
	handler, err := webhandler.New(webhandler.Dependencies{
		Authentication: webAuthenticationStub{principal: appauth.Principal{AccountID: accountID, Username: fmt.Sprintf("player%d", accountID)}},
		ServerState:    runningStateStub{}, CSRFSecrets: sequenceSecret{value: "csrf-token"},
		Economy: w.Economy, Fleet: w.Fleet, Missiles: w.Missiles, Galaxy: w.Galaxy, Reports: w.Reports, Activity: w.Activity,
	})
	if err != nil {
		t.Fatal(err)
	}
	return handler, &http.Cookie{Name: "uaw_session", Value: "session"}, &http.Cookie{Name: "uaw_csrf", Value: "csrf-token"}
}

func TestWebMissileGalaxyAvailabilityAndMoons(t *testing.T) {
	w, _ := missileWorld(t)
	handler, session, csrf := missileHandler(t, w, 1)
	ctx := context.Background()
	moon := insertMoon(t, ctx, w.Database, 2, 2, 1, 1, 1)
	page := getPage(t, handler, "/galaxy/1/1", session, csrf)
	row := galaxyRowOf(t, page, "1:1:1")
	for _, expected := range []string{"kind=planet", "kind=moon", "Missiles vers la lune", "Bob", "Lune"} {
		if !strings.Contains(row, expected) {
			t.Fatalf("missing %q in %s", expected, row)
		}
	}
	if moon <= 0 {
		t.Fatal("moon missing")
	}
	setUnits(t, ctx, w.Database, 1, "interplanetary_missile", 0)
	page = getPage(t, handler, "/galaxy/1/1", session, csrf)
	row = galaxyRowOf(t, page, "1:1:1")
	if !strings.Contains(row, "Aucun missile interplanétaire disponible") || !strings.Contains(row, "disabled") || strings.Contains(row, "/missiles?") {
		t.Fatalf("zero stock action: %s", row)
	}
	setUnits(t, ctx, w.Database, 1, "interplanetary_missile", 10)
	insertPlanet(t, ctx, w.Database, 2, "Lointaine", 1, 30, 4)
	page = getPage(t, handler, "/galaxy/1/30", session, csrf)
	if row = galaxyRowOf(t, page, "1:30:4"); !strings.Contains(row, "Cible hors de portée") || strings.Contains(row, "/missiles?") {
		t.Fatalf("range action: %s", row)
	}
	if _, err := w.Database.Write().ExecContext(ctx, "UPDATE ruleset_versions SET document=json_set(document,'$.combat.missiles_enabled',json('false')) WHERE status='active'"); err != nil {
		t.Fatal(err)
	}
	page = getPage(t, handler, "/galaxy/1/1", session, csrf)
	if row = galaxyRowOf(t, page, "1:1:1"); !strings.Contains(row, "Missiles désactivés") {
		t.Fatalf("disabled ruleset: %s", row)
	}
}

func TestWebMissilePreviewLaunchAndPrivateReports(t *testing.T) {
	w, _ := missileWorld(t)
	handler, session, csrf := missileHandler(t, w, 1)
	ctx := context.Background()
	setUnits(t, ctx, w.Database, 2, "rocket_launcher", 100)
	setUnits(t, ctx, w.Database, 2, "light_fighter", 987654)
	setResources(t, ctx, w.Database, 2, 765432, 0, 0)
	page := getPage(t, handler, "/planets/1/missiles?galaxy=1&system=1&position=1&kind=planet", session, csrf)
	for _, expected := range []string{"Attaque par missiles", "Nombre de missiles", "Défense prioritaire", "30s"} {
		if !strings.Contains(page, expected) {
			t.Fatalf("missing %q: %s", expected, page)
		}
	}
	for _, secret := range []string{"987654", "765432"} {
		if strings.Contains(page, secret) {
			t.Fatalf("target secret leaked: %s", secret)
		}
	}
	values := url.Values{"csrf_token": {"csrf-token"}, "galaxy": {"1"}, "system": {"1"}, "position": {"1"}, "kind": {"planet"}, "count": {"2"}, "priority": {"rocket_launcher"}}
	preview := postFormRequest("/planets/1/missiles/preview", values)
	preview.AddCookie(session)
	preview.AddCookie(csrf)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, preview)
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "Lancer les missiles — tir irréversible") {
		t.Fatalf("preview=%d: %s", response.Code, response.Body.String())
	}
	assertSingleValue(t, w.Database, "SELECT COUNT(*) FROM missile_strikes", 0)
	assertSingleValue(t, w.Database, "SELECT quantity FROM planet_units WHERE planet_id=1 AND unit_id='interplanetary_missile'", 10)
	match := regexp.MustCompile(`name="idempotency_key" value="([^"]+)"`).FindStringSubmatch(response.Body.String())
	if len(match) != 2 {
		t.Fatal("missing idempotency key")
	}
	values.Set("idempotency_key", match[1])
	for range 2 {
		launch := postFormRequest("/planets/1/missiles/launch", values)
		launch.AddCookie(session)
		launch.AddCookie(csrf)
		response = httptest.NewRecorder()
		handler.ServeHTTP(response, launch)
		if response.Code != http.StatusSeeOther {
			t.Fatalf("launch=%d: %s", response.Code, response.Body.String())
		}
	}
	assertSingleValue(t, w.Database, "SELECT COUNT(*) FROM missile_strikes", 1)
	assertSingleValue(t, w.Database, "SELECT quantity FROM planet_units WHERE planet_id=1 AND unit_id='interplanetary_missile'", 8)
	page = getPage(t, handler, "/planets/1/fleet", session, csrf)
	if !strings.Contains(page, "Salves de missiles en vol") || strings.Contains(page, "/recall") {
		t.Fatalf("missing salve or unexpected recall: %s", page)
	}
	w.Clock.Advance(30 * time.Second)
	if _, err := w.Events.CompleteDue(ctx, 20); err != nil {
		t.Fatal(err)
	}
	page = getPage(t, handler, "/reports", session, csrf)
	if !strings.Contains(page, "Attaque par missiles") {
		t.Fatalf("report missing: %s", page)
	}
	var attackerReport, defenderReport int64
	if err := w.Database.Read().QueryRowContext(ctx, "SELECT id FROM reports WHERE kind='missile_attack'").Scan(&attackerReport); err != nil {
		t.Fatal(err)
	}
	if err := w.Database.Read().QueryRowContext(ctx, "SELECT id FROM reports WHERE kind='missile_defense'").Scan(&defenderReport); err != nil {
		t.Fatal(err)
	}
	page = getPage(t, handler, fmt.Sprintf("/reports/%d", attackerReport), session, csrf)
	if !strings.Contains(page, "Défenses détruites") || !strings.Contains(page, "Lanceur de missiles") {
		t.Fatalf("missing losses: %s", page)
	}
	request := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/reports/%d", defenderReport), nil)
	request.AddCookie(session)
	request.AddCookie(csrf)
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusNotFound {
		t.Fatalf("foreign report accessible: %d", response.Code)
	}
}

func TestWebMissileRejectsCSRFAndForgedLaunch(t *testing.T) {
	w, _ := missileWorld(t)
	handler, session, csrf := missileHandler(t, w, 1)
	values := url.Values{"csrf_token": {"wrong"}, "galaxy": {"1"}, "system": {"1"}, "position": {"1"}, "kind": {"planet"}, "count": {"11"}, "priority": {""}, "idempotency_key": {"forged"}}
	for _, test := range []struct {
		name, token, path string
		status            int
	}{
		{"csrf", "wrong", "/planets/1/missiles/launch", http.StatusForbidden},
		{"stock", "csrf-token", "/planets/1/missiles/launch", http.StatusBadRequest},
		{"foreign origin", "csrf-token", "/planets/2/missiles/launch", http.StatusNotFound},
	} {
		t.Run(test.name, func(t *testing.T) {
			values.Set("csrf_token", test.token)
			request := postFormRequest(test.path, values)
			request.AddCookie(session)
			request.AddCookie(csrf)
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != test.status {
				t.Fatalf("status=%d: %s", response.Code, response.Body.String())
			}
		})
	}
	assertSingleValue(t, w.Database, "SELECT COUNT(*) FROM missile_strikes", 0)
}

func TestWebMissileActionUsesSelectedOriginAndRejectsLunarOrigin(t *testing.T) {
	w, _ := missileWorld(t)
	ctx := context.Background()
	handler, session, csrf := missileHandler(t, w, 1)
	colony := insertPlanet(t, ctx, w.Database, 1, "Colonie", 1, 1, 3)
	setBuilding(t, ctx, w.Database, colony, "missile_silo", 4)
	selected := &http.Cookie{Name: "uaw_body", Value: strconv.FormatInt(colony, 10)}
	page := getPage(t, handler, "/galaxy/1/1", session, csrf, selected)
	row := galaxyRowOf(t, page, "1:1:1")
	if !strings.Contains(row, "Aucun missile interplanétaire disponible") || strings.Contains(row, "/missiles?") {
		t.Fatalf("stock from wrong origin: %s", row)
	}
	setUnits(t, ctx, w.Database, colony, "interplanetary_missile", 1)
	page = getPage(t, handler, "/galaxy/1/1", session, csrf, selected)
	if row = galaxyRowOf(t, page, "1:1:1"); !strings.Contains(row, fmt.Sprintf("/planets/%d/missiles?", colony)) {
		t.Fatalf("wrong origin: %s", row)
	}
	moon := insertMoon(t, ctx, w.Database, 1, 1, 1, 1, 8)
	selected.Value = strconv.FormatInt(moon, 10)
	page = getPage(t, handler, "/galaxy/1/1", session, csrf, selected)
	if row = galaxyRowOf(t, page, "1:1:1"); !strings.Contains(row, "Les missiles partent uniquement d’une planète") || strings.Contains(row, "/missiles?") {
		t.Fatalf("lunar origin enabled: %s", row)
	}
}
