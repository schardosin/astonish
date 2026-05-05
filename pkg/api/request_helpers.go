package api

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/schardosin/astonish/pkg/config"
	"github.com/schardosin/astonish/pkg/store"
	"github.com/schardosin/astonish/pkg/store/filestore"
	"github.com/schardosin/astonish/pkg/store/pgstore"
)

// effectiveUserID returns the user ID for the current request.
//
// In platform mode, this is the authenticated user's ID from the JWT token.
// In personal mode, this falls back to the hardcoded "studio_user" constant.
//
// All handlers that create or query user-scoped data (sessions, apps, etc.)
// should call this instead of using studioChatUserID directly.
func effectiveUserID(r *http.Request) string {
	if pu := GetPlatformUser(r); pu != nil {
		return pu.ID
	}
	return studioChatUserID
}

// effectiveCredentialStore returns the credential store for the current request,
// optionally scoped by the "scope" query parameter.
//
// Scope values:
//   - "personal": returns the user's personal credential store
//   - "team": returns the team-scoped credential store
//   - "" (empty/omitted): returns a merged store (personal-first, team-fallback)
//
// In personal mode (no platform), always returns the file-based singleton.
func effectiveCredentialStore(r *http.Request) store.CredentialStore {
	scope := r.URL.Query().Get("scope")
	return effectiveCredentialStoreScoped(r, scope)
}

// effectiveCredentialStoreScoped returns the credential store for the given scope.
func effectiveCredentialStoreScoped(r *http.Request, scope string) store.CredentialStore {
	if svc := store.FromRequest(r); svc != nil && svc.Mode == store.ModePlatform {
		switch scope {
		case "personal":
			return svc.PersonalCredentials
		case "team":
			return svc.Credentials
		default:
			// Merged: personal-first, team-fallback
			if svc.PersonalCredentials != nil || svc.Credentials != nil {
				return store.NewMergedCredentialStore(svc.PersonalCredentials, svc.Credentials)
			}
			return svc.Credentials
		}
	}
	// Fall back to the personal-mode singleton.
	if cs := getAPICredentialStore(); cs != nil {
		return filestore.NewCredentialStore(cs)
	}
	return nil
}

// effectivePersonalCredentialStore returns just the personal credential store.
func effectivePersonalCredentialStore(r *http.Request) store.CredentialStore {
	if svc := store.FromRequest(r); svc != nil && svc.PersonalCredentials != nil {
		return svc.PersonalCredentials
	}
	// In personal mode, the single store IS the personal store.
	if cs := getAPICredentialStore(); cs != nil {
		return filestore.NewCredentialStore(cs)
	}
	return nil
}

// effectiveTeamCredentialStore returns just the team credential store.
func effectiveTeamCredentialStore(r *http.Request) store.CredentialStore {
	if svc := store.FromRequest(r); svc != nil && svc.Credentials != nil {
		return svc.Credentials
	}
	return nil
}

// isPlatformMode checks whether the current request is running in platform mode
// by inspecting the Services context. Returns false for personal mode or when
// Services is not available.
// Used by handlers in tasks 4.3-4.8 for platform-mode branching.
func isPlatformMode(r *http.Request) bool {
	svc := store.FromRequest(r)
	return svc != nil && svc.Mode == store.ModePlatform
}

// effectiveTeamSlug returns the team slug for the current request.
// In platform mode, this is read from the TenantContext (set by auth middleware).
// In personal mode, this returns an empty string.
func effectiveTeamSlug(r *http.Request) string {
	if tc := pgstore.TenantContextFrom(r.Context()); tc != nil {
		return tc.TeamSlug
	}
	return ""
}

// effectiveMCPStore returns the MCP server store for the current request based
// on the "scope" query parameter.
//
// Scope values:
//   - "team": returns the team-scoped MCP server store
//   - "org" or "" (empty): returns the org-level MCP server store
//
// Returns nil if not in platform mode or if the requested store is not available.
func effectiveMCPStore(r *http.Request) store.MCPServerStore {
	svc := store.FromRequest(r)
	if svc == nil || svc.Mode != store.ModePlatform {
		return nil
	}
	scope := r.URL.Query().Get("scope")
	switch scope {
	case "team":
		return svc.TeamMCPServers
	default:
		return svc.MCPServers
	}
}

// DefaultUserID returns the default user ID used in personal mode and for
// system-initiated operations (scheduled fleet sessions, recovery, etc.).
// External packages that need the default user ID should call this instead of
// hardcoding the string.
func DefaultUserID() string {
	return studioChatUserID
}

// storeMetaToResponse converts a store.SessionMeta to the API response type.
func storeMetaToResponse(m store.SessionMeta) StudioSessionResponse {
	return StudioSessionResponse{
		ID:           m.ID,
		Title:        m.Title,
		CreatedAt:    m.CreatedAt.Format(time.RFC3339),
		UpdatedAt:    m.UpdatedAt.Format(time.RFC3339),
		MessageCount: m.MessageCount,
		FleetKey:     m.FleetKey,
		FleetName:    m.FleetName,
		IssueNumber:  m.IssueNumber,
		Repo:         m.Repo,
	}
}

