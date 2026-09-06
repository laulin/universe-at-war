package tests

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	appauth "universeatwar/internal/app/authentication"
	appsetup "universeatwar/internal/app/setup"
	appclock "universeatwar/internal/clock"
	"universeatwar/internal/domain/rules"
	"universeatwar/internal/domain/server"
	storagesqlite "universeatwar/internal/storage/sqlite"
	webhandler "universeatwar/internal/web"
)

// TestProfilesLoadCompareImportAndExport walks what an administrator does with
// a ruleset before starting a universe.
func TestProfilesLoadCompareImportAndExport(t *testing.T) {
	ctx := context.Background()
	database, setup, admin := profileUniverse(t)

	profiles, err := setup.Profiles(ctx, admin)
	if err != nil || len(profiles) != 6 {
		t.Fatalf("Profiles() = %d %v", len(profiles), err)
	}
	draft, err := setup.Load(ctx, admin)
	if err != nil {
		t.Fatal(err)
	}
	// A fresh draft is the reference, so nothing differs yet.
	differences, err := setup.Differences(ctx, admin)
	if err != nil || len(differences) != 0 {
		t.Fatalf("Differences(fresh) = %d %v", len(differences), err)
	}

	loaded, err := setup.Apply(ctx, admin, draft.Version, "fast")
	if err != nil {
		t.Fatalf("Apply() error = %v", err)
	}
	if loaded.Rules.Time.EconomySpeed != 5 {
		t.Fatalf("the accelerated profile was not loaded: %+v", loaded.Rules.Time)
	}
	// Loading a profile does not answer the questions of the wizard for you.
	if loaded.CurrentStep != draft.CurrentStep {
		t.Fatalf("the wizard moved from %d to %d", draft.CurrentStep, loaded.CurrentStep)
	}
	differences, err = setup.Differences(ctx, admin)
	if err != nil || len(differences) == 0 {
		t.Fatalf("Differences(fast) = %d %v", len(differences), err)
	}
	assertSingleValue(t, database,
		"SELECT COUNT(*) FROM audit_log WHERE action = 'setup_ruleset_replaced'", 1)

	// What is exported comes back in unchanged.
	document, err := setup.Export(ctx, admin)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := setup.Apply(ctx, admin, loaded.Version, "slow"); err != nil {
		t.Fatalf("Apply(slow) error = %v", err)
	}
	current, err := setup.Load(ctx, admin)
	if err != nil {
		t.Fatal(err)
	}
	restored, err := setup.Import(ctx, admin, current.Version, document)
	if err != nil {
		t.Fatalf("Import() error = %v", err)
	}
	if restored.Rules != loaded.Rules {
		t.Fatal("the exported document did not come back unchanged")
	}
}

// TestACorruptOrFutureDocumentChangesNothing proves an import is all or
// nothing: a document this build refuses leaves the draft exactly as it was.
func TestACorruptOrFutureDocumentChangesNothing(t *testing.T) {
	ctx := context.Background()
	_, setup, admin := profileUniverse(t)
	draft, err := setup.Load(ctx, admin)
	if err != nil {
		t.Fatal(err)
	}
	before := draft.Rules

	for _, document := range []string{
		"",
		"{",
		`{"schema_version":99}`,
		`{"schema_version":6,"mystery":1}`,
		`{"schema_version":6,"time":{"economy_speed":-4}}`,
	} {
		if _, err := setup.Import(ctx, admin, draft.Version, []byte(document)); err == nil {
			t.Fatalf("the document %q was accepted", document)
		} else if !errors.Is(err, appsetup.ErrInvalidDocument) {
			t.Fatalf("the document %q was refused with %v", document, err)
		}
		current, err := setup.Load(ctx, admin)
		if err != nil {
			t.Fatal(err)
		}
		if current.Rules != before || current.Version != draft.Version {
			t.Fatalf("the refused document %q changed the draft", document)
		}
	}
	// An unknown profile is refused the same way.
	if _, err := setup.Apply(ctx, admin, draft.Version, "nowhere"); !errors.Is(err, rules.ErrUnknownProfile) {
		t.Fatalf("an unknown profile error = %v", err)
	}
}

