// Package chat orchestrates private and alliance conversations.
package chat

import (
	"context"
	"errors"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	appauth "universeatwar/internal/app/authentication"
	domainclock "universeatwar/internal/domain/clock"
)

const (
	MaxTextRunes = 2000
	MaxGIFBytes  = 2048
	messageLimit = 100
	typingLife   = 5 * time.Second
)

var (
	ErrForbidden        = errors.New("chat: this account cannot access conversations")
	ErrNotFound         = errors.New("chat: conversation not found")
	ErrNoPlayer         = errors.New("chat: this account has no player")
	ErrNoSuchPlayer     = errors.New("chat: target player not found")
	ErrSelfConversation = errors.New("chat: cannot open a conversation with oneself")
	ErrNotInAlliance    = errors.New("chat: player belongs to no alliance")
	ErrEmptyMessage     = errors.New("chat: a message needs text or a GIF")
	ErrMessageTooLong   = errors.New("chat: message is too long")
	ErrInvalidGIF       = errors.New("chat: GIF URL must be a valid HTTPS URL")
	ErrInvalidKey       = errors.New("chat: invalid client key")
)

// Kind separates one-to-one discussions from the common room of an alliance.
type Kind string

const (
	Direct   Kind = "direct"
	Alliance Kind = "alliance"
)

// Message is one immutable entry in a conversation.
type Message struct {
	ID             int64     `json:"id"`
	ConversationID int64     `json:"conversation_id"`
	AuthorPlayerID int64     `json:"author_player_id"`
	AuthorName     string    `json:"author_name"`
	Body           string    `json:"body"`
	GIFURL         string    `json:"gif_url,omitempty"`
	CreatedAt      time.Time `json:"created_at"`
	Own            bool      `json:"own"`
	FromInitiator  bool      `json:"from_initiator,omitempty"`
}

// Conversation is a room and the requested window of its messages.
type Conversation struct {
	ID             int64
	Kind           Kind
	Title          string
	TargetPlayerID int64
	AllianceID     int64
	Messages       []Message
}

// Summary is one room in the player's or administrator's conversation list.
type Summary struct {
	ID          int64
	Kind        Kind
	Title       string
	LastMessage string
	UpdatedAt   time.Time
}

// Inbox lists the rooms visible to a player and whether an alliance room can
// be opened even when nobody has written there yet.
type Inbox struct {
	Conversations []Summary
	HasAlliance   bool
}

// Draft is untrusted message input after the HTTP adapter has decoded it.
type Draft struct {
	Body      string
	GIFURL    string
	ClientKey string
}

// Update is the incremental state consumed by the dynamic interface.
type Update struct {
	Messages []Message `json:"messages"`
	Typing   []string  `json:"typing"`
}

// Member identifies the player behind an account after conversation access
// has been checked by the repository.
type Member struct {
	PlayerID int64
	Name     string
}

// Repository is the persistence and authorization boundary of messaging.
type Repository interface {
	ListForAccount(context.Context, int64) (Inbox, error)
	UnreadCount(context.Context, int64) (int, error)
	OpenDirect(context.Context, int64, int64, time.Time, int) (Conversation, error)
	OpenAlliance(context.Context, int64, time.Time, int) (Conversation, error)
	ConversationForAccount(context.Context, int64, int64, int64, int) (Conversation, error)
	MarkReadThrough(context.Context, int64, int64, int64, time.Time) error
	Append(context.Context, int64, int64, Draft, time.Time) (Message, error)
	MemberForConversation(context.Context, int64, int64) (Member, error)
	ListAll(context.Context) ([]Summary, error)
	ConversationForAdministrator(context.Context, int64, int64, int) (Conversation, error)
}

// Service applies player and administrator permissions around the repository.
type Service struct {
	Clock      domainclock.Clock
	Repository Repository
	Typing     *TypingTracker
}

func (s Service) Inbox(ctx context.Context, principal appauth.Principal) (Inbox, error) {
	if err := s.player(principal); err != nil {
		return Inbox{}, err
	}
	return s.Repository.ListForAccount(ctx, principal.AccountID)
}