// loadMCPConfigForRequest returns the merged MCP server config appropriate for
// the request context. In platform mode it reads from the org+team DB stores;
// in personal mode it reads from the filesystem.
func loadMCPConfigForRequest(r *http.Request) *config.MCPConfig {
	if svc := store.FromRequest(r); svc != nil && svc.Mode == store.ModePlatform {
		merged := make(map[string]config.MCPServerConfig)
		if svc.MCPServers != nil {
			orgServers, err := svc.MCPServers.List()
			if err == nil {
				for _, s := range orgServers {
					merged[s.Name] = config.MCPServerConfig{
						Command:   s.Command,
						Args:      s.Args,
						Env:       s.Env,
						Transport: s.Transport,
						URL:       s.URL,
						Enabled:   s.Enabled,
					}
				}
			}
		}
		if svc.TeamMCPServers != nil {
			teamServers, err := svc.TeamMCPServers.List()
			if err == nil {
				for _, s := range teamServers {
					merged[s.Name] = config.MCPServerConfig{
						Command:   s.Command,
						Args:      s.Args,
						Env:       s.Env,
						Transport: s.Transport,
						URL:       s.URL,
						Enabled:   s.Enabled,
					}
				}
			}
		}
		cfg := &config.MCPConfig{MCPServers: merged}
		// Merge standard servers (Tavily, Brave, etc.) so they appear alongside
		// user-configured DB servers in platform mode.
		config.MergeStandardServers(cfg)
		return cfg
	}

	// Personal mode
	mcpCfg, err := config.LoadMCPConfig()
	if err != nil {
		slog.Warn("failed to load MCP config for request", "error", err)
		return &config.MCPConfig{MCPServers: make(map[string]config.MCPServerConfig)}
	}
	return mcpCfg
}

// effectiveAppConfig returns the application configuration appropriate for the request.
// In platform mode, it loads the host config and overlays team-level settings from the DB.
// In personal mode, it simply returns config.LoadAppConfig().
func effectiveAppConfig(r *http.Request) *config.AppConfig {
	appCfg, err := config.LoadAppConfig()
	if err != nil {
		slog.Warn("failed to load app config", "error", err)
		appCfg = &config.AppConfig{}
	}

	svc := store.FromRequest(r)
	if svc == nil || svc.Mode != store.ModePlatform || svc.Settings == nil {
		return appCfg
	}

	// Platform mode: overlay team settings from DB
	teamSettings, err := svc.Settings.Get(r.Context())
	if err != nil {
		slog.Warn("failed to load team settings", "error", err)
		return appCfg
	}
	if teamSettings == nil {
		return appCfg
	}

	// Overlay team settings onto host config
	applyTeamSettings(appCfg, teamSettings)
	return appCfg
}

// applyTeamSettings overlays team-level settings from the DB onto the host AppConfig.
// Only non-zero/non-empty team values override the host defaults.
func applyTeamSettings(appCfg *config.AppConfig, ts *store.TeamSettings) {
	if ts.DefaultProvider != "" {
		appCfg.General.DefaultProvider = ts.DefaultProvider
	}
	if ts.DefaultModel != "" {
		appCfg.General.DefaultModel = ts.DefaultModel
	}
	if ts.WebSearchTool != "" {
		appCfg.General.WebSearchTool = ts.WebSearchTool
	}
	if ts.WebExtractTool != "" {
		appCfg.General.WebExtractTool = ts.WebExtractTool
	}
	if ts.ContextLength > 0 {
		appCfg.General.ContextLength = ts.ContextLength
	}

	// Overlay providers: team providers replace host providers entirely if set
	if len(ts.Providers) > 0 {
		if appCfg.Providers == nil {
			appCfg.Providers = make(map[string]config.ProviderConfig)
		}
		for name, provCfg := range ts.Providers {
			appCfg.Providers[name] = config.ProviderConfig(provCfg)
		}
	}

	// Overlay web servers: team web server configs replace host configs
	if len(ts.WebServers) > 0 {
		if appCfg.WebServers == nil {
			appCfg.WebServers = make(map[string]config.WebServerConfig)
		}
		for name, ws := range ts.WebServers {
			appCfg.WebServers[name] = config.WebServerConfig{
				APIKey:    ws["api_key"],
				Installed: ws["installed"] == "true",
			}
		}
	}

	// Overlay memory settings
	if ts.MemoryProvider != "" {
		appCfg.Memory.Embedding.Provider = ts.MemoryProvider
	}
	if ts.MemoryModel != "" {
		appCfg.Memory.Embedding.Model = ts.MemoryModel
	}
}
