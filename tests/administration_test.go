package tests

import (
	"context"
	"crypto/rand"
	"errors"
	"html"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"

	appadmin "universeatwar/internal/app/administration"
	appauth "universeatwar/internal/app/authentication"
	appmoderation "universeatwar/internal/app/moderation"
	"universeatwar/internal/auth"
	appclock "universeatwar/internal/clock"
	"universeatwar/internal/domain/rules"
	storagesqlite "universeatwar/internal/storage/sqlite"
	webhandler "universeatwar/internal/web"
)

// TestScenarioIBanBlocksLoginNotEmpire proves a sanction closes a door and
// nothing else: the empire behind it keeps producing and its files keep running.
func TestScenarioIBanBlocksLoginNotEmpire(t *testing.T) {
	ctx := context.Background()
	database, universeWorld, services := administeredUniverse(t)
	player := appauth.Principal{AccountID: 2}
	home, err := universeWorld.Economy.CreateEmpire(ctx, player, "Bob")
	if err != nil {
		t.Fatalf("CreateEmpire() error = %v", err)
	}
	setResources(t, ctx, database, home.ID, 5000, 5000, 5000)
	if _, err := universeWorld.Economy.EnqueueBuilding(ctx, player, home.ID, "metal_mine", "mine"); err != nil {
		t.Fatalf("EnqueueBuilding() error = %v", err)
	}
	before, err := universeWorld.Economy.Planet(ctx, player, home.ID)
	if err != nil {
		t.Fatal(err)
	}

	ban, err := services.moderation.Apply(ctx, services.admin, appmoderation.Request{
		AccountID: 2, Hours: 24, Justification: "propos inacceptables",
	})
	if err != nil {
		t.Fatalf("Apply() error = %v", err)
	}
	// The door is shut: the credential lookup reports the account unavailable.
	credential, err := storagesqlite.NewAuthenticationRepository(database.Read(), database.Write()).
		FindCredentialByID(ctx, 2, universeWorld.Clock.Now().UTC())
	if err != nil {
		t.Fatalf("FindCredentialByID() error = %v", err)
	}
	if !credential.Unavailable {
		t.Fatal("a banned account can still sign in")
	}
	assertSingleValue(t, database, "SELECT COUNT(*) FROM audit_log WHERE action = 'account_banned'", 1)

	// The empire is untouched, and it goes on producing and building.
	universeWorld.Clock.Advance(2 * time.Hour)
	if _, err := universeWorld.Events.CompleteDue(ctx, 50); err != nil {
		t.Fatalf("CompleteDue() error = %v", err)
	}
	after, err := universeWorld.Economy.Planet(ctx, player, home.ID)
	if err != nil {
		t.Fatal(err)
	}
	if after.Stock.Metal <= before.Stock.Metal {
		t.Fatalf("the empire stopped producing: %d then %d", before.Stock.Metal, after.Stock.Metal)
	}
	assertSingleValue(t, database,
		"SELECT COUNT(*) FROM building_queue WHERE planet_id = ? AND state = 'completed'", 1, home.ID)

	// Lifting the sanction opens the door again.
	if err := services.moderation.Lift(ctx, services.admin, ban.ID); err != nil {
		t.Fatalf("Lift() error = %v", err)
	}
	credential, err = storagesqlite.NewAuthenticationRepository(database.Read(), database.Write()).
		FindCredentialByID(ctx, 2, universeWorld.Clock.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	if credential.Unavailable {
		t.Fatal("a lifted sanction still keeps the account out")
	}
	assertSingleValue(t, database, "SELECT COUNT(*) FROM audit_log WHERE action = 'account_unbanned'", 1)
	if err := services.moderation.Lift(ctx, services.admin, ban.ID); !errors.Is(err, appmoderation.ErrBanNotFound) {
		t.Fatalf("lifting twice error = %v", err)
	}
}

// TestModeratorCannotReachAdministration proves each role stops where it should.
func TestModeratorCannotReachAdministration(t *testing.T) {
	ctx := context.Background()
	database, _, services := administeredUniverse(t)
	if _, err := database.Write().ExecContext(ctx,
		"INSERT INTO account_roles(account_id, role, granted_at) VALUES (3, 'MODERATOR', '2042-09-10T12:00:00Z')"); err != nil {
		t.Fatal(err)
	}
	moderator := appauth.Principal{AccountID: 3, Roles: []appauth.Role{appauth.RoleModerator}}
	player := appauth.Principal{AccountID: 2, Roles: []appauth.Role{appauth.RolePlayer}}

	// A moderator sanctions players and nothing else.
	if _, err := services.moderation.Apply(ctx, moderator, appmoderation.Request{
		AccountID: 2, Hours: 1, Justification: "avertissement",
	}); err != nil {
		t.Fatalf("a moderator banning a player error = %v", err)
	}
	if _, err := services.moderation.Apply(ctx, moderator, appmoderation.Request{
		AccountID: 1, Hours: 1, Justification: "coup d'état",
	}); !errors.Is(err, appmoderation.ErrProtected) {
		t.Fatalf("a moderator banning an administrator error = %v", err)
	}
	if _, err := services.moderation.Apply(ctx, moderator, appmoderation.Request{
		AccountID: 2, Hours: 1, Justification: "",
	}); !errors.Is(err, appmoderation.ErrInvalidRequest) {
		t.Fatalf("a sanction without a reason error = %v", err)
	}
	// And it reaches no administration at all.
	if _, err := services.dashboard.Health(ctx, moderator); !errors.Is(err, appadmin.ErrForbidden) {
		t.Fatalf("a moderator reading the dashboard error = %v", err)
	}
	if _, err := services.dashboard.GameSettings(ctx, moderator); !errors.Is(err, appadmin.ErrForbidden) {
		t.Fatalf("a moderator reading the game settings error = %v", err)
	}
	if _, err := services.invitations.Create(ctx, moderator, "moi"); !errors.Is(err, appadmin.ErrForbidden) {
		t.Fatalf("a moderator minting an invitation error = %v", err)
	}
	// A player reaches nothing.
	if _, err := services.moderation.List(ctx, player); !errors.Is(err, appmoderation.ErrForbidden) {
		t.Fatalf("a player reading the sanctions error = %v", err)
	}
	// An administrator cannot ban themselves out of their own universe.
	if _, err := services.moderation.Apply(ctx, services.admin, appmoderation.Request{
		AccountID: 1, Hours: 1, Justification: "test",
	}); !errors.Is(err, appmoderation.ErrProtected) {
		t.Fatalf("an administrator banning themselves error = %v", err)
	}
}

// TestDashboardShowsTheHealthOfTheUniverse proves an administrator can see what
// the server is doing, and manage roles and statuses.
func TestDashboardShowsTheHealthOfTheUniverse(t *testing.T) {
	ctx := context.Background()
	database, universeWorld, services := administeredUniverse(t)
	if _, err := universeWorld.Economy.CreateEmpire(ctx, appauth.Principal{AccountID: 2}, "Bob"); err != nil {
		t.Fatal(err)
	}
	health, err := services.dashboard.Health(ctx, services.admin)
	if err != nil {
		t.Fatalf("Health() error = %v", err)
	}
	if health.SchemaVersion == 0 || health.JournalMode == "" || health.DatabaseBytes == 0 {
		t.Fatalf("the dashboard knows nothing of the database: %+v", health)
	}
	if health.Accounts != 3 || health.Players != 1 || health.RulesetVersion != 1 {
		t.Fatalf("the dashboard miscounts the universe: %+v", health)
	}

	// Roles and statuses are managed from here, with an audit trail.
	if err := services.dashboard.SetRole(ctx, services.admin, 3, "MODERATOR", true); err != nil {
		t.Fatalf("SetRole() error = %v", err)
	}
	assertSingleValue(t, database,
		"SELECT COUNT(*) FROM account_roles WHERE account_id = 3 AND role = 'MODERATOR'", 1)
	if err := services.dashboard.SetStatus(ctx, services.admin, 3, "disabled"); err != nil {
		t.Fatalf("SetStatus() error = %v", err)
	}
	assertSingleText(t, database, "SELECT status FROM accounts WHERE id = 3", "disabled")
	assertSingleValue(t, database,
		"SELECT COUNT(*) FROM audit_log WHERE action IN ('role_granted', 'account_status_changed')", 2)

	// An administrator cannot strip their own administration nor disable
	// themselves: a universe always keeps somebody able to act.
	if err := services.dashboard.SetRole(ctx, services.admin, 1, "ADMIN", false); !errors.Is(err, appadmin.ErrForbidden) {
		t.Fatalf("an administrator removing their own role error = %v", err)
	}
	if err := services.dashboard.SetStatus(ctx, services.admin, 1, "disabled"); !errors.Is(err, appadmin.ErrForbidden) {
		t.Fatalf("an administrator disabling themselves error = %v", err)
	}
}

// TestAdministratorPublishesLiveGameSettings proves a running universe gains a
// new immutable ruleset version and that lazy production is split exactly at
// its activation instant.
func TestAdministratorPublishesLiveGameSettings(t *testing.T) {
	ctx := context.Background()
	database, universeWorld, services := administeredUniverse(t)
	home, err := universeWorld.Economy.CreateEmpire(ctx, appauth.Principal{AccountID: 2}, "Bob")
	if err != nil {
		t.Fatal(err)
	}

	current, err := services.dashboard.GameSettings(ctx, services.admin)
	if err != nil {
		t.Fatalf("GameSettings() error = %v", err)
	}
	if current.Version != 1 || current.Rules.Identity.Name == "" {
		t.Fatalf("active game settings = %+v", current)
	}

	universeWorld.Clock.Advance(time.Hour)
	updated := current.Rules
	updated.Identity.Name = "Univers accéléré"
	updated.Time.EconomySpeed = 2
	published, err := services.dashboard.UpdateGameSettings(ctx, services.admin, current.Version, updated,
		"Accélération de la campagne")
	if err != nil {
		t.Fatalf("UpdateGameSettings() error = %v", err)
	}
	if published.Version != 2 || published.Rules.Time.EconomySpeed != 2 {
		t.Fatalf("published game settings = %+v", published)
	}
	assertSingleValue(t, database,
		"SELECT COUNT(*) FROM ruleset_versions WHERE version = 1 AND status = 'superseded'", 1)
	assertSingleValue(t, database,
		"SELECT COUNT(*) FROM ruleset_versions WHERE version = 2 AND status = 'active' AND length(checksum) = 64", 1)
	assertSingleValue(t, database, "SELECT COUNT(*) FROM audit_log WHERE action = 'ruleset_updated'", 1)

	// The first hour is settled at x1 during publication; only the second hour
	// uses x2. A retroactive update would incorrectly produce 620 metal.
	universeWorld.Clock.Advance(time.Hour)
	after, err := universeWorld.Economy.Planet(ctx, appauth.Principal{AccountID: 2}, home.ID)
	if err != nil {
		t.Fatal(err)
	}
	if after.Stock.Metal != 590 || after.Stock.Crystal != 545 {
		t.Fatalf("production across ruleset boundary = %+v, want 590 metal and 545 crystal", after.Stock)
	}

	if _, err := services.dashboard.UpdateGameSettings(ctx, services.admin, current.Version, updated,
		"Formulaire périmé"); !errors.Is(err, appadmin.ErrRulesConflict) {
		t.Fatalf("UpdateGameSettings(stale) error = %v, want ErrRulesConflict", err)
	}
	immutable := published.Rules
	immutable.Topology.Galaxies++
	if _, err := services.dashboard.UpdateGameSettings(ctx, services.admin, published.Version, immutable,
		"Agrandissement"); !errors.Is(err, appadmin.ErrImmutableRules) {
		t.Fatalf("UpdateGameSettings(topology) error = %v, want ErrImmutableRules", err)
	}
	invalid := published.Rules
	invalid.Time.EconomySpeed = 0
	if _, err := services.dashboard.UpdateGameSettings(ctx, services.admin, published.Version, invalid,
		"Vitesse invalide"); !errors.Is(err, appadmin.ErrInvalidRulesUpdate) {
		t.Fatalf("UpdateGameSettings(invalid) error = %v, want ErrInvalidRulesUpdate", err)
	}
	if _, err := services.dashboard.UpdateGameSettings(ctx, services.admin, published.Version,
		rules.Default(), ""); !errors.Is(err, appadmin.ErrInvalidRulesUpdate) {
		t.Fatalf("UpdateGameSettings(without justification) error = %v, want ErrInvalidRulesUpdate", err)
	}
}

// TestWebAdministrationIsReservedAndWarnsAPlayingAdministrator proves the pages
// answer to nobody else and say plainly when the administrator also plays.
func TestWebAdministrationIsReservedAndWarnsAPlayingAdministrator(t *testing.T) {
	ctx := context.Background()
	database, universeWorld, services := administeredUniverse(t)
	if _, err := universeWorld.Economy.CreateEmpire(ctx, appauth.Principal{AccountID: 1}, "Admin"); err != nil {
		t.Fatal(err)
	}
	build := func(principal appauth.Principal) http.Handler {
		handler, err := webhandler.New(webhandler.Dependencies{
			Authentication: webAuthenticationStub{principal: principal},
			ServerState:    runningStateStub{}, CSRFSecrets: sequenceSecret{value: "csrf-token"},
			Economy: universeWorld.Economy, Dashboard: services.dashboard,
			Invitations: services.invitations, Moderation: services.moderation,
		})
		if err != nil {
			t.Fatal(err)
		}
		return handler
	}
	admin := build(appauth.Principal{AccountID: 1, Username: "admin", Roles: []appauth.Role{appauth.RoleAdmin}})
	moderator := build(appauth.Principal{AccountID: 3, Username: "mod", Roles: []appauth.Role{appauth.RoleModerator}})
	player := build(appauth.Principal{AccountID: 2, Username: "bob", Roles: []appauth.Role{appauth.RolePlayer}})
	session := &http.Cookie{Name: "uaw_session", Value: "session"}
	csrfCookie := &http.Cookie{Name: "uaw_csrf", Value: "csrf-token"}

	page := getPage(t, admin, "/admin", session, csrfCookie)
	for _, expected := range []string{"État de l'univers", "vous y jouez", "Paramètres du jeu", "Comptes", "Invitations"} {
		if !strings.Contains(page, expected) {
			t.Fatalf("the dashboard misses %q", expected)
		}
	}
	if !strings.Contains(page, `class="admin-account-actions"`) {
		t.Fatalf("the account commands do not share an action row: %q", page)
	}
	styles := fetch(admin, "/static/css/pages.css")
	if styles.Code != http.StatusOK || !strings.Contains(styles.Body.String(), ".admin-account-actions {") ||
		!strings.Contains(styles.Body.String(), "flex-wrap: nowrap;") ||
		!strings.Contains(styles.Body.String(), "align-items: flex-end;") {
		t.Fatalf("the administration action row is not kept aligned: %q", styles.Body.String())
	}
	// A moderator reaches the sanctions and nothing else.
	if code := statusOf(t, moderator, "/admin", session, csrfCookie); code != http.StatusNotFound {
		t.Fatalf("GET the dashboard as a moderator = %d", code)
	}
	sanctions := getPage(t, moderator, "/admin/moderation", session, csrfCookie)
	if !strings.Contains(sanctions, "que des\n    joueurs ordinaires") && !strings.Contains(sanctions, "joueurs ordinaires") {
		t.Fatalf("the moderation page does not state its limits: %q", sanctions)
	}
	// A player reaches nothing at all.
	for _, route := range []string{"/admin", "/admin/settings", "/admin/moderation", "/admin/ai"} {
		if code := statusOf(t, player, route, session, csrfCookie); code != http.StatusNotFound {
			t.Fatalf("GET %s as a player = %d", route, code)
		}
	}
	if code := statusOf(t, moderator, "/admin/settings", session, csrfCookie); code != http.StatusNotFound {
		t.Fatalf("GET the game settings as a moderator = %d", code)
	}

	// The full active ruleset can be inspected and a valid form publishes a
	// second version without asking the process to restart.
	settingsPage := getPage(t, admin, "/admin/settings", session, csrfCookie)
	for _, expected := range []string{"Version active", "Vitesses et temps", "Espionnage", "Expéditions", "Paramètres structurels en lecture seule"} {
		if !strings.Contains(settingsPage, expected) {
			t.Fatalf("the game settings page misses %q", expected)
		}
	}
	values := settingsFormValues(t, settingsPage)
	values.Set("economy_speed", "3")
	values.Set("justification", "Test de l'administration web")
	postForm(t, admin, "/admin/settings", values, http.StatusSeeOther, session, csrfCookie)
	assertSingleValue(t, database, "SELECT COUNT(*) FROM ruleset_versions WHERE version = 2 AND status = 'active'", 1)
	reloaded := getPage(t, admin, "/admin/settings?saved=1", session, csrfCookie)
	if !strings.Contains(reloaded, "nouvelle version") || !strings.Contains(reloaded, `name="economy_speed" value="3"`) {
		t.Fatalf("the published settings are not reloaded: %q", reloaded)
	}

	// An invitation is shown exactly once, on creation.
	body := postForm(t, admin, "/admin/invitations", url.Values{
		"csrf_token": {"csrf-token"}, "label": {"un ami"},
	}, http.StatusOK, session, csrfCookie)
	if !strings.Contains(body, "ne sera plus jamais affiché") {
		t.Fatalf("the invitation code was not shown: %q", body)
	}
	again := getPage(t, admin, "/admin", session, csrfCookie)
	if strings.Contains(again, "ne sera plus jamais affiché") {
		t.Fatal("the invitation code came back on a later view")
	}
}

// settingsFormValues reads the application-owned administration form just as a
// browser would submit it. It also makes this test fail when a newly required
// field is rendered without a value or selected option.
func settingsFormValues(t *testing.T, page string) url.Values {
	t.Helper()
	values := url.Values{}
	attribute := func(tag, name string) string {
		match := regexp.MustCompile(`\b` + name + `="([^"]*)"`).FindStringSubmatch(tag)
		if len(match) != 2 {
			return ""
		}
		return html.UnescapeString(match[1])
	}
	for _, match := range regexp.MustCompile(`<input\b[^>]*>`).FindAllString(page, -1) {
		name := attribute(match, "name")
		if name == "" || (attribute(match, "type") == "checkbox" && !strings.Contains(match, " checked")) {
			continue
		}
		value := attribute(match, "value")
		if attribute(match, "type") == "checkbox" && value == "" {
			value = "on"
		}
		values.Set(name, value)
	}
	for _, match := range regexp.MustCompile(`(?s)<textarea\b[^>]*name="([^"]+)"[^>]*>(.*?)</textarea>`).FindAllStringSubmatch(page, -1) {
		values.Set(match[1], html.UnescapeString(match[2]))
	}
	for _, match := range regexp.MustCompile(`(?s)<select\b[^>]*name="([^"]+)"[^>]*>(.*?)</select>`).FindAllStringSubmatch(page, -1) {
		selected := regexp.MustCompile(`<option\b[^>]*value="([^"]*)"[^>]*selected[^>]*>`).FindStringSubmatch(match[2])
		if len(selected) == 2 {
			values.Set(match[1], html.UnescapeString(selected[1]))
		}
	}
	return values
}

// administrationServices are the privileged services of a test universe.
type administrationServices struct {
	admin       appauth.Principal
	dashboard   appadmin.DashboardService
	invitations appadmin.InvitationService
	moderation  appmoderation.Service
}

func administeredUniverse(t *testing.T) (*storagesqlite.Database, *world, administrationServices) {
	t.Helper()
	ctx := context.Background()
	clock := appclock.NewFake(time.Date(2042, time.September, 10, 12, 0, 0, 0, time.UTC))
	database := economyDatabase(t, ctx, 3)
	universeWorld := newWorld(t, database, clock)
	if _, err := database.Write().ExecContext(ctx,
		"INSERT INTO server_state(id, state, updated_at) VALUES (1, 'RUNNING', '2042-09-10T12:00:00Z')"); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Write().ExecContext(ctx,
		"INSERT INTO account_roles(account_id, role, granted_at) VALUES (1, 'ADMIN', '2042-09-10T12:00:00Z')"); err != nil {
		t.Fatal(err)
	}
	for id := 1; id <= 3; id++ {
		if _, err := database.Write().ExecContext(ctx, `
			INSERT INTO password_credentials(account_id, encoded_hash, must_change_password, updated_at)
			VALUES (?, 'argon2id$placeholder', 0, '2042-09-10T12:00:00Z')
		`, id); err != nil {
			t.Fatal(err)
		}
	}
	services := administrationServices{
		admin: appauth.Principal{AccountID: 1, Roles: []appauth.Role{appauth.RoleAdmin}},
		dashboard: appadmin.DashboardService{
			Clock: clock, Repository: storagesqlite.NewDashboardRepository(
				database.Write(), "", clock.Now().UTC().Add(-time.Hour)),
		},
		invitations: appadmin.InvitationService{
			Clock: clock, Secrets: auth.NewSecretGenerator(rand.Reader, 24),
			Repository: storagesqlite.NewInvitationRepository(database.Write()),
		},
		moderation: appmoderation.Service{
			Clock: clock, Repository: storagesqlite.NewModerationRepository(database.Write()),
		},
	}
	return database, universeWorld, services
}