// UnreadCount reports messages authored by somebody else in conversations the
// player can currently access.
func (s Service) UnreadCount(ctx context.Context, principal appauth.Principal) (int, error) {
	if err := s.player(principal); err != nil {
		return 0, err
	}
	return s.Repository.UnreadCount(ctx, principal.AccountID)
}

func (s Service) Direct(ctx context.Context, principal appauth.Principal, targetPlayerID int64) (Conversation, error) {
	if err := s.player(principal); err != nil {
		return Conversation{}, err
	}
	if targetPlayerID <= 0 {
		return Conversation{}, ErrNoSuchPlayer
	}
	now := s.Clock.Now().UTC()
	conversation, err := s.Repository.OpenDirect(ctx, principal.AccountID, targetPlayerID, now, messageLimit)
	if err != nil {
		return Conversation{}, err
	}
	return conversation, s.markRead(ctx, principal.AccountID, conversation, now)
}

func (s Service) Alliance(ctx context.Context, principal appauth.Principal) (Conversation, error) {
	if err := s.player(principal); err != nil {
		return Conversation{}, err
	}
	now := s.Clock.Now().UTC()
	conversation, err := s.Repository.OpenAlliance(ctx, principal.AccountID, now, messageLimit)
	if err != nil {
		return Conversation{}, err
	}
	return conversation, s.markRead(ctx, principal.AccountID, conversation, now)
}

func (s Service) Conversation(ctx context.Context, principal appauth.Principal, conversationID int64) (Conversation, error) {
	if err := s.player(principal); err != nil {
		return Conversation{}, err
	}
	if conversationID <= 0 {
		return Conversation{}, ErrNotFound
	}
	conversation, err := s.Repository.ConversationForAccount(ctx, principal.AccountID, conversationID, 0, messageLimit)
	if err != nil {
		return Conversation{}, err
	}
	return conversation, s.markRead(ctx, principal.AccountID, conversation, s.Clock.Now().UTC())
}

func (s Service) Updates(ctx context.Context, principal appauth.Principal, conversationID, afterID int64) (Update, error) {
	if err := s.player(principal); err != nil {
		return Update{}, err
	}
	member, err := s.Repository.MemberForConversation(ctx, principal.AccountID, conversationID)
	if err != nil {
		return Update{}, err
	}
	conversation, err := s.Repository.ConversationForAccount(ctx, principal.AccountID, conversationID, afterID, messageLimit)
	if err != nil {
		return Update{}, err
	}
	if err := s.markRead(ctx, principal.AccountID, conversation, s.Clock.Now().UTC()); err != nil {
		return Update{}, err
	}
	return Update{Messages: conversation.Messages, Typing: s.typing().Active(conversationID, member.PlayerID, s.Clock.Now().UTC())}, nil
}

func (s Service) markRead(ctx context.Context, accountID int64, conversation Conversation, now time.Time) error {
	if len(conversation.Messages) == 0 {
		return nil
	}
	return s.Repository.MarkReadThrough(ctx, accountID, conversation.ID,
		conversation.Messages[len(conversation.Messages)-1].ID, now)
}

func (s Service) Send(ctx context.Context, principal appauth.Principal, conversationID int64, draft Draft) (Message, error) {
	if err := s.player(principal); err != nil {
		return Message{}, err
	}
	normalized, err := normalizeDraft(draft)
	if err != nil {
		return Message{}, err
	}
	message, err := s.Repository.Append(ctx, principal.AccountID, conversationID, normalized, s.Clock.Now().UTC())
	if err == nil {
		s.typing().Clear(conversationID, message.AuthorPlayerID)
	}
	return message, err
}

func (s Service) IsTyping(ctx context.Context, principal appauth.Principal, conversationID int64) error {
	if err := s.player(principal); err != nil {
		return err
	}
	member, err := s.Repository.MemberForConversation(ctx, principal.AccountID, conversationID)
	if err != nil {
		return err
	}
	s.typing().Touch(conversationID, member, s.Clock.Now().UTC())
	return nil
}

