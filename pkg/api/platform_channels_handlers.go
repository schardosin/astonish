package api

import (
	"encoding/json"
	"net/http"

	"github.com/gorilla/mux"
	"github.com/schardosin/astonish/pkg/config"
)

// channelAdapterInfo describes a channel adapter's configuration status.
type channelAdapterInfo struct {
	Type        string   `json:"type"`        // "telegram", "email", "slack"
	Configured  bool     `json:"configured"`  // whether all required secrets are set
	SecretKeys  []string `json:"secret_keys"` // which keys are configured (no values!)
	Description string   `json:"description"` // human-readable description
}

// channelSecretDefinition defines the required secrets for each channel type.
var channelSecretDefinitions = map[string]struct {
	description string
	keys        []string
}{
	"telegram": {
		description: "Telegram Bot (via BotFather)",
		keys:        []string{"channels.telegram.bot_token"},
	},
	"email": {
		description: "Email (IMAP/SMTP)",
		keys:        []string{"channels.email.password"},
	},
	"slack": {
		description: "Slack App",
		keys: []string{
			"channels.slack.bot_token",
			"channels.slack.app_token",
			"channels.slack.signing_secret",
		},
	},
}

// PlatformAdminListChannelsHandler handles GET /api/platform/admin/channels.
// Returns the configuration status of each channel adapter.
func PlatformAdminListChannelsHandler(w http.ResponseWriter, r *http.Request) {
	if RequirePlatformAdmin(w, r) == nil {
		return
	}

	pgStore := getPlatformPGStore()
	if pgStore == nil {
		respondError(w, http.StatusInternalServerError, "platform store not available")
		return
	}

	secrets := pgStore.PlatformSecrets()
	var adapters []channelAdapterInfo

	for adapterType, def := range channelSecretDefinitions {
		info := channelAdapterInfo{
			Type:        adapterType,
			Description: def.description,
		}

		configuredKeys := []string{}
		for _, key := range def.keys {
			if secrets.GetSecret(key) != "" {
				configuredKeys = append(configuredKeys, key)
			}
		}
		info.SecretKeys = configuredKeys
		info.Configured = len(configuredKeys) == len(def.keys)
		adapters = append(adapters, info)
	}

	respondJSON(w, http.StatusOK, adapters)
}

// PlatformAdminSetChannelSecretsHandler handles PUT /api/platform/admin/channels/{type}.
// Sets one or more secrets for a channel adapter.
func PlatformAdminSetChannelSecretsHandler(w http.ResponseWriter, r *http.Request) {
	if RequirePlatformAdmin(w, r) == nil {
		return
	}

	pgStore := getPlatformPGStore()
	if pgStore == nil {
		respondError(w, http.StatusInternalServerError, "platform store not available")
		return
	}

	channelType := mux.Vars(r)["type"]
	def, ok := channelSecretDefinitions[channelType]
	if !ok {
		respondError(w, http.StatusBadRequest, "unknown channel type: "+channelType)
		return
	}

	// Parse request body: map of secret key → value
	var body map[string]string
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		respondError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	// Validate that all provided keys belong to this channel type
	allowedKeys := make(map[string]bool, len(def.keys))
	for _, k := range def.keys {
		allowedKeys[k] = true
	}

	secrets := pgStore.PlatformSecrets()
	var saved []string

	for key, value := range body {
		if !allowedKeys[key] {
			respondError(w, http.StatusBadRequest, "key "+key+" is not valid for channel type "+channelType)
			return
		}
		if value == "" {
			// Empty value = remove
			_ = secrets.RemoveSecret(key)
		} else {
			if err := secrets.SetSecret(key, value); err != nil {
				respondError(w, http.StatusInternalServerError, "failed to save secret: "+err.Error())
				return
			}
			saved = append(saved, key)
		}
	}

	// Trigger automatic channel reload so the adapter picks up the new secrets
	// without requiring a manual daemon restart.
	reloadMsg := "Channel secrets updated."
	if reload := getChannelReloadFunc(); reload != nil {
		if err := reload(); err != nil {
			reloadMsg = "Channel secrets saved, but reload failed: " + err.Error() + ". Restart the daemon manually."
		} else {
			reloadMsg = "Channel secrets updated and channels reloaded successfully."
		}
	} else {
		reloadMsg = "Channel secrets updated. Restart the daemon for changes to take effect."
	}

	respondJSON(w, http.StatusOK, map[string]any{
		"saved":   saved,
		"message": reloadMsg,
	})
}

