// Every build screen shows the same thing: an ordered list of orders, the first
// of which is running. This file holds that shared view and the one shape the
// template reads, so the three screens cannot drift apart again.
package web

import (
	"fmt"
	"time"

	appeconomy "universeatwar/internal/app/economy"
	appresearch "universeatwar/internal/app/research"
	appshipyard "universeatwar/internal/app/shipyard"
)

// queuePanel is what the "buildQueue" template renders.
type queuePanel struct {
	Eyebrow string
	Title   string
	Entries []queueEntryView
}

// queueEntryView is one line of a queue, whatever it builds. A running entry
// carries a real span for its bar and its countdown; a waiting one only carries
// the forecast of when its turn comes.
type queueEntryView struct {
	ID       int64
	Name     string
	Detail   string
	Active   bool
	StartsAt string
	EndsAt   string
	EndsISO  string
	// ElapsedSeconds and TotalSeconds fill the progress attributes. The policy
	// refuses inline styles, so the bar is filled by value, never by CSS.
	ElapsedSeconds int
	TotalSeconds   int
	Progress       string
	Delivered      int64
	Quantity       int64
}

// runningEntry describes the order being built.
func runningEntry(id int64, name, detail string, startedAt, completesAt, now time.Time) queueEntryView {
	total := int(completesAt.Sub(startedAt) / time.Second)
	if total < 1 {
		total = 1
	}
	elapsed := int(now.Sub(startedAt) / time.Second)
	elapsed = min(max(elapsed, 0), total)
	return queueEntryView{
		ID: id, Name: name, Detail: detail, Active: true,
		StartsAt: startedAt.Format(clockLayout), EndsAt: completesAt.Format(clockLayout),
		EndsISO: completesAt.Format(time.RFC3339), ElapsedSeconds: elapsed, TotalSeconds: total,
		Progress: fmt.Sprintf("%d %%", elapsed*100/total),
	}
}

// waitingEntry describes an order whose turn has not come, with the forecast
// the player is shown instead of a deadline.
func waitingEntry(id int64, name, detail string, startsAt, completesAt time.Time) queueEntryView {
	return queueEntryView{
		ID: id, Name: name, Detail: detail,
		StartsAt: startsAt.Format(clockLayout), EndsAt: completesAt.Format(clockLayout),
	}
}

const clockLayout = "15:04:05"

// buildingQueuePanel turns a body's construction queue into the shared view.
func buildingQueuePanel(planet appeconomy.Planet, now time.Time) queuePanel {
	panel := queuePanel{Eyebrow: "File de construction", Title: "Construction en cours"}
	for _, entry := range planet.Queue {
		detail := fmt.Sprintf("niveau %d", entry.TargetLevel)
		name := buildingName(entry.Building)
		if entry.Waiting() {
			panel.Entries = append(panel.Entries, waitingEntry(entry.ID, name, detail, entry.EstimatedStartAt, entry.EstimatedCompletesAt))
			continue
		}
		panel.Entries = append(panel.Entries, runningEntry(entry.ID, name, detail, entry.StartedAt, entry.CompletesAt, now))
	}
	return panel
}

// researchQueuePanel turns a player's research queue into the shared view.
func researchQueuePanel(queue []appresearch.Queue, now time.Time) queuePanel {
	panel := queuePanel{Eyebrow: "File de recherche", Title: "Recherche en cours"}
	for _, entry := range queue {
		detail := fmt.Sprintf("niveau %d", entry.TargetLevel)
		name := researchName(entry.Research)
		if entry.Waiting() {
			panel.Entries = append(panel.Entries, waitingEntry(entry.ID, name, detail, entry.EstimatedStartAt, entry.EstimatedCompletesAt))
			continue
		}
		panel.Entries = append(panel.Entries, runningEntry(entry.ID, name, detail, entry.StartedAt, entry.CompletesAt, now))
	}
	return panel
}

// productionQueuePanel turns one family's queue into the shared view. A batch
// also states how many of its units have already left the yard.
func productionQueuePanel(queue []appshipyard.Order, now time.Time) queuePanel {
	panel := queuePanel{Eyebrow: "File de production", Title: "Production en cours"}
	for _, order := range queue {
		detail := fmt.Sprintf("× %d", order.Quantity)
		name := unitName(order.Unit)
		var view queueEntryView
		if order.Waiting() {
			view = waitingEntry(order.ID, name, detail, order.EstimatedStartAt, order.EstimatedCompletesAt)
		} else {
			view = runningEntry(order.ID, name, detail, order.StartedAt, order.CompletesAt, now)
			view.Delivered, view.Quantity = order.Delivered, order.Quantity
		}
		panel.Entries = append(panel.Entries, view)
	}
	return panel
}
