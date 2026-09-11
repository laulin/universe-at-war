package tests

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	storagesqlite "universeatwar/internal/storage/sqlite"
)

// The map offered nothing at all on the player's own worlds: the actions column
// only opened for somebody else's. Moving resources or ships between one's own
// planets is the most ordinary mission there is, and it had to be composed by
// hand from the fleet page.
func TestTheMapOffersToSendAFleetToOwnPlanets(t *testing.T) {
	handler, database, _, session, csrfCookie := intelligenceHandler(t)
	ctx := context.Background()
	second := insertPlanet(t, ctx, database, 1, "Colonie", 1, 1, 3)

	page := getPage(t, handler, "/galaxy/1/1", session, csrfCookie)
	row := galaxyRowOf(t, page, "1:1:3")
	for _, action := range []string{"mission=deploy", "mission=transport", "Stationner", "Transporter"} {
		if !strings.Contains(row, action) {
			t.Fatalf("the map offers no %s action to a planet of the player: %q", action, row)
		}
	}
	for _, hostile := range []string{"Espionner", "Attaquer"} {
		if strings.Contains(row, hostile) {
			t.Fatalf("the map offers to %s a planet of the player: %q", hostile, row)
		}
	}
	if second <= 0 {
		t.Fatal("the second planet was not created")
	}
}

func TestTheMapOffersToColonizeEmptyPositions(t *testing.T) {
	handler, database, _, session, csrfCookie := intelligenceHandler(t)

	page := getPage(t, handler, "/galaxy/1/1", session, csrfCookie)
	empty := galaxyRowOf(t, page, "1:1:4")
	for _, expected := range []string{
		"inoccupée", ">Coloniser</a>",
		`/planets/1/fleet/send?galaxy=1&amp;system=1&amp;position=4&amp;mission=colonize`,
	} {
		if !strings.Contains(empty, expected) {
			t.Fatalf("the empty position misses %s: %q", expected, empty)
		}
	}

	colonization := getPage(t, handler,
		"/planets/1/fleet/send?galaxy=1&system=1&position=4&mission=colonize",
		session, csrfCookie)
	for _, expected := range []string{
		`name="galaxy" type="number" min="1" value="1"`,
		`name="system" type="number" min="1" value="1"`,
		`name="position" type="number" min="1" value="4"`,
		`value="colonize" selected`,
	} {
		if !strings.Contains(colonization, expected) {
			t.Fatalf("the colonization shortcut did not prefill %s: %q", expected, colonization)
		}
	}

	for _, occupied := range []string{"1:1:1", homeCoordinate(t, context.Background(), database, 1)} {
		row := galaxyRowOf(t, page, occupied)
		if strings.Contains(row, "Coloniser") || strings.Contains(row, "mission=colonize") {
			t.Fatalf("the occupied position %s offers colonization: %q", occupied, row)
		}
	}
}

func TestTheMapNavigatesDirectlyToAnEditableSystem(t *testing.T) {
	handler, _, _, session, csrfCookie := intelligenceHandler(t)
	page := getPage(t, handler, "/galaxy/1/1", session, csrfCookie)
	form := betweenMarkers(page, `class="galaxy-coordinate"`, "</form>")
	for _, field := range []string{`name="galaxy"`, `name="system"`, `value="1"`, `>Afficher</button>`} {
		if !strings.Contains(form, field) {
			t.Fatalf("the coordinate form misses %s: %q", field, form)
		}
	}

	request := httptest.NewRequest(http.MethodGet, "/galaxy?galaxy=1&system=2", nil)
	request.AddCookie(session)
	request.AddCookie(csrfCookie)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusSeeOther || response.Header().Get("Location") != "/galaxy/1/2" {
		t.Fatalf("GET editable coordinates = %d %q", response.Code, response.Header().Get("Location"))
	}

	outside := httptest.NewRequest(http.MethodGet, "/galaxy?galaxy=1&system=0", nil)
	outside.AddCookie(session)
	outside.AddCookie(csrfCookie)
	refused := httptest.NewRecorder()
	handler.ServeHTTP(refused, outside)
	if refused.Code != http.StatusBadRequest {
		t.Fatalf("GET outside coordinates = %d, want 400", refused.Code)
	}
}

