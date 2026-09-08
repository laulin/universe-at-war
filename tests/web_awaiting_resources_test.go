package tests

import (
	"context"
	"strings"
	"testing"
)

// The bar keeps climbing between two loads while a card keeps the verdict it was
// rendered with, so the two contradict each other: the bar says the price is
// met and the card still refuses. A card whose only shortfall is the stock now
// carries its price and a disabled button, which the page can lift on its own.
func TestABuildingWaitingOnlyForResourcesOffersADisabledForm(t *testing.T) {
	handler, _, session, csrfCookie := progressionHandler(t)

	card := cardOf(t, getPage(t, handler, "/planets/1", session, csrfCookie), "metal_storage")
	if !strings.Contains(card, `data-cost-metal="1000"`) {
		t.Fatalf("the card does not carry its price: %q", card)
	}
	if !strings.Contains(card, "<button type=\"submit\" disabled>") {
		t.Fatalf("the card does not offer a disabled button: %q", card)
	}
}

// A prerequisite is not a matter of waiting, so nothing on that card may ever
// be lifted by the page: it carries no price and no form at all.
func TestABuildingWaitingOnAPrerequisiteOffersNoForm(t *testing.T) {
	handler, _, session, csrfCookie := progressionHandler(t)

	card := cardOf(t, getPage(t, handler, "/planets/1", session, csrfCookie), "nanite_factory")
	if strings.Contains(card, "<form") || strings.Contains(card, "data-cost-metal") {
		t.Fatalf("a card blocked by a prerequisite offers a form: %q", card)
	}
	if !strings.Contains(card, "Prérequis manquants") {
		t.Fatalf("the card does not say what it waits for: %q", card)
	}
}

// The shipyard states the ceiling of an order alongside the price, because the
// page has to work out how many units the stock covers, not merely whether it
// covers one.
func TestAUnitWaitingOnlyForResourcesCarriesThePriceAndTheCeiling(t *testing.T) {
	handler, database, session, csrfCookie := progressionHandler(t)
	setBuilding(t, context.Background(), database, 1, "shipyard", 1)
	setResearch(t, context.Background(), database, 1, "combustion_drive", 1)
	setResources(t, context.Background(), database, 1, 10, 10, 0)

	card := cardOf(t, getPage(t, handler, "/planets/1/shipyard", session, csrfCookie), "light_fighter")
	if !strings.Contains(card, `data-cost-metal="3000"`) || !strings.Contains(card, `data-cost-crystal="1000"`) {
		t.Fatalf("the card does not carry its price: %q", card)
	}
	if !strings.Contains(card, `data-ceiling="1000000"`) {
		t.Fatalf("the card does not carry the ceiling of an order: %q", card)
	}
}

// Energy is not a store and never climbs on its own, so a research short of it
// must not be liftable by the page however long it waits. The graviton costs
// nothing but energy, which makes it the whole of that case.
func TestAResearchShortOfEnergyCarriesNoPrice(t *testing.T) {
	handler, database, session, csrfCookie := progressionHandler(t)
	setBuilding(t, context.Background(), database, 1, "research_lab", 12)

	card := cardOf(t, getPage(t, handler, "/planets/1/research", session, csrfCookie), "graviton_technology")
	if strings.Contains(card, "data-cost-") {
		t.Fatalf("a research short of energy could be lifted by the page: %q", card)
	}
}

// cardOf cuts one card out of a page, from its picture to the end of the
// article that holds it.
func cardOf(t *testing.T, page, id string) string {
	t.Helper()
	marker := "/" + id + `"`
	at := strings.Index(page, marker)
	if at < 0 {
		t.Fatalf("the page holds no card for %s", id)
	}
	start := strings.LastIndex(page[:at], "<article")
	end := strings.Index(page[at:], "</article>")
	if start < 0 || end < 0 {
		t.Fatalf("the card of %s is not an article", id)
	}
	return page[start : at+end]
}
