package tests

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	webhandler "universeatwar/internal/web"
)

// An illustration slot always answers, whether artwork exists for it or not, so
// a screen is never left with a broken picture.
func TestArtSlotsAlwaysRenderAnImage(t *testing.T) {
	handler := artHandler(t)

	for _, target := range []string{
		"/art/building/metal_mine", "/art/research/astrophysics", "/art/ship/light_fighter",
		"/art/defense/rocket_launcher", "/art/resource/metal", "/art/resource/crystal",
		"/art/resource/deuterium", "/art/resource/energy", "/art/body/planet",
		"/art/body/moon", "/art/banner/overview",
	} {
		recorder := fetch(handler, target)
		if recorder.Code != http.StatusOK {
			t.Fatalf("GET %s = %d", target, recorder.Code)
		}
		if contentType := recorder.Header().Get("Content-Type"); !strings.Contains(contentType, "image/") {
			t.Fatalf("GET %s content type = %q", target, contentType)
		}
		if body := recorder.Body.String(); !strings.HasPrefix(body, "<svg") {
			t.Fatalf("GET %s did not draw a placeholder: %q", target, body)
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