func TestGalaxyShortcutsPrefillMissionsAndEveryRecycler(t *testing.T) {
	handler, database, _, session, csrfCookie := intelligenceHandler(t)
	ctx := context.Background()
	setUnits(t, ctx, database, 1, "recycler", 7)
	if _, err := database.Write().ExecContext(ctx, `
		INSERT INTO debris_fields(galaxy, system, position, metal, crystal, created_at, updated_at)
		VALUES (1, 1, 1, 12345, 6789, '2042-09-10T11:12:13Z', '2042-09-10T11:12:13Z')
	`); err != nil {
		t.Fatal(err)
	}

	page := getPage(t, handler, "/galaxy/1/1", session, csrfCookie)
	row := galaxyRowOf(t, page, "1:1:1")
	for _, expected := range []string{
		`mission=espionage`, `>Espionner</a>`, `mission=attack`, `>Attaquer</a>`,
		`/art/resource/debris`, `mission=recycle&amp;select=recycler`, `M 12.345`, `C 6.789`,
	} {
		if !strings.Contains(row, expected) {
			t.Fatalf("the target row misses %s: %q", expected, row)
		}
	}

	recycle := getPage(t, handler,
		"/planets/1/fleet/send?galaxy=1&system=1&position=1&mission=recycle&select=recycler",
		session, csrfCookie)
	if !strings.Contains(recycle, `value="recycle" selected`) {
		t.Fatalf("the debris shortcut did not select recycling: %q", recycle)
	}
	quantity := betweenMarkers(recycle, `id="ship-recycler"`, ">")
	if !strings.Contains(quantity, `value="7"`) {
		t.Fatalf("the debris shortcut selected something other than every recycler: %q", quantity)
	}
}

func TestGalaxyActionsLeaveFromTheSelectedBody(t *testing.T) {
	handler, database, _, session, csrfCookie := intelligenceHandler(t)
	ctx := context.Background()
	second := insertPlanet(t, ctx, database, 1, "Colonie", 1, 1, 3)
	page := getPage(t, handler, "/galaxy/1/1", session, csrfCookie,
		&http.Cookie{Name: "uaw_body", Value: strconv.FormatInt(second, 10)})
	foreign := galaxyRowOf(t, page, "1:1:1")
	if !strings.Contains(foreign, fmt.Sprintf(`/planets/%d/fleet/send`, second)) {
		t.Fatalf("the map ignored the selected origin: %q", foreign)
	}
	selected := galaxyRowOf(t, page, "1:1:3")
	if strings.Contains(selected, "/fleet/send") {
		t.Fatalf("the selected body offers to fly to itself: %q", selected)
	}
	empty := galaxyRowOf(t, page, "1:1:4")
	if !strings.Contains(empty, fmt.Sprintf(`/planets/%d/fleet/send`, second)) ||
		!strings.Contains(empty, "mission=colonize") {
		t.Fatalf("the colonization shortcut ignored the selected origin: %q", empty)
	}
}

// The body the actions fly from has nothing to offer itself. The domain does
// allow a fleet to reach its own coordinate, so this is an interface choice
// rather than a rule, and it must not take the debris link away with it.
func TestTheMapOffersNoFleetFromABodyToItself(t *testing.T) {
	handler, database, _, session, csrfCookie := intelligenceHandler(t)
	ctx := context.Background()
	home := homeCoordinate(t, ctx, database, 1)

	page := getPage(t, handler, "/galaxy/1/1", session, csrfCookie)
	row := galaxyRowOf(t, page, home)
	if strings.Contains(row, "fleet/send") {
		t.Fatalf("the map offers to send a fleet from a body to itself: %q", row)
	}
}

// insertPlanet puts a second world of a player on the map, which the wizard
// never creates by itself in these fixtures.
func insertPlanet(t *testing.T, ctx context.Context, database *storagesqlite.Database,
	playerID int64, name string, galaxy, system, position int) int64 {
	t.Helper()
	result, err := database.Write().ExecContext(ctx, `
		INSERT INTO planets(owner_player_id, kind, name, galaxy, system, position,
			total_fields, minimum_temperature, maximum_temperature, created_at)
		VALUES (?, 'planet', ?, ?, ?, ?, 163, 10, 50, '2042-09-10T11:12:13Z')
	`, playerID, name, galaxy, system, position)
	if err != nil {
		t.Fatalf("insert planet: %v", err)
	}
	planetID, err := result.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.Write().ExecContext(ctx,
		"INSERT INTO planet_resources(planet_id, metal, crystal, deuterium, produced_at) VALUES (?, 0, 0, 0, '2042-09-10T11:12:13Z')",
		planetID); err != nil {
		t.Fatalf("insert planet resources: %v", err)
	}
	return planetID
}

// homeCoordinate is where the actions of the map fly from: the oldest world of
// the player, which is what the page picks as their origin.
func homeCoordinate(t *testing.T, ctx context.Context, database *storagesqlite.Database, playerID int64) string {
	t.Helper()
	var galaxy, system, position int
	if err := database.Read().QueryRowContext(ctx,
		"SELECT galaxy, system, position FROM planets WHERE owner_player_id = ? ORDER BY id LIMIT 1",
		playerID).Scan(&galaxy, &system, &position); err != nil {
		t.Fatalf("read home coordinate: %v", err)
	}
	return fmt.Sprintf("%d:%d:%d", galaxy, system, position)
}

func galaxyRowOf(t *testing.T, page, coordinate string) string {
	t.Helper()
	at := strings.Index(page, ">"+coordinate+"<")
	if at < 0 {
		t.Fatalf("the map holds no row for %s", coordinate)
	}
	start := strings.LastIndex(page[:at], "<tr")
	end := strings.Index(page[at:], "</tr>")
	if start < 0 || end < 0 {
		t.Fatalf("the row of %s is not a row", coordinate)
	}
	return page[start : at+end]
}