// TestWebProfilePageShowsTheDifferencesBeforeActivation proves an administrator
// reads what changes before starting a universe, and that nobody else can.
func TestWebProfilePageShowsTheDifferencesBeforeActivation(t *testing.T) {
	_, setup, _ := profileUniverse(t)
	handler := func(principal appauth.Principal) http.Handler {
		built, err := webhandler.New(webhandler.Dependencies{
			Authentication: webAuthenticationStub{principal: principal},
			ServerState:    setupStateStub{}, CSRFSecrets: sequenceSecret{value: "csrf-token"},
			Setup: setup,
		})
		if err != nil {
			t.Fatal(err)
		}
		return built
	}
	admin := handler(appauth.Principal{AccountID: 1, Username: "admin", Roles: []appauth.Role{appauth.RoleAdmin}})
	player := handler(appauth.Principal{AccountID: 2, Username: "player", Roles: []appauth.Role{appauth.RolePlayer}})
	session := &http.Cookie{Name: "uaw_session", Value: "session"}
	csrfCookie := &http.Cookie{Name: "uaw_csrf", Value: "csrf-token"}

	page := getPage(t, admin, "/setup/profiles", session, csrfCookie)
	for _, expected := range []string{"Proche classique", "PvPvE", "Écarts avec les règles", "Importer"} {
		if !strings.Contains(page, expected) {
			t.Fatalf("the profiles page misses %q", expected)
		}
	}
	// A player has no business here.
	if code := statusOf(t, player, "/setup/profiles", session, csrfCookie); code != http.StatusNotFound {
		t.Fatalf("GET the profiles page as a player = %d", code)
	}

	postForm(t, admin, "/setup/profiles", url.Values{
		"csrf_token": {"csrf-token"}, "version": {"1"}, "profile": {"conflict"},
	}, http.StatusSeeOther, session, csrfCookie)
	after := getPage(t, admin, "/setup/profiles", session, csrfCookie)
	if !strings.Contains(after, "economy.pillage_ratio") {
		t.Fatalf("the page does not show what the profile changed: %q", after)
	}
	// A refused import says so rather than silently doing nothing.
	body := postForm(t, admin, "/setup/import", url.Values{
		"csrf_token": {"csrf-token"}, "version": {"2"}, "document": {`{"schema_version":99}`},
	}, http.StatusBadRequest, session, csrfCookie)
	if !strings.Contains(body, "version plus récente") {
		t.Fatalf("the refusal is not explained: %q", body)
	}
}

func profileUniverse(t *testing.T) (*storagesqlite.Database, appsetup.Service, appauth.Principal) {
	t.Helper()
	ctx := context.Background()
	database := economyDatabase(t, ctx, 1)
	if _, err := database.Write().ExecContext(ctx, "DELETE FROM ruleset_versions"); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Write().ExecContext(ctx,
		"INSERT INTO server_state(id, state, updated_at) VALUES (1, 'BOOTSTRAP_PENDING', '2042-09-10T11:12:13Z')"); err != nil {
		t.Fatal(err)
	}
	setup := appsetup.Service{
		Clock:      appclock.NewFake(time.Date(2042, time.September, 10, 12, 0, 0, 0, time.UTC)),
		Repository: storagesqlite.NewSetupRepository(database.Write()),
	}
	return database, setup, appauth.Principal{AccountID: 1, Username: "admin", Roles: []appauth.Role{appauth.RoleAdmin}}
}

// setupStateStub reports a universe still waiting to be configured.
type setupStateStub struct{}

func (setupStateStub) Current(context.Context) (server.State, error) {
	return server.SetupInProgress, nil
}
