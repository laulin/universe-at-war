package tests

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"testing"
	"time"

	appauth "universeatwar/internal/app/authentication"
	appchat "universeatwar/internal/app/chat"
	appclock "universeatwar/internal/clock"
	webhandler "universeatwar/internal/web"
)

func TestPrivateAndAllianceChatsStayInsideTheirAudience(t *testing.T) {
	ctx := context.Background()
	database := economyDatabase(t, ctx, 4)
	universeWorld := newWorld(t, database, appclock.NewFake(time.Date(2042, time.September, 10, 11, 12, 13, 0, time.UTC)))
	alice := appauth.Principal{AccountID: 1, Username: "player1", Roles: []appauth.Role{appauth.RolePlayer}}
	bob := appauth.Principal{AccountID: 2, Username: "player2", Roles: []appauth.Role{appauth.RolePlayer}}
	charlie := appauth.Principal{AccountID: 3, Username: "player3", Roles: []appauth.Role{appauth.RolePlayer}}
	for _, player := range []struct {
		principal appauth.Principal
		name      string
	}{{alice, "Alice"}, {bob, "Bob"}, {charlie, "Charlie"}} {
		if _, err := universeWorld.Economy.CreateEmpire(ctx, player.principal, player.name); err != nil {
			t.Fatalf("CreateEmpire(%s): %v", player.name, err)
		}
	}

	direct, err := universeWorld.Chat.Direct(ctx, alice, 2)
	if err != nil {
		t.Fatalf("Direct() error = %v", err)
	}
	message, err := universeWorld.Chat.Send(ctx, alice, direct.ID, appchat.Draft{
		Body: "Salut Bob 👋", GIFURL: "https://media.example.test/victory.gif", ClientKey: "message-direct-1",
	})
	if err != nil {
		t.Fatalf("Send() error = %v", err)
	}
	if message.AuthorName != "Alice" || !message.Own || message.GIFURL == "" {
		t.Fatalf("sent message = %#v", message)
	}
	// Retrying the same browser operation is idempotent.
	if _, err := universeWorld.Chat.Send(ctx, alice, direct.ID, appchat.Draft{
		Body: "Salut Bob 👋", GIFURL: "https://media.example.test/victory.gif", ClientKey: "message-direct-1",
	}); err != nil {
		t.Fatalf("replayed Send() error = %v", err)
	}
	assertSingleValue(t, database, "SELECT COUNT(*) FROM chat_messages WHERE conversation_id = ?", 1, direct.ID)

	bobView, err := universeWorld.Chat.Conversation(ctx, bob, direct.ID)
	if err != nil || len(bobView.Messages) != 1 || bobView.Messages[0].Own || bobView.Messages[0].Body != "Salut Bob 👋" {
		t.Fatalf("Bob conversation = %#v, %v", bobView, err)
	}
	if _, err := universeWorld.Chat.Conversation(ctx, charlie, direct.ID); !errors.Is(err, appchat.ErrNotFound) {
		t.Fatalf("stranger Conversation() error = %v, want not found", err)
	}
	if _, err := universeWorld.Chat.Send(ctx, alice, direct.ID, appchat.Draft{
		GIFURL: "http://tracking.example.test/not-secure.gif", ClientKey: "message-direct-2",
	}); !errors.Is(err, appchat.ErrInvalidGIF) {
		t.Fatalf("insecure GIF error = %v", err)
	}

	if err := universeWorld.Chat.IsTyping(ctx, alice, direct.ID); err != nil {
		t.Fatalf("IsTyping() error = %v", err)
	}
	update, err := universeWorld.Chat.Updates(ctx, bob, direct.ID, message.ID)
	if err != nil || !slices.Equal(update.Typing, []string{"Alice"}) {
		t.Fatalf("typing update = %#v, %v", update, err)
	}
	universeWorld.Clock.Advance(6 * time.Second)
	update, err = universeWorld.Chat.Updates(ctx, bob, direct.ID, message.ID)
	if err != nil || len(update.Typing) != 0 {
		t.Fatalf("expired typing update = %#v, %v", update, err)
	}

	joinAlliance(t, ctx, universeWorld, alice, bob, "Bob")
	allianceRoom, err := universeWorld.Chat.Alliance(ctx, alice)
	if err != nil {
		t.Fatalf("Alliance() error = %v", err)
	}
	if _, err := universeWorld.Chat.Send(ctx, bob, allianceRoom.ID, appchat.Draft{
		Body: "La flotte est prête 🚀", ClientKey: "alliance-message-1",
	}); err != nil {
		t.Fatalf("alliance Send() error = %v", err)
	}
	allianceView, err := universeWorld.Chat.Alliance(ctx, alice)
	if err != nil || len(allianceView.Messages) != 1 || allianceView.Messages[0].AuthorName != "Bob" {
		t.Fatalf("alliance conversation = %#v, %v", allianceView, err)
	}
	if _, err := universeWorld.Chat.Conversation(ctx, charlie, allianceRoom.ID); !errors.Is(err, appchat.ErrNotFound) {
		t.Fatalf("outsider alliance conversation error = %v", err)
	}
}

