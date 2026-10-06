package tests

import (
	"strings"
	"testing"
)

func TestEveryProgressionFamilyCarriesTechnologyDialogs(t *testing.T) {
	handler, _, session, csrfCookie := progressionHandler(t)
	tests := []struct {
		route string
		art   string
		id    string
	}{
		{route: "/planets/1", art: "art/building/metal_mine", id: "technology-building-metal_mine"},
		{route: "/planets/1/research", art: "art/research/energy_technology", id: "technology-research-energy_technology"},
		{route: "/planets/1/shipyard", art: "art/ship/small_cargo", id: "technology-ship-small_cargo"},
		{route: "/planets/1/defense", art: "art/defense/rocket_launcher", id: "technology-defense-rocket_launcher"},
	}
	for _, test := range tests {
		t.Run(test.route, func(t *testing.T) {
			card := cardOf(t, getPage(t, handler, test.route, session, csrfCookie), test.art)
			for _, expected := range []string{
				"aria-haspopup=\"dialog\"",
				"aria-controls=\"" + test.id + "\"",
				"<dialog class=\"technology-dialog\" id=\"" + test.id + "\"",
				"Informations", "Arbre tech",
				"data-technology-panel=\"info\"",
				"data-technology-panel=\"tree\"",
			} {
				if !strings.Contains(card, expected) {
					t.Fatalf("%s card misses %q: %q", test.route, expected, card)
				}
			}
		})
	}
}

func TestBuildingTechnologyDialogShowsFifteenCalculatedLevels(t *testing.T) {
	handler, _, session, csrfCookie := progressionHandler(t)
	page := getPage(t, handler, "/planets/1", session, csrfCookie)
	dialog := technologyDialogOf(t, page, "technology-building-metal_mine")

	if rows := strings.Count(dialog, "<tr"); rows != 16 {
		t.Fatalf("metal mine table has %d rows including its heading, want 16", rows)
	}
	for _, expected := range []string{
		"<th scope=\"col\">Niveau</th>",
		"<th scope=\"col\">Métal</th>",
		"<th scope=\"col\">Énergie</th>",
		"<th scope=\"col\">Effet</th>",
		">60<", ">15<", ">-11<", "&#43;33/h",
	} {
		if !strings.Contains(dialog, expected) {
			t.Fatalf("metal mine technology table misses %q: %q", expected, dialog)
		}
	}
}

func TestResearchTechnologyDialogShowsDependenciesUnlocksAndEffects(t *testing.T) {
	handler, _, session, csrfCookie := progressionHandler(t)
	page := getPage(t, handler, "/planets/1/research", session, csrfCookie)
	dialog := technologyDialogOf(t, page, "technology-research-computer_technology")

	for _, expected := range []string{
		"Laboratoire de recherche", "0 / 1",
		"Usine de nanites", "niveau 10",
		"Réseau de recherche intergalactique", "niveau 8",
		"2 flottes simultanées",
	} {
		if !strings.Contains(dialog, expected) {
			t.Fatalf("computer technology dialog misses %q: %q", expected, dialog)
		}
	}
}

func TestUnitTechnologyDialogKeepsTreeCombatStatsAndRapidFireTogether(t *testing.T) {
	handler, _, session, csrfCookie := progressionHandler(t)
	page := getPage(t, handler, "/planets/1/shipyard", session, csrfCookie)
	dialog := technologyDialogOf(t, page, "technology-ship-cruiser")

	for _, expected := range []string{
		"Chantier spatial", "0 / 5", "Réacteur à impulsion", "0 / 4",
		"Arme", "400", "Bouclier", "50", "Coque", "2.700",
		"Rapid fire infligé", "<span>Chasseur léger</span><b>×6</b>",
		"Rapid fire subi", "<span>Étoile de la mort</span><b>×33</b>",
	} {
		if !strings.Contains(dialog, expected) {
			t.Fatalf("cruiser technology dialog misses %q: %q", expected, dialog)
		}
	}
}

func technologyDialogOf(t *testing.T, page, id string) string {
	t.Helper()
	start := strings.Index(page, "<dialog class=\"technology-dialog\" id=\""+id+"\"")
	if start < 0 {
		t.Fatalf("page holds no technology dialog %s", id)
	}
	dialog := page[start:]
	end := strings.Index(dialog, "</dialog>")
	if end < 0 {
		t.Fatalf("technology dialog %s is never closed", id)
	}
	return dialog[:end]
}
