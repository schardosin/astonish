package slack

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/slack-go/slack"
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

// InstallHandler redirects the user to Slack's OAuth authorization page.
// Mount at: GET /slack/install
func (h *OAuthHandler) InstallHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		scopeStr := ""
		for i, s := range h.config.Scopes {
			if i > 0 {
				scopeStr += ","
			}
			scopeStr += s
		}

		// Generate a random state for CSRF protection
		state := fmt.Sprintf("%d", time.Now().UnixNano())

		url := fmt.Sprintf(
			"https://slack.com/oauth/v2/authorize?client_id=%s&scope=%s&redirect_uri=%s&state=%s",
			h.config.ClientID,
			scopeStr,
			h.config.RedirectURI,
			state,
		)

		http.Redirect(w, r, url, http.StatusFound)
	})
}

// CallbackHandler processes the OAuth callback from Slack.
// It exchanges the temporary code for a bot token and stores the installation.
// Mount at: GET /slack/callback
func (h *OAuthHandler) CallbackHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		code := r.URL.Query().Get("code")
		if code == "" {
			http.Error(w, "missing code parameter", http.StatusBadRequest)
			return
		}

		// Check for error from Slack (user denied)
		if errParam := r.URL.Query().Get("error"); errParam != "" {
			http.Error(w, fmt.Sprintf("Slack OAuth error: %s", errParam), http.StatusBadRequest)
			return
		}

		// Exchange code for token
		resp, err := slack.GetOAuthV2Response(
			http.DefaultClient,
			h.config.ClientID,
			h.config.ClientSecret,
			code,
			h.config.RedirectURI,
		)
		if err != nil {
			h.logger("[slack] OAuth token exchange failed: %v", err)
			http.Error(w, "OAuth token exchange failed", http.StatusInternalServerError)
			return
		}

		// Build installation record
		install := &SlackInstallation{
			TeamID:      resp.Team.ID,
			TeamName:    resp.Team.Name,
			BotToken:    resp.AccessToken,
			BotUserID:   resp.BotUserID,
			InstallerID: resp.AuthedUser.ID,
			InstalledAt: time.Now(),
		}

		// Store the installation
		if err := h.store.SaveInstallation(r.Context(), install); err != nil {
			h.logger("[slack] Failed to save installation for team %s: %v", install.TeamID, err)
			http.Error(w, "Failed to save installation", http.StatusInternalServerError)
			return
		}

		// Register the workspace with the channel adapter
		h.channel.RegisterWorkspace(install.TeamID, install.BotToken, install.BotUserID)

		h.logger("[slack] New workspace installed: %s (%s) by user %s",
			install.TeamName, install.TeamID, install.InstallerID)

		// Return success page
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, `<!DOCTYPE html>
<html>
<head><title>Slack App Installed</title></head>
<body style="font-family: sans-serif; text-align: center; padding: 60px;">
<h1>Success!</h1>
<p>Astonish has been installed to <strong>%s</strong>.</p>
<p>You can close this window and start chatting with the bot in Slack.</p>
</body>
</html>`, install.TeamName)
	})
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