func TestChatUnreadStateIsPerPlayerMonotonicAndAllianceAware(t *testing.T) {
	ctx := context.Background()
	database := economyDatabase(t, ctx, 4)
	universeWorld := newWorld(t, database, appclock.NewFake(time.Date(2042, time.September, 10, 11, 12, 13, 0, time.UTC)))
	alice := appauth.Principal{AccountID: 1, Username: "player1", Roles: []appauth.Role{appauth.RolePlayer}}
	bob := appauth.Principal{AccountID: 2, Username: "player2", Roles: []appauth.Role{appauth.RolePlayer}}
	charlie := appauth.Principal{AccountID: 3, Username: "player3", Roles: []appauth.Role{appauth.RolePlayer}}
	for _, player := range []struct {
		principal appauth.Principal
		name      string
	}{{alice, "Alice"}, {bob, "Bob"}, {charlie, "Charlie"}} {
		if _, err := universeWorld.Economy.CreateEmpire(ctx, player.principal, player.name); err != nil {
			t.Fatal(err)
		}
	}

	direct, err := universeWorld.Chat.Direct(ctx, alice, 2)
	if err != nil {
		t.Fatal(err)
	}
	first, err := universeWorld.Chat.Send(ctx, alice, direct.ID, appchat.Draft{
		Body: "Premier", ClientKey: "unread-direct-first",
	})
	if err != nil {
		t.Fatal(err)
	}
	second, err := universeWorld.Chat.Send(ctx, alice, direct.ID, appchat.Draft{
		Body: "Deuxième", ClientKey: "unread-direct-second",
	})
	if err != nil {
		t.Fatal(err)
	}
	if got, err := universeWorld.Chat.UnreadCount(ctx, alice); err != nil || got != 0 {
		t.Fatalf("Alice unread = %d, %v; want 0", got, err)
	}
	if got, err := universeWorld.Chat.UnreadCount(ctx, bob); err != nil || got != 2 {
		t.Fatalf("Bob unread = %d, %v; want 2", got, err)
	}
	if _, err := universeWorld.Chat.Inbox(ctx, bob); err != nil {
		t.Fatal(err)
	}
	if got, err := universeWorld.Chat.UnreadCount(ctx, bob); err != nil || got != 2 {
		t.Fatalf("listing the inbox marked messages read: %d, %v", got, err)
	}
	if _, err := universeWorld.Chat.Conversation(ctx, bob, direct.ID); err != nil {
		t.Fatal(err)
	}
	if got, err := universeWorld.Chat.UnreadCount(ctx, bob); err != nil || got != 0 {
		t.Fatalf("Bob unread after opening = %d, %v; want 0", got, err)
	}

	third, err := universeWorld.Chat.Send(ctx, alice, direct.ID, appchat.Draft{
		Body: "Troisième", ClientKey: "unread-direct-third",
	})
	if err != nil {
		t.Fatal(err)
	}
	if got, err := universeWorld.Chat.UnreadCount(ctx, bob); err != nil || got != 1 {
		t.Fatalf("Bob unread after another message = %d, %v; want 1", got, err)
	}
	update, err := universeWorld.Chat.Updates(ctx, bob, direct.ID, second.ID)
	if err != nil || len(update.Messages) != 1 || update.Messages[0].ID != third.ID {
		t.Fatalf("Bob update = %#v, %v", update, err)
	}
	if got, err := universeWorld.Chat.UnreadCount(ctx, bob); err != nil || got != 0 {
		t.Fatalf("Bob unread after visible update = %d, %v; want 0", got, err)
	}
	if err := universeWorld.Chat.Repository.MarkReadThrough(ctx, bob.AccountID, direct.ID, first.ID, universeWorld.Clock.Now()); err != nil {
		t.Fatal(err)
	}
	if got, err := universeWorld.Chat.UnreadCount(ctx, bob); err != nil || got != 0 {
		t.Fatalf("an old cursor moved backwards: %d, %v", got, err)
	}
	if err := universeWorld.Chat.Repository.MarkReadThrough(ctx, charlie.AccountID, direct.ID, first.ID, universeWorld.Clock.Now()); !errors.Is(err, appchat.ErrNotFound) {
		t.Fatalf("stranger mark-read error = %v, want not found", err)
	}

	if _, err := universeWorld.Alliance.Create(ctx, alice, "Les Veilleurs", "veil", ""); err != nil {
		t.Fatal(err)
	}
	allianceRoom, err := universeWorld.Chat.Alliance(ctx, alice)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := universeWorld.Chat.Send(ctx, alice, allianceRoom.ID, appchat.Draft{
		Body: "Avant Bob", ClientKey: "unread-alliance-before",
	}); err != nil {
		t.Fatal(err)
	}
	universeWorld.Clock.Advance(time.Second)
	joinAlliance(t, ctx, universeWorld, alice, bob, "Bob")
	if got, err := universeWorld.Chat.UnreadCount(ctx, bob); err != nil || got != 0 {
		t.Fatalf("pre-membership alliance unread = %d, %v; want 0", got, err)
	}
	if _, err := universeWorld.Chat.Send(ctx, alice, allianceRoom.ID, appchat.Draft{
		Body: "Après Bob", ClientKey: "unread-alliance-after",
	}); err != nil {
		t.Fatal(err)
	}
	if got, err := universeWorld.Chat.UnreadCount(ctx, bob); err != nil || got != 1 {
		t.Fatalf("alliance unread = %d, %v; want 1", got, err)
	}
	if err := universeWorld.Alliance.Leave(ctx, bob); err != nil {
		t.Fatal(err)
	}
	if got, err := universeWorld.Chat.UnreadCount(ctx, bob); err != nil || got != 0 {
		t.Fatalf("former member unread = %d, %v; want 0", got, err)
	}
}

