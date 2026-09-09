package tests

import (
	"context"
	"regexp"
	"strings"
	"testing"
)

// The shipyard works out how many of a ship the stores cover and writes it on
// the field itself. Typing that number back in by hand was the only way to
// order it, so the page offers to fill the field with its own ceiling.
func TestTheShipyardOffersItsMaximumInOneClick(t *testing.T) {
	handler, database, session, csrfCookie := progressionHandler(t)
	ctx := context.Background()
	setBuilding(t, ctx, database, 1, "shipyard", 1)
	setResearch(t, ctx, database, 1, "combustion_drive", 1)
	setResources(t, ctx, database, 1, 30_000, 10_000, 0)

	card := cardOf(t, getPage(t, handler, "/planets/1/shipyard", session, csrfCookie), "art/ship/light_fighter")
	if !strings.Contains(card, `data-max-for="quantity-light_fighter"`) {
		t.Fatalf("the card offers no way to reach its maximum: %q", card)
	}
	// The button fills the field from the field's own ceiling, so the two figures
	// the card carries have to be the same one.
	announced := betweenMarkers(card, "Quantité (max ", ")")
	ceiling := betweenMarkers(card, `id="quantity-light_fighter"`, ">")
	ceiling = betweenMarkers(ceiling, `max="`, `"`)
	if announced == "" || announced != ceiling {
		t.Fatalf("the card announces %q and bounds the field at %q: %q", announced, ceiling, card)
	}
}

// A card the server disabled for want of resources is lifted by the page the
// moment the stores catch up, and the same tick shrinks the ceiling of its
// field. A button freed any earlier would fill in a ceiling nobody can pay.
func TestAMaximumButtonWaitsWithTheFormItBelongsTo(t *testing.T) {
	handler, database, session, csrfCookie := progressionHandler(t)
	ctx := context.Background()
	setBuilding(t, ctx, database, 1, "shipyard", 1)
	setResearch(t, ctx, database, 1, "combustion_drive", 1)
	setResources(t, ctx, database, 1, 10, 10, 0)

	card := cardOf(t, getPage(t, handler, "/planets/1/shipyard", session, csrfCookie), "art/ship/light_fighter")
	button := betweenMarkers(card, `<button type="button" class="ghost" data-max-for`, ">")
	if !strings.Contains(button, "disabled") {
		t.Fatalf("a maximum button is offered on a card that cannot be paid for: %q", card)
	}
}

// A button that only works with a script must not be rendered to a page that
// has none: it would be a control that does nothing. Every one of them is
// rendered away and revealed by the script, and this sweep is what keeps the
// promise the head of app.js makes.
func TestNoMaximumButtonIsRenderedVisible(t *testing.T) {
	handler, database, session, csrfCookie := progressionHandler(t)
	ctx := context.Background()
	setBuilding(t, ctx, database, 1, "shipyard", 1)
	setResearch(t, ctx, database, 1, "combustion_drive", 1)
	setResources(t, ctx, database, 1, 100_000, 100_000, 100_000)

	opening := regexp.MustCompile(`<button[^>]*data-max-for[^>]*>`)
	for _, route := range []string{"/planets/1/shipyard", "/planets/1/defense"} {
		page := getPage(t, handler, route, session, csrfCookie)
		found := opening.FindAllString(page, -1)
		if len(found) == 0 {
			t.Fatalf("%s offers no maximum at all", route)
		}
		for _, tag := range found {
			if !strings.Contains(tag, " hidden") {
				t.Fatalf("%s renders a maximum button a page without a script would see: %s", route, tag)
			}
		}
	}
}
