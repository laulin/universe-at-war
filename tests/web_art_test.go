package tests

import (
	"io/fs"
	"net/http"
	"net/http/httptest"
	"path"
	"strings"
	"testing"

	"universeatwar/internal/domain/building"
	"universeatwar/internal/domain/unit"
	webhandler "universeatwar/internal/web"
	webassets "universeatwar/web"
)

// An illustration slot always answers, whether artwork exists for it or not, so
// a screen is never left with a broken picture.
func TestArtSlotsAlwaysRenderAnImage(t *testing.T) {
	handler := artHandler(t)

	for _, target := range []string{
		"/art/building/metal_mine", "/art/research/astrophysics", "/art/ship/light_fighter",
		"/art/defense/rocket_launcher", "/art/resource/metal", "/art/resource/crystal",
		"/art/resource/deuterium", "/art/resource/energy", "/art/resource/debris", "/art/body/planet",
		"/art/body/moon", "/art/banner/overview", "/art/banner/shipyard",
	} {
		recorder := fetch(handler, target)
		if recorder.Code != http.StatusOK {
			t.Fatalf("GET %s = %d", target, recorder.Code)
		}
		if contentType := recorder.Header().Get("Content-Type"); !strings.Contains(contentType, "image/") {
			t.Fatalf("GET %s content type = %q", target, contentType)
		}
		if recorder.Body.Len() == 0 {
			t.Fatalf("GET %s returned an empty body", target)
		}
	}
}

// A slot no artwork has reached yet still answers, with a placeholder cached for
// a day rather than for a year: the next build may fill it.
func TestArtDrawsAPlaceholderForAnEmptySlot(t *testing.T) {
	recorder := fetch(artHandler(t), "/art/building/test_placeholder")
	if recorder.Code != http.StatusOK {
		t.Fatalf("GET /art/building/test_placeholder = %d", recorder.Code)
	}
	if body := recorder.Body.String(); !strings.HasPrefix(body, "<svg") {
		t.Fatalf("an empty slot did not draw a placeholder: %q", body)
	}
	if cache := recorder.Header().Get("Cache-Control"); cache != "public, max-age=86400" {
		t.Fatalf("placeholder cache control = %q", cache)
	}
}

// The resource page exposes every planetary building, so each of those slots
// must contain real artwork. Lunar buildings may keep their placeholders until
// their own masters arrive, but any embedded building picture must still name
// an entry from the catalogue.
func TestEveryPlanetBuildingOfTheCatalogueIsIllustrated(t *testing.T) {
	handler := artHandler(t)
	catalogue := building.DefaultCatalogue()
	known := map[string]bool{}

	for _, definition := range catalogue.Definitions() {
		slug := string(definition.ID)
		known[slug] = true
		if definition.Placement != building.OnPlanet {
			continue
		}
		recorder := fetch(handler, "/art/building/"+slug)
		if recorder.Code != http.StatusOK {
			t.Fatalf("GET /art/building/%s = %d", slug, recorder.Code)
		}
		if contentType := recorder.Header().Get("Content-Type"); contentType != "image/webp" {
			t.Fatalf("/art/building/%s served %q: the slot has no artwork", slug, contentType)
		}
		if cache := recorder.Header().Get("Cache-Control"); !strings.Contains(cache, "immutable") {
			t.Fatalf("/art/building/%s cache control = %q", slug, cache)
		}
	}

	entries, err := fs.ReadDir(webassets.Files, "static/art/building")
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		slug := strings.TrimSuffix(entry.Name(), path.Ext(entry.Name()))
		if !known[slug] {
			t.Fatalf("static/art/building/%s fills no slot of the catalogue", entry.Name())
		}
	}
}

// The same slot must always draw the same picture, or the interface would
// flicker between two requests.
func TestArtPlaceholdersAreDeterministic(t *testing.T) {
	handler := artHandler(t)
	first := fetch(handler, "/art/building/metal_mine").Body.String()
	second := fetch(handler, "/art/building/metal_mine").Body.String()
	if first != second {
		t.Fatal("the same slot drew two different placeholders")
	}
	if other := fetch(handler, "/art/building/crystal_mine").Body.String(); other == first {
		t.Fatal("two different slots drew the same placeholder")
	}
}

// A missing illustration is invisible: the slot quietly falls back to a
// placeholder and no screen ever says so. The shipyard is the first family to
// be illustrated in full, so the catalogue and the folder are held against each
// other in both directions.
func TestEveryShipOfTheCatalogueIsIllustrated(t *testing.T) {
	handler := artHandler(t)

	illustrated := map[string]bool{}
	for _, definition := range unit.DefaultCatalogue().Definitions(unit.Ship) {
		slug := string(definition.ID)
		illustrated[slug] = true
		recorder := fetch(handler, "/art/ship/"+slug)
		if recorder.Code != http.StatusOK {
			t.Fatalf("GET /art/ship/%s = %d", slug, recorder.Code)
		}
		if contentType := recorder.Header().Get("Content-Type"); contentType != "image/webp" {
			t.Fatalf("/art/ship/%s served %q: the slot has no artwork", slug, contentType)
		}
		if cache := recorder.Header().Get("Cache-Control"); !strings.Contains(cache, "immutable") {
			t.Fatalf("/art/ship/%s cache control = %q", slug, cache)
		}
	}

	// The other way round: a file the catalogue does not name would travel in
	// every binary and be requested by nobody.
	entries, err := fs.ReadDir(webassets.Files, "static/art/ship")
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		slug := strings.TrimSuffix(entry.Name(), path.Ext(entry.Name()))
		if !illustrated[slug] {
			t.Fatalf("static/art/ship/%s fills no slot of the catalogue", entry.Name())
		}
	}
}

// The slug reaches the filesystem, so anything that is not a plain identifier is
// refused rather than cleaned up.
func TestArtRefusesUnknownCategoriesAndHostileSlugs(t *testing.T) {
	handler := artHandler(t)
	for _, target := range []string{
		"/art/secrets/passwd", "/art/building/..", "/art/building/%2e%2e%2fapp.css",
		"/art/building/a%2fb", "/art/building/",
	} {
		if recorder := fetch(handler, target); recorder.Code == http.StatusOK {
			t.Fatalf("GET %s = 200, expected a refusal", target)
		}
	}
}

func artHandler(t *testing.T) http.Handler {
	t.Helper()
	handler, err := webhandler.New(webhandler.Dependencies{
		Authentication: webAuthenticationStub{},
		ServerState:    runningStateStub{},
		CSRFSecrets:    sequenceSecret{value: "csrf-token"},
	})
	if err != nil {
		t.Fatal(err)
	}
	return handler
}

func fetch(handler http.Handler, target string) *httptest.ResponseRecorder {
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, target, nil))
	return recorder
}
