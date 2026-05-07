package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/mux"
	"github.com/schardosin/astonish/pkg/store"
)

// --- Link Code Store ---

var (
	linkCodeStoreMu sync.RWMutex
	linkCodeStore   *LinkCodeStore
)

// SetLinkCodeStore registers the link code store for API handlers.
func SetLinkCodeStore(s *LinkCodeStore) {
	linkCodeStoreMu.Lock()
	defer linkCodeStoreMu.Unlock()
	linkCodeStore = s
}

// GetLinkCodeStore returns the active link code store.
func GetLinkCodeStore() *LinkCodeStore {
	linkCodeStoreMu.RLock()
	defer linkCodeStoreMu.RUnlock()
	return linkCodeStore
}

// --- User Channel Management Endpoints ---
//
// These endpoints allow authenticated users to link and manage their
// external messaging channels (Telegram, Email). The linked channels
// are used for:
//   - Inbound message routing (Telegram → correct user/team context)
//   - Scheduler delivery (job results sent to the user's linked channels)
//   - Dynamic allowlist (replaces static config.yaml in platform mode)

// handleListUserChannels returns all channel links for the authenticated user.
// GET /api/user/channels
func handleListUserChannels(w http.ResponseWriter, r *http.Request) {
	user := GetPlatformUser(r)
	if user == nil {
		respondError(w, http.StatusUnauthorized, "authentication required")
		return
	}

	pg := getPlatformPGStore()
	if pg == nil {
		respondError(w, http.StatusNotImplemented, "user channels require platform mode")
		return
	}

	channels, err := pg.UserChannels().ListByUser(r.Context(), user.ID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to list channels")
		return
	}
	if channels == nil {
		channels = []*store.UserChannel{}
	}

	respondJSON(w, http.StatusOK, map[string]any{"channels": channels})
}

// handleLinkUserChannel creates a new channel link.
// POST /api/user/channels
// Body: { "channel_type": "telegram", "external_id": "123456789", "display_name": "@user" }
func handleLinkUserChannel(w http.ResponseWriter, r *http.Request) {
	user := GetPlatformUser(r)
	if user == nil {
		respondError(w, http.StatusUnauthorized, "authentication required")
		return
	}

	pg := getPlatformPGStore()
	if pg == nil {
		respondError(w, http.StatusNotImplemented, "user channels require platform mode")
		return
	}

	var req struct {
		ChannelType     string `json:"channel_type"`
		ExternalID      string `json:"external_id"`
		DisplayName     string `json:"display_name"`
		DefaultOrgSlug  string `json:"default_org_slug"`
		DefaultTeamSlug string `json:"default_team_slug"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.ChannelType == "" || req.ExternalID == "" {
		respondError(w, http.StatusBadRequest, "channel_type and external_id are required")
		return
	}
	if req.ChannelType != "telegram" && req.ChannelType != "email" {
		respondError(w, http.StatusBadRequest, "channel_type must be 'telegram' or 'email'")
		return
	}

	// Check if this external_id is already linked to another user
	existing, err := pg.UserChannels().GetByExternalID(r.Context(), req.ChannelType, req.ExternalID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to check existing links")
		return
	}
	if existing != nil {
		if existing.UserID == user.ID {
			respondError(w, http.StatusConflict, "channel already linked to your account")
		} else {
			respondError(w, http.StatusConflict, "channel already linked to another user")
		}
		return
	}

	// Use caller's org/team as defaults if not specified
	orgSlug := req.DefaultOrgSlug
	if orgSlug == "" {
		orgSlug = user.OrgSlug
	}
	teamSlug := req.DefaultTeamSlug
	if teamSlug == "" {
		teamSlug = user.TeamSlug
	}

	ch := &store.UserChannel{
		ID:              uuid.New().String(),
		UserID:          user.ID,
		ChannelType:     req.ChannelType,
		ExternalID:      req.ExternalID,
		DisplayName:     req.DisplayName,
		DefaultOrgSlug:  orgSlug,
		DefaultTeamSlug: teamSlug,
		Enabled:         true,
		Verified:        false,
		CreatedAt:       time.Now(),
	}

	if err := pg.UserChannels().Link(r.Context(), ch); err != nil {
		respondError(w, http.StatusInternalServerError, "failed to link channel")
		return
	}

	respondJSON(w, http.StatusCreated, map[string]any{
		"channel": ch,
		"message": "Channel linked. Verification required before it becomes active.",
	})
}

// handleUpdateUserChannel updates a channel link's mutable fields.
// PATCH /api/user/channels/{id}
func handleUpdateUserChannel(w http.ResponseWriter, r *http.Request) {
	user := GetPlatformUser(r)
	if user == nil {
		respondError(w, http.StatusUnauthorized, "authentication required")
		return
	}

	pg := getPlatformPGStore()
	if pg == nil {
		respondError(w, http.StatusNotImplemented, "user channels require platform mode")
		return
	}

	id := mux.Vars(r)["id"]
	ch, err := pg.UserChannels().GetByID(r.Context(), id)
	if err != nil {
		respondError(w, http.StatusNotFound, "channel link not found")
		return
	}

	// Ensure user owns this link
	if ch.UserID != user.ID {
		respondError(w, http.StatusForbidden, "not your channel link")
		return
	}

	var req struct {
		DisplayName     *string `json:"display_name"`
		DefaultOrgSlug  *string `json:"default_org_slug"`
		DefaultTeamSlug *string `json:"default_team_slug"`
		Enabled         *bool   `json:"enabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.DisplayName != nil {
		ch.DisplayName = *req.DisplayName
	}
	if req.DefaultOrgSlug != nil {
		ch.DefaultOrgSlug = *req.DefaultOrgSlug
	}
	if req.DefaultTeamSlug != nil {
		ch.DefaultTeamSlug = *req.DefaultTeamSlug
	}
	if req.Enabled != nil {
		ch.Enabled = *req.Enabled
	}

	if err := pg.UserChannels().Update(r.Context(), ch); err != nil {
		respondError(w, http.StatusInternalServerError, "failed to update channel")
		return
	}

	respondJSON(w, http.StatusOK, map[string]any{"channel": ch})
}

