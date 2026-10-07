// Every build screen shows the same thing: an ordered list of orders, the first
// of which is running. This file holds that shared view and the one shape the
// template reads, so the three screens cannot drift apart again.
package web

import (
	"fmt"
	"net/http"
	"strconv"
	"time"

	appeconomy "universeatwar/internal/app/economy"
	appresearch "universeatwar/internal/app/research"
	appshipyard "universeatwar/internal/app/shipyard"
	domaineconomy "universeatwar/internal/domain/economy"
)

// queuePanel is what the "buildQueue" template renders.
type queuePanel struct {
	Eyebrow  string
	Title    string
	Entries  []queueEntryView
	Timezone string
	Location *time.Location
}

// queueEntryView is one line of a queue, whatever it builds. A running entry
// carries a real span for its bar and its countdown; a waiting one only carries
// the forecast of when its turn comes.
type queueEntryView struct {
	ID       int64
	Name     string
	Detail   string
	Active   bool
	StartsAt time.Time
	EndsAt   time.Time
	EndsISO  string
	// ElapsedSeconds and TotalSeconds fill the progress attributes. The policy
	// refuses inline styles, so the bar is filled by value, never by CSS.
	ElapsedSeconds int
	TotalSeconds   int
	Progress       string
	Delivered      int64
	Quantity       int64
	CancelPath     string
	CSRFToken      string
	// Family tells the unit cancellation which of the two production screens
	// to come back to. It stays empty on the other queues.
	Family string
	// Overflow warns that the stores are too full to take the whole refund
	// back, so the player decides knowing what cancelling costs.
	Overflow bool
}

// queueBuilder carries what every entry of one screen needs: where its cancel
// form posts, the token that form carries, and the stores the refund goes back
// into.
type queueBuilder struct {
	cancelPrefix string
	token        string
	stock        domaineconomy.Resources
	capacity     domaineconomy.Resources
	now          time.Time
}

func (b queueBuilder) running(id int64, name, detail string, cost domaineconomy.Resources, startedAt, completesAt time.Time) queueEntryView {
	total := int(completesAt.Sub(startedAt) / time.Second)
	if total < 1 {
		total = 1
	}
	elapsed := min(max(int(b.now.Sub(startedAt)/time.Second), 0), total)
	view := b.entry(id, name, detail, cost)
	view.Active = true
	view.StartsAt = startedAt
	view.EndsAt = completesAt
	view.EndsISO = instant(completesAt)
	view.ElapsedSeconds, view.TotalSeconds = elapsed, total
	view.Progress = fmt.Sprintf("%d %%", elapsed*100/total)
	return view
}

func (b queueBuilder) waiting(id int64, name, detail string, cost domaineconomy.Resources, startsAt, completesAt time.Time) queueEntryView {
	view := b.entry(id, name, detail, cost)
	view.StartsAt = startsAt
	view.EndsAt = completesAt
	return view
}

func (b queueBuilder) entry(id int64, name, detail string, cost domaineconomy.Resources) queueEntryView {
	_, lost := b.stock.Refund(cost, b.capacity)
	return queueEntryView{
		ID: id, Name: name, Detail: detail,
		CancelPath: fmt.Sprintf("%s/%d/cancel", b.cancelPrefix, id),
		CSRFToken:  b.token,
		Overflow:   lost != domaineconomy.Resources{},
	}
}

// buildingQueuePanel turns a body's construction queue into the shared view.
func buildingQueuePanel(planet appeconomy.Planet, token string, now time.Time, timezone string, location *time.Location) queuePanel {
	builder := queueBuilder{
		cancelPrefix: fmt.Sprintf("/planets/%d/queue/building", planet.ID),
		token:        token, stock: planet.Stock, capacity: planet.Capacity, now: now,
	}
	panel := queuePanel{Eyebrow: "File de construction", Title: "Construction en cours", Timezone: timezone, Location: location}
	for _, entry := range planet.Queue {
		detail := fmt.Sprintf("niveau %d", entry.TargetLevel)
		name := buildingName(entry.Building)
		if entry.Waiting() {
			panel.Entries = append(panel.Entries, builder.waiting(entry.ID, name, detail, entry.Cost, entry.EstimatedStartAt, entry.EstimatedCompletesAt))
			continue
		}
		panel.Entries = append(panel.Entries, builder.running(entry.ID, name, detail, entry.Cost, entry.StartedAt, entry.CompletesAt))
	}
	return panel
}

