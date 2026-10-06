package web

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	appauth "universeatwar/internal/app/authentication"
	appchat "universeatwar/internal/app/chat"
	appeconomy "universeatwar/internal/app/economy"
)

type chatPageData struct {
	pageShell
	Inbox        appchat.Inbox
	Conversation *appchat.Conversation
	FormKey      string
	ReturnTo     string
	LastMessage  int64
}

type administrativeChatsPageData struct {
	pageShell
	Conversations []appchat.Summary
	Conversation  *appchat.Conversation
}

func (h *Handler) chatInboxPage(response http.ResponseWriter, request *http.Request) {
	principal, _, ok := h.requirePrincipal(response, request)
	if !ok {
		return
	}
	h.renderChat(response, request, principal, nil, "/chat")
}

func (h *Handler) directChatPage(response http.ResponseWriter, request *http.Request) {
	principal, _, ok := h.requirePrincipal(response, request)
	if !ok {
		return
	}
	playerID, err := strconv.ParseInt(request.PathValue("player"), 10, 64)
	if err != nil || h.chat == nil {
		http.NotFound(response, request)
		return
	}
	conversation, err := h.chat.Direct(request.Context(), principal, playerID)
	if err != nil {
		handleChatReadError(response, request, err)
		return
	}
	h.renderChat(response, request, principal, &conversation, "/chat/players/"+strconv.FormatInt(playerID, 10))
}

func (h *Handler) allianceChatPage(response http.ResponseWriter, request *http.Request) {
	principal, _, ok := h.requirePrincipal(response, request)
	if !ok {
		return
	}
	if h.chat == nil {
		http.NotFound(response, request)
		return
	}
	conversation, err := h.chat.Alliance(request.Context(), principal)
	if err != nil {
		handleChatReadError(response, request, err)
		return
	}
	h.renderChat(response, request, principal, &conversation, "/chat/alliance")
}

func (h *Handler) conversationChatPage(response http.ResponseWriter, request *http.Request) {
	principal, _, ok := h.requirePrincipal(response, request)
	if !ok {
		return
	}
	conversationID, err := chatConversationID(request)
	if err != nil || h.chat == nil {
		http.NotFound(response, request)
		return
	}
	conversation, err := h.chat.Conversation(request.Context(), principal, conversationID)
	if err != nil {
		handleChatReadError(response, request, err)
		return
	}
	h.renderChat(response, request, principal, &conversation,
		"/chat/conversations/"+strconv.FormatInt(conversationID, 10))
}

func (h *Handler) renderChat(response http.ResponseWriter, request *http.Request, principal appauth.Principal,
	conversation *appchat.Conversation, returnTo string) {
	if h.chat == nil || principal.MustChangePassword {
		http.NotFound(response, request)
		return
	}
	inbox, err := h.chat.Inbox(request.Context(), principal)
	if err != nil {
		handleChatReadError(response, request, err)
		return
	}
	var planets []appeconomy.Planet
	if h.economy != nil {
		planets, err = h.economy.Planets(request.Context(), principal)
		if err != nil && !errors.Is(err, appeconomy.ErrNoEmpire) {
			http.Error(response, "messaging unavailable", http.StatusInternalServerError)
			return
		}
	}
	token, ok := h.ensureCSRF(response, request)
	if !ok {
		return
	}
	data := chatPageData{
		pageShell:    h.gameShell(request.Context(), token, principal, "chat", planets, h.rememberedBody(request)),
		Inbox:        inbox,
		Conversation: conversation,
		ReturnTo:     returnTo,
	}
	if conversation != nil {
		key, ok := h.formKey(response, "chat-message:"+strconv.FormatInt(conversation.ID, 10))
		if !ok {
			return
		}
		data.FormKey = key
		if count := len(conversation.Messages); count > 0 {
			data.LastMessage = conversation.Messages[count-1].ID
		}
	}
	h.render(response, http.StatusOK, "chat", data)
}

func (h *Handler) chatUpdates(response http.ResponseWriter, request *http.Request) {
	principal, _, ok := h.requirePrincipal(response, request)
	if !ok {
		return
	}
	conversationID, err := chatConversationID(request)
	if err != nil || h.chat == nil {
		http.NotFound(response, request)
		return
	}
	afterID, _ := strconv.ParseInt(request.URL.Query().Get("after"), 10, 64)
	if afterID < 0 {
		afterID = 0
	}
	update, err := h.chat.Updates(request.Context(), principal, conversationID, afterID)
	if err != nil {
		handleChatReadError(response, request, err)
		return
	}
	writeChatJSON(response, http.StatusOK, update)
}