// handleUnlinkUserChannel removes a channel link.
// DELETE /api/user/channels/{id}
func handleUnlinkUserChannel(w http.ResponseWriter, r *http.Request) {
	user := GetPlatformUser(r)
	if user == nil {
		respondError(w, http.StatusUnauthorized, "authentication required")
		return
	}

	pg := getPlatformPGStore()
	if pg == nil {
		respondError(w, http.StatusNotImplemented, "user channels require platform mode")
		return
	}

	id := mux.Vars(r)["id"]
	ch, err := pg.UserChannels().GetByID(r.Context(), id)
	if err != nil {
		respondError(w, http.StatusNotFound, "channel link not found")
		return
	}

	// Ensure user owns this link
	if ch.UserID != user.ID {
		respondError(w, http.StatusForbidden, "not your channel link")
		return
	}

	if err := pg.UserChannels().Unlink(r.Context(), id); err != nil {
		respondError(w, http.StatusInternalServerError, "failed to unlink channel")
		return
	}

	respondJSON(w, http.StatusOK, map[string]string{"status": "unlinked"})
}

// handleVerifyUserChannel marks a channel as verified.
// POST /api/user/channels/{id}/verify
// This endpoint is called after the user proves ownership of the external channel.
// For Telegram: user sends a verification code to the bot.
// For now: verification is admin-driven or auto-verified if the user messages the bot.
func handleVerifyUserChannel(w http.ResponseWriter, r *http.Request) {
	user := GetPlatformUser(r)
	if user == nil {
		respondError(w, http.StatusUnauthorized, "authentication required")
		return
	}

	pg := getPlatformPGStore()
	if pg == nil {
		respondError(w, http.StatusNotImplemented, "user channels require platform mode")
		return
	}

	id := mux.Vars(r)["id"]
	ch, err := pg.UserChannels().GetByID(r.Context(), id)
	if err != nil {
		respondError(w, http.StatusNotFound, "channel link not found")
		return
	}

	// Ensure user owns this link
	if ch.UserID != user.ID {
		respondError(w, http.StatusForbidden, "not your channel link")
		return
	}

	if ch.Verified {
		respondJSON(w, http.StatusOK, map[string]any{"channel": ch, "message": "already verified"})
		return
	}

	if err := pg.UserChannels().Verify(r.Context(), id); err != nil {
		respondError(w, http.StatusInternalServerError, "failed to verify channel")
		return
	}

	ch.Verified = true
	now := time.Now()
	ch.VerifiedAt = &now
	respondJSON(w, http.StatusOK, map[string]any{"channel": ch, "message": "channel verified"})
}

