package tests

import (
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// A counter has to keep climbing between two page loads, so every figure the
// interface refreshes carries what the extrapolation needs: the stock the
// server rendered, the hourly rate, and the ceiling that stops it.
func TestLiveFiguresCarryStockRateAndCeiling(t *testing.T) {
	handler, session, csrfCookie := playableHandler(t)
	page := getPage(t, handler, "/planets/1", session, csrfCookie)

	bar := `<span class="stat-value res-metal`
	if stock := number(t, page, bar, "data-stock"); stock < 0 {
		t.Fatalf("the bar carries a negative stock: %d", stock)
	}
	if rate := number(t, page, bar, "data-rate"); rate <= 0 {
		t.Fatalf("the bar carries no production rate: %d", rate)
	}
	if ceiling := number(t, page, bar, "data-cap"); ceiling <= 0 {
		t.Fatalf("the bar carries no ceiling: %d", ceiling)
	}
}

// Energy is a balance, not a store: it does not accrue, so nothing must make it
// climb.
func TestTheEnergyBalanceIsNotALiveCounter(t *testing.T) {
	handler, session, csrfCookie := playableHandler(t)
	page := getPage(t, handler, "/planets/1", session, csrfCookie)

	energy := openingTag(t, page, `<span class="stat-value res-energy`)
	if strings.Contains(energy, "data-stock") {
		t.Fatalf("the energy balance is treated as a store: %q", energy)
	}
}

// The totals stand next to the bar, so they have to climb at the same pace or
// the two would contradict each other on screen. The empire earns what its
// bodies earn.
func TestEmpireTotalsClimbAtTheRateOfTheirBodies(t *testing.T) {
	handler, session, csrfCookie := playableHandler(t)
	page := getPage(t, handler, "/planets/1", session, csrfCookie)

	body := number(t, page, `<span class="stat-value res-metal`, "data-rate")
	totals := number(t, page, `<dd class="res-metal"`, "data-rate")
	if totals != body {
		t.Fatalf("the empire earns %d metal an hour while its only body earns %d", totals, body)
	}
}

// openingTag returns the opening tag of the first element the marker names.
func openingTag(t *testing.T, page, marker string) string {
	t.Helper()
	start := strings.Index(page, marker)
	if start < 0 {
		t.Fatalf("the page holds no %q: %q", marker, page)
	}
	tag := page[start:]
	if end := strings.Index(tag, ">"); end >= 0 {
		tag = tag[:end+1]
	}
	return tag
}

// number reads one integer attribute off the element the marker names.
func number(t *testing.T, page, marker, attribute string) int64 {
	t.Helper()
	tag := openingTag(t, page, marker)
	match := regexp.MustCompile(attribute + `="(-?\d+)"`).FindStringSubmatch(tag)
	if match == nil {
		t.Fatalf("%s carries no %s: %q", marker, attribute, tag)
	}
	value, err := strconv.ParseInt(match[1], 10, 64)
	if err != nil {
		t.Fatal(err)
	}
	return value
}