// researchQueuePanel turns a player's research queue into the shared view.
func researchQueuePanel(queue []appresearch.Queue, planet appeconomy.Planet, token string, now time.Time, timezone string, location *time.Location) queuePanel {
	builder := queueBuilder{
		cancelPrefix: fmt.Sprintf("/planets/%d/queue/research", planet.ID),
		token:        token, stock: planet.Stock, capacity: planet.Capacity, now: now,
	}
	panel := queuePanel{Eyebrow: "File de recherche", Title: "Recherche en cours", Timezone: timezone, Location: location}
	for _, entry := range queue {
		detail := fmt.Sprintf("niveau %d", entry.TargetLevel)
		name := researchName(entry.Research)
		if entry.Waiting() {
			panel.Entries = append(panel.Entries, builder.waiting(entry.ID, name, detail, entry.Cost, entry.EstimatedStartAt, entry.EstimatedCompletesAt))
			continue
		}
		panel.Entries = append(panel.Entries, builder.running(entry.ID, name, detail, entry.Cost, entry.StartedAt, entry.CompletesAt))
	}
	return panel
}

// productionQueuePanel turns one family's queue into the shared view. A running
// batch also states how many of its units have already left the yard.
func productionQueuePanel(queue []appshipyard.Order, planet appeconomy.Planet, token string, now time.Time, timezone string, location *time.Location) queuePanel {
	builder := queueBuilder{
		cancelPrefix: fmt.Sprintf("/planets/%d/queue/unit", planet.ID),
		token:        token, stock: planet.Stock, capacity: planet.Capacity, now: now,
	}
	panel := queuePanel{Eyebrow: "File de production", Title: "Production en cours", Timezone: timezone, Location: location}
	for _, order := range queue {
		detail := fmt.Sprintf("× %d", order.Quantity)
		name := unitName(order.Unit)
		owed := economyTimes(order.UnitCost, order.Quantity-order.Delivered)
		if order.Waiting() {
			view := builder.waiting(order.ID, name, detail, owed, order.EstimatedStartAt, order.EstimatedCompletesAt)
			view.Family = string(order.Family)
			panel.Entries = append(panel.Entries, view)
			continue
		}
		view := builder.running(order.ID, name, detail, owed, order.StartedAt, order.CompletesAt)
		view.Delivered, view.Quantity = order.Delivered, order.Quantity
		view.Family = string(order.Family)
		panel.Entries = append(panel.Entries, view)
	}
	return panel
}

// economyTimes multiplies a unit cost by a quantity, for the refund a
// cancellation would return.
func economyTimes(cost domaineconomy.Resources, quantity int64) domaineconomy.Resources {
	if quantity <= 0 {
		return domaineconomy.Resources{}
	}
	return domaineconomy.Resources{
		Metal: cost.Metal * quantity, Crystal: cost.Crystal * quantity, Deuterium: cost.Deuterium * quantity,
	}
}

// cancellationNotice turns what a cancellation gave back into one sentence, and
// carries it across the redirect in the query rather than in a cookie.
func cancellationQuery(cancellation appeconomy.Cancellation) string {
	query := fmt.Sprintf("?cancelled=%d", cancellation.Cancelled)
	if cancellation.Lost != (domaineconomy.Resources{}) {
		query += fmt.Sprintf("&lost_metal=%d&lost_crystal=%d&lost_deuterium=%d",
			cancellation.Lost.Metal, cancellation.Lost.Crystal, cancellation.Lost.Deuterium)
	}
	return query
}

// cancellationNotice reads that query back on the page the redirect landed on.
func cancellationNotice(request *http.Request) string {
	cancelled, err := strconv.Atoi(request.URL.Query().Get("cancelled"))
	if err != nil || cancelled <= 0 {
		return ""
	}
	notice := fmt.Sprintf("%d ordre annulé et remboursé.", cancelled)
	if cancelled > 1 {
		notice = fmt.Sprintf("%d ordres annulés et remboursés.", cancelled)
	}
	lost := lostFromQuery(request)
	if lost == (domaineconomy.Resources{}) {
		return notice
	}
	return notice + " Entrepôts pleins : " + resourceList(lost) + " perdus."
}

func lostFromQuery(request *http.Request) domaineconomy.Resources {
	query := request.URL.Query()
	amount := func(name string) int64 {
		value, err := strconv.ParseInt(query.Get(name), 10, 64)
		if err != nil || value < 0 {
			return 0
		}
		return value
	}
	return domaineconomy.Resources{
		Metal: amount("lost_metal"), Crystal: amount("lost_crystal"), Deuterium: amount("lost_deuterium"),
	}
}

// resourceList names the non-zero parts of an amount in the player's language.
func resourceList(amount domaineconomy.Resources) string {
	parts := make([]string, 0, 3)
	for _, pair := range []struct {
		value int64
		label string
	}{{amount.Metal, "métal"}, {amount.Crystal, "cristal"}, {amount.Deuterium, "deutérium"}} {
		if pair.value > 0 {
			parts = append(parts, fmt.Sprintf("%s %s", figure(pair.value), pair.label))
		}
	}
	return joinWithAnd(parts)
}

func joinWithAnd(parts []string) string {
	switch len(parts) {
	case 0:
		return ""
	case 1:
		return parts[0]
	default:
		joined := parts[0]
		for _, part := range parts[1 : len(parts)-1] {
			joined += ", " + part
		}
		return joined + " et " + parts[len(parts)-1]
	}
}
