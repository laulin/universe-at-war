package tests

import (
	"context"
	"fmt"
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
	if !strings.Contains(row, "/fleet/send?galaxy=1&amp;system=1&amp;position=3") {
		t.Fatalf("the map offers no fleet to a planet of the player: %q", row)
	}
	if strings.Contains(row, "Espionner") {
		t.Fatalf("the map offers to spy a planet of the player: %q", row)
	}
	if second <= 0 {
		t.Fatal("the second planet was not created")
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
	start := strings.LastIndex(page[:at], "<tr>")
	end := strings.Index(page[at:], "</tr>")
	if start < 0 || end < 0 {
		t.Fatalf("the row of %s is not a row", coordinate)
	}
	return page[start : at+end]
}