func (h *Handler) sendChatMessage(response http.ResponseWriter, request *http.Request) {
	principal, _, ok := h.requirePrincipal(response, request)
	if !ok {
		return
	}
	conversationID, err := chatConversationID(request)
	if err != nil || h.chat == nil {
		http.NotFound(response, request)
		return
	}
	if !h.validCSRF(response, request) {
		return
	}
	message, err := h.chat.Send(request.Context(), principal, conversationID, appchat.Draft{
		Body: request.PostFormValue("body"), GIFURL: request.PostFormValue("gif_url"),
		ClientKey: request.PostFormValue("client_key"),
	})
	if err != nil {
		if acceptsChatJSON(request) {
			writeChatJSON(response, chatMutationStatus(err), map[string]string{"error": chatError(err)})
			return
		}
		http.Error(response, chatError(err), chatMutationStatus(err))
		return
	}
	if acceptsChatJSON(request) {
		writeChatJSON(response, http.StatusCreated, message)
		return
	}
	http.Redirect(response, request, safeChatReturn(request.PostFormValue("return_to")), http.StatusSeeOther)
}

func (h *Handler) chatTyping(response http.ResponseWriter, request *http.Request) {
	principal, _, ok := h.requirePrincipal(response, request)
	if !ok {
		return
	}
	conversationID, err := chatConversationID(request)
	if err != nil || h.chat == nil {
		http.NotFound(response, request)
		return
	}
	if !h.validCSRF(response, request) {
		return
	}
	if err := h.chat.IsTyping(request.Context(), principal, conversationID); err != nil {
		handleChatReadError(response, request, err)
		return
	}
	response.WriteHeader(http.StatusNoContent)
}

func (h *Handler) administrativeChatsPage(response http.ResponseWriter, request *http.Request) {
	h.renderAdministrativeChats(response, request, 0)
}

func (h *Handler) administrativeChatPage(response http.ResponseWriter, request *http.Request) {
	conversationID, err := strconv.ParseInt(request.PathValue("conversation"), 10, 64)
	if err != nil {
		http.NotFound(response, request)
		return
	}
	h.renderAdministrativeChats(response, request, conversationID)
}

func (h *Handler) renderAdministrativeChats(response http.ResponseWriter, request *http.Request, conversationID int64) {
	principal, ok := h.requireAdministrator(response, request)
	if !ok {
		return
	}
	if h.chat == nil {
		http.NotFound(response, request)
		return
	}
	conversations, err := h.chat.AdministrativeInbox(request.Context(), principal)
	if err != nil {
		handleChatReadError(response, request, err)
		return
	}
	var selected *appchat.Conversation
	if conversationID > 0 {
		conversation, err := h.chat.AdministrativeConversation(request.Context(), principal, conversationID)
		if err != nil {
			handleChatReadError(response, request, err)
			return
		}
		selected = &conversation
	}
	token, ok := h.ensureCSRF(response, request)
	if !ok {
		return
	}
	shell := h.gameShell(request.Context(), token, principal, "admin",
		h.administrationBodies(request, principal), h.rememberedBody(request))
	h.render(response, http.StatusOK, "admin-chats", administrativeChatsPageData{
		pageShell: shell, Conversations: conversations, Conversation: selected,
	})
}

func chatConversationID(request *http.Request) (int64, error) {
	return strconv.ParseInt(request.PathValue("conversation"), 10, 64)
}

func handleChatReadError(response http.ResponseWriter, request *http.Request, err error) {
	switch {
	case errors.Is(err, appchat.ErrForbidden), errors.Is(err, appchat.ErrNotFound),
		errors.Is(err, appchat.ErrNoPlayer), errors.Is(err, appchat.ErrNoSuchPlayer),
		errors.Is(err, appchat.ErrSelfConversation), errors.Is(err, appchat.ErrNotInAlliance):
		http.NotFound(response, request)
	default:
		http.Error(response, "messaging unavailable", http.StatusInternalServerError)
	}
}

func chatMutationStatus(err error) int {
	if errors.Is(err, appchat.ErrNotFound) || errors.Is(err, appchat.ErrForbidden) {
		return http.StatusNotFound
	}
	return http.StatusBadRequest
}

func chatError(err error) string {
	switch {
	case errors.Is(err, appchat.ErrEmptyMessage):
		return "Écrivez un message ou ajoutez un GIF."
	case errors.Is(err, appchat.ErrMessageTooLong):
		return "Le message dépasse 2 000 caractères."
	case errors.Is(err, appchat.ErrInvalidGIF):
		return "Le GIF doit utiliser une URL HTTPS valide."
	case errors.Is(err, appchat.ErrInvalidKey):
		return "La clé du message est invalide. Rechargez la page."
	default:
		return "Le message n'a pas pu être envoyé."
	}
}

func acceptsChatJSON(request *http.Request) bool {
	return strings.Contains(request.Header.Get("Accept"), "application/json")
}

func writeChatJSON(response http.ResponseWriter, status int, value any) {
	response.Header().Set("Content-Type", "application/json; charset=utf-8")
	response.Header().Set("Cache-Control", "no-store")
	response.WriteHeader(status)
	_ = json.NewEncoder(response).Encode(value)
}

func safeChatReturn(value string) string {
	if value == "/chat/alliance" {
		return value
	}
	for _, prefix := range []string{"/chat/players/", "/chat/conversations/"} {
		if strings.HasPrefix(value, prefix) {
			if id, err := strconv.ParseInt(strings.TrimPrefix(value, prefix), 10, 64); err == nil && id > 0 {
				return value
			}
		}
	}
	return "/chat"
}