func TestOnlyAdministratorsCanSuperviseEveryChat(t *testing.T) {
	ctx := context.Background()
	database := economyDatabase(t, ctx, 4)
	universeWorld := newWorld(t, database, appclock.NewFake(time.Date(2042, time.September, 10, 11, 12, 13, 0, time.UTC)))
	alice := appauth.Principal{AccountID: 1, Roles: []appauth.Role{appauth.RolePlayer}}
	bob := appauth.Principal{AccountID: 2, Roles: []appauth.Role{appauth.RolePlayer}}
	for _, player := range []struct {
		principal appauth.Principal
		name      string
	}{{alice, "Alice"}, {bob, "Bob"}} {
		if _, err := universeWorld.Economy.CreateEmpire(ctx, player.principal, player.name); err != nil {
			t.Fatal(err)
		}
	}
	conversation, err := universeWorld.Chat.Direct(ctx, alice, 2)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := universeWorld.Chat.Send(ctx, alice, conversation.ID, appchat.Draft{
		Body: "Coordonnées confirmées.", ClientKey: "admin-visible-message",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := universeWorld.Chat.Send(ctx, bob, conversation.ID, appchat.Draft{
		Body: "Bien reçu.", ClientKey: "admin-visible-reply",
	}); err != nil {
		t.Fatal(err)
	}

	admin := appauth.Principal{AccountID: 4, Username: "admin", Roles: []appauth.Role{appauth.RoleAdmin}}
	moderator := appauth.Principal{AccountID: 3, Username: "moderator", Roles: []appauth.Role{appauth.RoleModerator}}
	player := appauth.Principal{AccountID: 3, Username: "player3", Roles: []appauth.Role{appauth.RolePlayer}}
	listed, err := universeWorld.Chat.AdministrativeInbox(ctx, admin)
	if err != nil || len(listed) != 1 || listed[0].Title != "Alice ↔ Bob" {
		t.Fatalf("AdministrativeInbox() = %#v, %v", listed, err)
	}
	inspected, err := universeWorld.Chat.AdministrativeConversation(ctx, admin, conversation.ID)
	if err != nil || len(inspected.Messages) != 2 || inspected.Messages[0].Body != "Coordonnées confirmées." ||
		!inspected.Messages[0].FromInitiator || inspected.Messages[1].FromInitiator {
		t.Fatalf("AdministrativeConversation() = %#v, %v", inspected, err)
	}
	for name, principal := range map[string]appauth.Principal{"moderator": moderator, "player": player} {
		if _, err := universeWorld.Chat.AdministrativeInbox(ctx, principal); !errors.Is(err, appchat.ErrForbidden) {
			t.Fatalf("%s AdministrativeInbox() error = %v", name, err)
		}
	}
}

func TestWebChatOpensFromTheGalaxyAndUpdatesWithoutReloading(t *testing.T) {
	ctx := context.Background()
	database := economyDatabase(t, ctx, 4)
	universeWorld := newWorld(t, database, appclock.NewFake(time.Date(2042, time.September, 10, 11, 12, 13, 0, time.UTC)))
	alice := appauth.Principal{AccountID: 1, Username: "player1", Roles: []appauth.Role{appauth.RolePlayer}}
	bob := appauth.Principal{AccountID: 2, Username: "player2", Roles: []appauth.Role{appauth.RolePlayer}}
	for _, player := range []struct {
		principal appauth.Principal
		name      string
	}{{alice, "Alice"}, {bob, "Bob"}} {
		if _, err := universeWorld.Economy.CreateEmpire(ctx, player.principal, player.name); err != nil {
			t.Fatal(err)
		}
	}
	playerHandler := chatHandler(t, universeWorld, alice)
	session := &http.Cookie{Name: "uaw_session", Value: "session"}
	csrf := &http.Cookie{Name: "uaw_csrf", Value: "csrf-token"}

	galaxy := getPage(t, playerHandler, "/galaxy/1/1", session, csrf)
	bobRow := galaxyRowOf(t, galaxy, "1:1:1")
	for _, expected := range []string{`class="galaxy-chat"`, `href="/chat/players/2"`, `Écrire à Bob`} {
		if !strings.Contains(bobRow, expected) {
			t.Fatalf("galaxy row misses %s: %q", expected, bobRow)
		}
	}

	page := getPage(t, playerHandler, "/chat/players/2", session, csrf)
	for _, expected := range []string{
		"Conversation privée", "Bob", `data-chat-updates="/chat/conversations/1/updates"`,
		`data-chat-typing="/chat/conversations/1/typing"`, `data-chat-emoji`, `name="gif_url"`,
	} {
		if !strings.Contains(page, expected) {
			t.Fatalf("chat page misses %s: %q", expected, page)
		}
	}

	request := postMultipartFormRequest(t, "/chat/conversations/1/messages", url.Values{
		"csrf_token": {"csrf-token"}, "client_key": {"web-message-1"}, "body": {"À l'attaque ! ⚔️"},
		"gif_url": {"https://media.example.test/attack.gif"},
	})
	request.Header.Set("Accept", "application/json")
	request.AddCookie(session)
	request.AddCookie(csrf)
	response := httptest.NewRecorder()
	playerHandler.ServeHTTP(response, request)
	if response.Code != http.StatusCreated || response.Header().Get("Content-Type") != "application/json; charset=utf-8" {
		t.Fatalf("POST dynamic message = %d %q: %s", response.Code, response.Header().Get("Content-Type"), response.Body.String())
	}
	var sent appchat.Message
	if err := json.Unmarshal(response.Body.Bytes(), &sent); err != nil || sent.Body != "À l'attaque ! ⚔️" || !sent.Own {
		t.Fatalf("dynamic response = %#v, %v", sent, err)
	}

	bobHandler := chatHandler(t, universeWorld, bob)
	bobPage := getPage(t, bobHandler, "/chat/conversations/1", session, csrf)
	for _, expected := range []string{"À l&#39;attaque ! ⚔️", "https://media.example.test/attack.gif", "Alice"} {
		if !strings.Contains(bobPage, expected) {
			t.Fatalf("Bob page misses %s: %q", expected, bobPage)
		}
	}
	if _, err := universeWorld.Chat.Send(ctx, bob, 1, appchat.Draft{
		Body: "Bien reçu.", ClientKey: "web-message-reply",
	}); err != nil {
		t.Fatal(err)
	}
	joinAlliance(t, ctx, universeWorld, alice, bob, "Bob")
	alliancePage := getPage(t, playerHandler, "/chat/alliance", session, csrf)
	for _, expected := range []string{"Canal commun", "Alliance [COR] Les Corsaires", `href="/chat/alliance"`} {
		if !strings.Contains(alliancePage, expected) {
			t.Fatalf("alliance chat page misses %s: %q", expected, alliancePage)
		}
	}

	admin := appauth.Principal{AccountID: 4, Username: "admin", Roles: []appauth.Role{appauth.RoleAdmin}}
	adminPage := getPage(t, chatHandler(t, universeWorld, admin), "/admin/chats/1", session, csrf)
	for _, expected := range []string{
		"Vue globale en lecture seule", "À l&#39;attaque ! ⚔️", "Bien reçu.",
		`class="chat-message">`, `class="chat-message chat-message--right"`,
	} {
		if !strings.Contains(adminPage, expected) {
			t.Fatalf("administrator chat page misses %s: %q", expected, adminPage)
		}
	}
	moderator := appauth.Principal{AccountID: 3, Username: "moderator", Roles: []appauth.Role{appauth.RoleModerator}}
	if status := statusOf(t, chatHandler(t, universeWorld, moderator), "/admin/chats", session, csrf); status != http.StatusNotFound {
		t.Fatalf("moderator GET /admin/chats = %d, want 404", status)
	}
}

func TestWebNavigationShowsAndClearsUnreadMessages(t *testing.T) {
	ctx := context.Background()
	database := economyDatabase(t, ctx, 2)
	universeWorld := newWorld(t, database, appclock.NewFake(time.Date(2042, time.September, 10, 11, 12, 13, 0, time.UTC)))
	alice := appauth.Principal{AccountID: 1, Username: "player1", Roles: []appauth.Role{appauth.RolePlayer}}
	bob := appauth.Principal{AccountID: 2, Username: "player2", Roles: []appauth.Role{appauth.RolePlayer}}
	for _, player := range []struct {
		principal appauth.Principal
		name      string
	}{{alice, "Alice"}, {bob, "Bob"}} {
		if _, err := universeWorld.Economy.CreateEmpire(ctx, player.principal, player.name); err != nil {
			t.Fatal(err)
		}
	}
	conversation, err := universeWorld.Chat.Direct(ctx, alice, 2)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := universeWorld.Chat.Send(ctx, alice, conversation.ID, appchat.Draft{
		Body: "Message en attente", ClientKey: "web-unread-message",
	}); err != nil {
		t.Fatal(err)
	}

	session := &http.Cookie{Name: "uaw_session", Value: "session"}
	csrf := &http.Cookie{Name: "uaw_csrf", Value: "csrf-token"}
	bobHandler := chatHandler(t, universeWorld, bob)
	page := getPage(t, bobHandler, "/galaxy/1/1", session, csrf)
	for _, expected := range []string{
		`data-chat-navigation`, `data-chat-unread`, `data-chat-unread-count>1</b>`,
		`aria-label="Messagerie, nombre de messages non lus : 1"`,
	} {
		if !strings.Contains(page, expected) {
			t.Fatalf("navigation misses %s: %q", expected, page)
		}
	}

	unreadResponse := httptest.NewRecorder()
	unreadRequest := httptest.NewRequest(http.MethodGet, "/chat/unread", nil)
	unreadRequest.Header.Set("Accept", "application/json")
	unreadRequest.AddCookie(session)
	bobHandler.ServeHTTP(unreadResponse, unreadRequest)
	if unreadResponse.Code != http.StatusOK || unreadResponse.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("GET /chat/unread = %d, cache %q", unreadResponse.Code, unreadResponse.Header().Get("Cache-Control"))
	}
	var unread struct {
		Count int `json:"count"`
	}
	if err := json.Unmarshal(unreadResponse.Body.Bytes(), &unread); err != nil || unread.Count != 1 {
		t.Fatalf("unread response = %#v, %v", unread, err)
	}

	opened := getPage(t, bobHandler, "/chat/conversations/1", session, csrf)
	if !strings.Contains(opened, `data-chat-unread hidden`) {
		t.Fatalf("opening the conversation kept the unread marker: %q", opened)
	}
	if strings.Contains(opened, `aria-label="Messagerie, nombre de messages non lus`) {
		t.Fatalf("opening the conversation kept the unread label: %q", opened)
	}
}

func TestChatKeyboardUsesEnterToSendAndShiftEnterForANewline(t *testing.T) {
	handler, err := webhandler.New(webhandler.Dependencies{
		Authentication: webAuthenticationStub{}, ServerState: runningStateStub{},
		CSRFSecrets: sequenceSecret{value: "csrf-token"},
	})
	if err != nil {
		t.Fatal(err)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/static/app.js", nil))
	if response.Code != http.StatusOK {
		t.Fatalf("GET /static/app.js = %d", response.Code)
	}
	script := response.Body.String()
	for _, expected := range []string{
		`input.addEventListener("keydown"`,
		`event.key !== "Enter" || event.shiftKey || event.isComposing || submit.disabled`,
		`event.preventDefault()`,
		`form.requestSubmit(submit)`,
	} {
		if !strings.Contains(script, expected) {
			t.Fatalf("chat keyboard behavior misses %q", expected)
		}
	}
}

func postMultipartFormRequest(t *testing.T, target string, values url.Values) *http.Request {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	for name, entries := range values {
		for _, value := range entries {
			if err := writer.WriteField(name, value); err != nil {
				t.Fatalf("write multipart field %s: %v", name, err)
			}
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close multipart form: %v", err)
	}
	request := httptest.NewRequest(http.MethodPost, target, &body)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	return request
}

func chatHandler(t *testing.T, universeWorld *world, principal appauth.Principal) http.Handler {
	t.Helper()
	handler, err := webhandler.New(webhandler.Dependencies{
		Authentication: webAuthenticationStub{principal: principal},
		ServerState:    runningStateStub{}, CSRFSecrets: sequenceSecret{value: "csrf-token"},
		Nonces: sequenceSecret{value: "chat-message-key"}, Economy: universeWorld.Economy,
		Galaxy: universeWorld.Galaxy, Chat: universeWorld.Chat,
	})
	if err != nil {
		t.Fatal(err)
	}
	return handler
}