// handleGenerateLinkCode creates a one-time link code for channel linking.
// POST /api/user/channels/link-code
// Body: { "channel_type": "telegram" }               → returns code + bot_username
// Body: { "channel_type": "email", "email": "..." }  → sends code via email
func handleGenerateLinkCode(w http.ResponseWriter, r *http.Request) {
	user := GetPlatformUser(r)
	if user == nil {
		respondError(w, http.StatusUnauthorized, "authentication required")
		return
	}

	store := GetLinkCodeStore()
	if store == nil {
		respondError(w, http.StatusNotImplemented, "link code service not available")
		return
	}

	var req struct {
		ChannelType string `json:"channel_type"`
		Email       string `json:"email"` // required for email channel
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.ChannelType == "" {
		req.ChannelType = "telegram"
	}

	switch req.ChannelType {
	case "telegram":
		// Generate the code
		code := store.Generate(user.ID, user.Email, user.OrgSlug, user.TeamSlug, "telegram")

		// Get bot username from channel manager
		botUsername := ""
		if cm := GetChannelManager(); cm != nil {
			botUsername = cm.GetTelegramBotUsername()
		}

		respondJSON(w, http.StatusOK, map[string]any{
			"code":         code,
			"bot_username": botUsername,
			"expires_in":   300,
		})

	case "email":
		// Validate email address
		emailAddr := strings.TrimSpace(strings.ToLower(req.Email))
		if emailAddr == "" || !strings.Contains(emailAddr, "@") {
			respondError(w, http.StatusBadRequest, "valid email address is required")
			return
		}

		// Check email channel is configured
		if getEmailClient() == nil {
			respondError(w, http.StatusServiceUnavailable, "email channel is not configured")
			return
		}

		// Generate the code
		code := store.Generate(user.ID, user.Email, user.OrgSlug, user.TeamSlug, "email:"+emailAddr)

		// Send verification email
		if err := sendEmailVerificationCode(r.Context(), emailAddr, code); err != nil {
			respondError(w, http.StatusInternalServerError, "failed to send verification email: "+err.Error())
			return
		}

		respondJSON(w, http.StatusOK, map[string]any{
			"code_sent":  true,
			"email":      emailAddr,
			"expires_in": 300,
		})

	default:
		respondError(w, http.StatusBadRequest, "unsupported channel type: "+req.ChannelType)
	}
}

// handleVerifyEmailCode validates a code the user received via email and
// creates a verified user_channel record linking their email address.
// POST /api/user/channels/verify-email-code
// Body: { "code": "ABC123" }
func handleVerifyEmailCode(w http.ResponseWriter, r *http.Request) {
	user := GetPlatformUser(r)
	if user == nil {
		respondError(w, http.StatusUnauthorized, "authentication required")
		return
	}

	linkStore := GetLinkCodeStore()
	if linkStore == nil {
		respondError(w, http.StatusNotImplemented, "link code service not available")
		return
	}

	var req struct {
		Code string `json:"code"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	code := strings.TrimSpace(strings.ToUpper(req.Code))
	if code == "" {
		respondError(w, http.StatusBadRequest, "code is required")
		return
	}

	// Consume the code
	pending := linkStore.Consume(code)
	if pending == nil {
		respondError(w, http.StatusBadRequest, "invalid or expired code")
		return
	}

	// Verify it belongs to this user
	if pending.UserID != user.ID {
		respondError(w, http.StatusForbidden, "code does not belong to this user")
		return
	}

	// The Channel field for email codes has the format "email:<address>"
	if !strings.HasPrefix(pending.Channel, "email:") {
		respondError(w, http.StatusBadRequest, "this code is not for email verification")
		return
	}
	emailAddr := strings.TrimPrefix(pending.Channel, "email:")

	// Check if this email is already linked to another user
	pgStore := getPlatformPGStore()
	if pgStore == nil {
		respondError(w, http.StatusInternalServerError, "platform store not available")
		return
	}
	ucStore := pgStore.UserChannels()

	existing, err := ucStore.GetByExternalID(r.Context(), "email", emailAddr)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "database error")
		return
	}
	if existing != nil && existing.UserID != user.ID {
		respondError(w, http.StatusConflict, "this email is already linked to a different user")
		return
	}
	if existing != nil && existing.UserID == user.ID {
		// Re-verify existing link
		_ = ucStore.Verify(r.Context(), existing.ID)
		if !existing.Enabled {
			existing.Enabled = true
			_ = ucStore.Update(r.Context(), existing)
		}
		// Refresh email allowlist
		refreshEmailAllowlist(r.Context(), pgStore)
		respondJSON(w, http.StatusOK, map[string]any{
			"message": "email re-verified",
			"channel": existing,
		})
		return
	}

	// Create new verified user_channel
	ch := &store.UserChannel{
		ID:              uuid.New().String(),
		UserID:          user.ID,
		ChannelType:     "email",
		ExternalID:      emailAddr,
		DisplayName:     emailAddr,
		DefaultOrgSlug:  pending.OrgSlug,
		DefaultTeamSlug: pending.TeamSlug,
		Enabled:         true,
		Verified:        true,
		VerifiedAt:      timePtr(time.Now()),
		CreatedAt:       time.Now(),
	}
	if err := ucStore.Link(r.Context(), ch); err != nil {
		respondError(w, http.StatusInternalServerError, "failed to link email: "+err.Error())
		return
	}

	// Refresh email allowlist so the channel starts accepting from this address
	refreshEmailAllowlist(r.Context(), pgStore)

	respondJSON(w, http.StatusOK, map[string]any{
		"message": "email linked and verified",
		"channel": ch,
	})
}

// refreshEmailAllowlist rebuilds the email channel's allowlist from the
// user_channels table. This ensures newly-linked email addresses are
// immediately accepted by the email channel.
func refreshEmailAllowlist(ctx context.Context, pgStore interface{ UserChannels() store.UserChannelStore }) {
	cm := GetChannelManager()
	if cm == nil {
		return
	}

	// Get all verified+enabled email channels from DB
	ucStore := pgStore.UserChannels()
	channels, err := ucStore.ListByChannelType(ctx, "email")
	if err != nil {
		return
	}

	// Build the new allowlist
	var addresses []string
	for _, ch := range channels {
		if ch.Enabled && ch.Verified {
			addresses = append(addresses, ch.ExternalID)
		}
	}

	// Update the channel manager's allowlists
	cm.UpdateAllowlists(map[string][]string{
		"email": addresses,
	})
}

func timePtr(t time.Time) *time.Time {
	return &t
}

// handleGetChannelInfo returns information about configured channels.
// GET /api/channels/info
// Response: { "telegram": { ... }, "email": { ... } }
func handleGetChannelInfo(w http.ResponseWriter, r *http.Request) {
	info := map[string]any{}

	// --- Telegram ---
	botUsername := ""
	tgConnected := false
	if cm := GetChannelManager(); cm != nil {
		botUsername = cm.GetTelegramBotUsername()
		tgConnected = botUsername != ""
	}

	tgEnabled := false
	tgError := ""
	emailEnabled := false
	emailError := ""
	emailAddress := ""
	if cfgStatuses := getChannelConfigStatuses(); cfgStatuses != nil {
		if tg, ok := cfgStatuses["telegram"]; ok {
			tgEnabled = tg.Enabled
			tgError = tg.Error
		}
		if em, ok := cfgStatuses["email"]; ok {
			emailEnabled = em.Enabled
			emailError = em.Error
		}
	}
	// If connected, it's definitely enabled
	if tgConnected {
		tgEnabled = true
	}

	info["telegram"] = map[string]any{
		"bot_username": botUsername,
		"configured":   tgConnected, // bot is actually authenticated and has a username
		"enabled":      tgEnabled,   // channel is enabled in config
		"error":        tgError,
	}

	// --- Email ---
	emailConnected := false
	if cm := GetChannelManager(); cm != nil {
		if addr := cm.GetEmailAddress(); addr != "" {
			emailConnected = true
			emailAddress = addr
		}
	}
	if emailConnected {
		emailEnabled = true
	}

	info["email"] = map[string]any{
		"configured": emailConnected,
		"enabled":    emailEnabled,
		"error":      emailError,
		"address":    emailAddress,
	}

	respondJSON(w, http.StatusOK, info)
}