// PlatformAdminDeleteChannelHandler handles DELETE /api/platform/admin/channels/{type}.
// Removes all secrets for a channel adapter.
func PlatformAdminDeleteChannelHandler(w http.ResponseWriter, r *http.Request) {
	if RequirePlatformAdmin(w, r) == nil {
		return
	}

	pgStore := getPlatformPGStore()
	if pgStore == nil {
		respondError(w, http.StatusInternalServerError, "platform store not available")
		return
	}

	channelType := mux.Vars(r)["type"]
	def, ok := channelSecretDefinitions[channelType]
	if !ok {
		respondError(w, http.StatusBadRequest, "unknown channel type: "+channelType)
		return
	}

	secrets := pgStore.PlatformSecrets()
	for _, key := range def.keys {
		_ = secrets.RemoveSecret(key)
	}

	// Trigger channel reload to stop the adapter that lost its credentials.
	reloadMsg := "All secrets removed for channel " + channelType + "."
	if reload := getChannelReloadFunc(); reload != nil {
		if err := reload(); err != nil {
			reloadMsg += " Reload failed: " + err.Error()
		} else {
			reloadMsg += " Channels reloaded."
		}
	}

	respondJSON(w, http.StatusOK, map[string]any{
		"message": reloadMsg,
	})
}

// --- Standard MCP Servers (Web Services: Tavily, Brave, Firecrawl) ---

// webServiceInfo describes a standard MCP server's configuration status.
type webServiceInfo struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Category    string `json:"category"`
	Configured  bool   `json:"configured"`
	SecretKey   string `json:"secret_key"` // platform_secrets key used
}

// PlatformAdminListWebServicesHandler handles GET /api/platform/admin/web-services.
// Returns the configuration status of standard MCP servers (web services).
func PlatformAdminListWebServicesHandler(w http.ResponseWriter, r *http.Request) {
	if RequirePlatformAdmin(w, r) == nil {
		return
	}

	pgStore := getPlatformPGStore()
	if pgStore == nil {
		respondError(w, http.StatusInternalServerError, "platform store not available")
		return
	}

	secrets := pgStore.PlatformSecrets()
	stdServers := config.GetStandardServers()
	var services []webServiceInfo

	for _, srv := range stdServers {
		secretKey := "web_servers." + srv.ID + ".api_key"
		services = append(services, webServiceInfo{
			ID:          srv.ID,
			Name:        srv.DisplayName,
			Description: srv.Description,
			Category:    srv.Category,
			Configured:  secrets.GetSecret(secretKey) != "",
			SecretKey:   secretKey,
		})
	}

	respondJSON(w, http.StatusOK, services)
}

// PlatformAdminSetWebServiceKeyHandler handles PUT /api/platform/admin/web-services/{id}.
// Sets the API key for a standard MCP server.
func PlatformAdminSetWebServiceKeyHandler(w http.ResponseWriter, r *http.Request) {
	if RequirePlatformAdmin(w, r) == nil {
		return
	}

	pgStore := getPlatformPGStore()
	if pgStore == nil {
		respondError(w, http.StatusInternalServerError, "platform store not available")
		return
	}

	serverID := mux.Vars(r)["id"]
	srv := config.GetStandardServer(serverID)
	if srv == nil {
		respondError(w, http.StatusBadRequest, "unknown web service: "+serverID)
		return
	}

	var body struct {
		APIKey string `json:"api_key"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		respondError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	secretKey := "web_servers." + serverID + ".api_key"
	secrets := pgStore.PlatformSecrets()

	if body.APIKey == "" {
		_ = secrets.RemoveSecret(secretKey)
		respondJSON(w, http.StatusOK, map[string]any{
			"message": "API key removed for " + srv.DisplayName,
		})
		return
	}

	if err := secrets.SetSecret(secretKey, body.APIKey); err != nil {
		respondError(w, http.StatusInternalServerError, "failed to save secret: "+err.Error())
		return
	}

	respondJSON(w, http.StatusOK, map[string]any{
		"message": "API key saved for " + srv.DisplayName + ". Restart the daemon for changes to take effect.",
	})
}

// PlatformAdminDeleteWebServiceHandler handles DELETE /api/platform/admin/web-services/{id}.
// Removes the API key for a standard MCP server.
func PlatformAdminDeleteWebServiceHandler(w http.ResponseWriter, r *http.Request) {
	if RequirePlatformAdmin(w, r) == nil {
		return
	}

	pgStore := getPlatformPGStore()
	if pgStore == nil {
		respondError(w, http.StatusInternalServerError, "platform store not available")
		return
	}

	serverID := mux.Vars(r)["id"]
	srv := config.GetStandardServer(serverID)
	if srv == nil {
		respondError(w, http.StatusBadRequest, "unknown web service: "+serverID)
		return
	}

	secretKey := "web_servers." + serverID + ".api_key"
	_ = pgStore.PlatformSecrets().RemoveSecret(secretKey)

	respondJSON(w, http.StatusOK, map[string]any{
		"message": "API key removed for " + srv.DisplayName,
	})
}