func (s Service) AdministrativeInbox(ctx context.Context, principal appauth.Principal) ([]Summary, error) {
	if err := s.administrator(principal); err != nil {
		return nil, err
	}
	return s.Repository.ListAll(ctx)
}

func (s Service) AdministrativeConversation(ctx context.Context, principal appauth.Principal, conversationID int64) (Conversation, error) {
	if err := s.administrator(principal); err != nil {
		return Conversation{}, err
	}
	if conversationID <= 0 {
		return Conversation{}, ErrNotFound
	}
	return s.Repository.ConversationForAdministrator(ctx, conversationID, 0, messageLimit)
}

func (s Service) player(principal appauth.Principal) error {
	if s.Clock == nil || s.Repository == nil {
		return errors.New("chat: incomplete service dependencies")
	}
	if principal.AccountID <= 0 || principal.MustChangePassword || !principal.HasRole(appauth.RolePlayer) {
		return ErrForbidden
	}
	return nil
}

func (s Service) administrator(principal appauth.Principal) error {
	if s.Clock == nil || s.Repository == nil {
		return errors.New("chat: incomplete service dependencies")
	}
	if principal.AccountID <= 0 || principal.MustChangePassword || !principal.HasRole(appauth.RoleAdmin) {
		return ErrForbidden
	}
	return nil
}

func (s Service) typing() *TypingTracker {
	if s.Typing == nil {
		// A service is normally composed with a tracker. Keeping the zero value
		// usable makes command-line and narrowly scoped tests degrade gracefully.
		return &TypingTracker{}
	}
	return s.Typing
}

func normalizeDraft(draft Draft) (Draft, error) {
	draft.Body = strings.TrimSpace(draft.Body)
	draft.GIFURL = strings.TrimSpace(draft.GIFURL)
	draft.ClientKey = strings.TrimSpace(draft.ClientKey)
	if draft.Body == "" && draft.GIFURL == "" {
		return Draft{}, ErrEmptyMessage
	}
	if !utf8.ValidString(draft.Body) || utf8.RuneCountInString(draft.Body) > MaxTextRunes {
		return Draft{}, ErrMessageTooLong
	}
	if len(draft.ClientKey) < 8 || len(draft.ClientKey) > 128 {
		return Draft{}, ErrInvalidKey
	}
	if draft.GIFURL != "" {
		if len(draft.GIFURL) > MaxGIFBytes {
			return Draft{}, ErrInvalidGIF
		}
		parsed, err := url.ParseRequestURI(draft.GIFURL)
		if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil {
			return Draft{}, ErrInvalidGIF
		}
	}
	return draft, nil
}

type typingEntry struct {
	name      string
	expiresAt time.Time
}

// TypingTracker is intentionally process-local and short-lived. Losing it on
// restart merely removes an ephemeral hint; no message or conversation is lost.
type TypingTracker struct {
	mu      sync.Mutex
	entries map[int64]map[int64]typingEntry
}

func (t *TypingTracker) Touch(conversationID int64, member Member, now time.Time) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.entries == nil {
		t.entries = make(map[int64]map[int64]typingEntry)
	}
	if t.entries[conversationID] == nil {
		t.entries[conversationID] = make(map[int64]typingEntry)
	}
	t.entries[conversationID][member.PlayerID] = typingEntry{name: member.Name, expiresAt: now.Add(typingLife)}
}

func (t *TypingTracker) Clear(conversationID, playerID int64) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.entries != nil {
		delete(t.entries[conversationID], playerID)
	}
}

func (t *TypingTracker) Active(conversationID, exceptPlayerID int64, now time.Time) []string {
	t.mu.Lock()
	defer t.mu.Unlock()
	var names []string
	for playerID, entry := range t.entries[conversationID] {
		if !entry.expiresAt.After(now) {
			delete(t.entries[conversationID], playerID)
			continue
		}
		if playerID != exceptPlayerID {
			names = append(names, entry.name)
		}
	}
	sort.Strings(names)
	return names
}
