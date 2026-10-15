package slack

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// OAuthConfig holds configuration for the Slack OAuth installation flow.
type OAuthConfig struct {
	ClientID     string
	ClientSecret string
	RedirectURI  string // e.g., "https://yourdomain.com/slack/callback"
	Scopes       []string
}

// SlackInstallation represents a workspace that has installed the Slack app.
type SlackInstallation struct {
	TeamID      string    `json:"team_id"`
	TeamName    string    `json:"team_name"`
	BotToken    string    `json:"bot_token"`
	BotUserID   string    `json:"bot_user_id"`
	InstallerID string    `json:"installer_id"`
	InstalledAt time.Time `json:"installed_at"`
}

// InstallationStore is the interface for persisting Slack workspace installations.
type InstallationStore interface {
	SaveInstallation(ctx context.Context, install *SlackInstallation) error
	GetInstallation(ctx context.Context, teamID string) (*SlackInstallation, error)
	DeleteInstallation(ctx context.Context, teamID string) error
	ListInstallations(ctx context.Context) ([]*SlackInstallation, error)
}

// OAuthHandler provides HTTP handlers for the Slack OAuth installation flow.
type OAuthHandler struct {
	config  *OAuthConfig
	store   InstallationStore
	channel *SlackChannel
	logger  func(format string, args ...any)
}

// NewOAuthHandler creates a new OAuth handler.
func NewOAuthHandler(cfg *OAuthConfig, store InstallationStore, channel *SlackChannel) *OAuthHandler {
	scopes := cfg.Scopes
	if len(scopes) == 0 {
		// Default scopes for a bot
		scopes = []string{
			"app_mentions:read",
			"chat:write",
			"im:history",
			"im:read",
			"im:write",
			"users:read",
		}
	}
	cfg.Scopes = scopes

	return &OAuthHandler{
		config:  cfg,
		store:   store,
		channel: channel,
		logger:  channel.logger.Printf,
	}
}

// InstallHandler is deliberately disabled. The daemon has never wired this
// OAuth flow; retaining its timestamp state and unchecked callback would leave
// an unsafe installation path available to future callers. A future flow must
// use cryptographically random, expiring, one-time browser-bound state, fail
// closed on entropy errors, and escape all workspace values rendered as HTML.
func (h *OAuthHandler) InstallHandler() http.Handler {
	return http.NotFoundHandler()
}

// CallbackHandler is disabled together with InstallHandler. No code exchange,
// installation mutation or workspace HTML rendering is performed.
func (h *OAuthHandler) CallbackHandler() http.Handler {
	return http.NotFoundHandler()
}

// UninstallEvent handles the app_uninstalled event from Slack.
// Call this when processing an app_uninstalled event.
func (h *OAuthHandler) UninstallEvent(ctx context.Context, teamID string) {
	if err := h.store.DeleteInstallation(ctx, teamID); err != nil {
		h.logger("[slack] Failed to delete installation for team %s: %v", teamID, err)
	}
	h.channel.UnregisterWorkspace(teamID)
	h.logger("[slack] Workspace %s uninstalled", teamID)
}

// LoadInstallations loads all stored installations and registers their API clients
// with the channel adapter. Call this on daemon startup.
func (h *OAuthHandler) LoadInstallations(ctx context.Context) error {
	installs, err := h.store.ListInstallations(ctx)
	if err != nil {
		return fmt.Errorf("failed to load slack installations: %w", err)
	}

	for _, install := range installs {
		h.channel.RegisterWorkspace(install.TeamID, install.BotToken, install.BotUserID)
	}

	if len(installs) > 0 {
		h.logger("[slack] Loaded %d workspace installation(s)", len(installs))
	}

	return nil
}

// StatusResponse returns a JSON-friendly status of all installations.
func (h *OAuthHandler) StatusResponse(ctx context.Context) ([]map[string]any, error) {
	installs, err := h.store.ListInstallations(ctx)
	if err != nil {
		return nil, err
	}

	var result []map[string]any
	for _, inst := range installs {
		result = append(result, map[string]any{
			"team_id":      inst.TeamID,
			"team_name":    inst.TeamName,
			"bot_user_id":  inst.BotUserID,
			"installed_at": inst.InstalledAt,
		})
	}
	return result, nil
}

// respondJSON writes a JSON response.
//
//nolint:unused // Will be used when OAuth HTTP routes are wired
func respondJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}
