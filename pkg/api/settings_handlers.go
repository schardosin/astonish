package api

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/gorilla/mux"
	"github.com/schardosin/astonish/pkg/cache"
	"github.com/schardosin/astonish/pkg/common"
	"github.com/schardosin/astonish/pkg/config"
	"github.com/schardosin/astonish/pkg/credentials"
	"github.com/schardosin/astonish/pkg/mcp"
	"github.com/schardosin/astonish/pkg/provider"
	"github.com/schardosin/astonish/pkg/provider/anthropic"
	"github.com/schardosin/astonish/pkg/provider/google"
	"github.com/schardosin/astonish/pkg/provider/groq"
	"github.com/schardosin/astonish/pkg/provider/lmstudio"
	"github.com/schardosin/astonish/pkg/provider/ollama"
	openai_provider "github.com/schardosin/astonish/pkg/provider/openai"
	"github.com/schardosin/astonish/pkg/provider/openai_compat"
	"github.com/schardosin/astonish/pkg/provider/openrouter"
	"github.com/schardosin/astonish/pkg/provider/poe"
	"github.com/schardosin/astonish/pkg/provider/sap"
	"github.com/schardosin/astonish/pkg/provider/xai"
	"github.com/schardosin/astonish/pkg/version"
)

// --- Package-level credential store for API handlers ---

var (
	apiCredStoreMu sync.RWMutex
	apiCredStore   *credentials.Store
)

// SetAPICredentialStore registers the credential store for use by API handlers.
// Called during startup (daemon/factory).
func SetAPICredentialStore(s *credentials.Store) {
	apiCredStoreMu.Lock()
	defer apiCredStoreMu.Unlock()
	apiCredStore = s
}

// getAPICredentialStore returns the registered credential store (or nil).
func getAPICredentialStore() *credentials.Store {
	apiCredStoreMu.RLock()
	defer apiCredStoreMu.RUnlock()
	return apiCredStore
}

// injectProviderSecrets injects secrets from the credential store into a
// freshly-loaded AppConfig. API handlers load config from disk per request
// (to pick up changes), but the on-disk config has scrubbed API keys after
// credential migration. This re-hydrates them from the encrypted store.
func injectProviderSecrets(appCfg *config.AppConfig) {
	store := getAPICredentialStore()
	if store == nil || appCfg == nil {
		return
	}
	config.InjectProviderSecretsToConfig(appCfg, store.GetSecret)
}

// resolveProviderSecret looks up a provider secret from the credential store,
// falling back to the config map and then to an environment variable.
func resolveProviderSecret(instanceName, cfgKey, envVar string, providerCfg config.ProviderConfig) string {
	// 1. Credential store
	if store := getAPICredentialStore(); store != nil {
		storeKey := "provider." + instanceName + "." + cfgKey
		if val := store.GetSecret(storeKey); val != "" {
			return val
		}
	}
	// 2. Config map
	if val, ok := providerCfg[cfgKey]; ok && val != "" {
		return val
	}
	// 3. Environment variable
	if envVar != "" {
		return os.Getenv(envVar)
	}
	return ""
}

// isProviderConfigured checks if a provider has any secret configured
// (either in the credential store or in the config map).
func isProviderConfigured(instanceName string, providerCfg config.ProviderConfig) bool {
	// Check credential store for any secrets for this provider
	if store := getAPICredentialStore(); store != nil {
		provType := config.GetProviderType(instanceName, providerCfg)
		if provType == "" {
			provType = instanceName
		}
		// Check common secret keys for this provider type
		secretKeys := providerSecretKeys(provType)
		for _, key := range secretKeys {
			storeKey := "provider." + instanceName + "." + key
			if store.GetSecret(storeKey) != "" {
				return true
			}
		}
	}

	// Check config map (non-secret fields like base_url also count as "configured")
	for key, val := range providerCfg {
		if key != "type" && val != "" {
			return true
		}
	}
	return false
}

// providerSecretKeys returns the secret field names for a given provider type.
func providerSecretKeys(provType string) []string {
	switch provType {
	case "sap_ai_core":
		return []string{"client_id", "client_secret", "auth_url"}
	default:
		return []string{"api_key"}
	}
}

// GeneralSettings represents the general app settings
type GeneralSettings struct {
	DefaultProvider            string `json:"default_provider"`
	DefaultProviderDisplayName string `json:"default_provider_display_name"`
	DefaultModel               string `json:"default_model"`
	WebSearchTool              string `json:"web_search_tool"`
	WebExtractTool             string `json:"web_extract_tool"`
	ContextLength              int    `json:"context_length,omitempty"`
}

// ProviderSettings represents a provider's configuration (masked)
type ProviderSettings struct {
	Name        string            `json:"name"`
	Type        string            `json:"type"`
	DisplayName string            `json:"display_name"`
	Configured  bool              `json:"configured"`
	Fields      map[string]string `json:"fields"` // Masked values for display
}

// AppSettingsResponse is the response for GET /api/settings/config
type AppSettingsResponse struct {
	General   GeneralSettings    `json:"general"`
	Providers []ProviderSettings `json:"providers"`
}

// UpdateAppSettingsRequest is the request for PUT /api/settings/config
type UpdateAppSettingsRequest struct {
	General   *GeneralSettings             `json:"general,omitempty"`
	Providers map[string]map[string]string `json:"providers,omitempty"`
}

// GetSettingsHandler handles GET /api/settings/config
func GetSettingsHandler(w http.ResponseWriter, r *http.Request) {
	cfg, err := config.LoadAppConfig()
	if err != nil {
		http.Error(w, "Failed to load config: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Build response with all provider instances
	providers := []ProviderSettings{}

	// List all provider instances (instance names are keys in cfg.Providers)
	for instanceName, instanceConfig := range cfg.Providers {
		// Determine provider type and display name
		providerType := config.GetProviderType(instanceName, instanceConfig)
		displayName := provider.GetProviderDisplayName(providerType)
		if displayName == "" {
			displayName = instanceName
		}

		// Filter out "type" field from display (it's metadata, not a credential)
		fields := make(map[string]string)
		configured := false

		for key, val := range instanceConfig {
			if key != "type" && val != "" {
				fields[key] = val
				configured = true
			}
		}

		// Also check credential store for secret fields not in config
		configured = configured || isProviderConfigured(instanceName, instanceConfig)

		providers = append(providers, ProviderSettings{
			Name:        instanceName,
			Type:        providerType,
			DisplayName: displayName,
			Configured:  configured,
			Fields:      fields,
		})
	}

	response := AppSettingsResponse{
		General: GeneralSettings{
			DefaultProvider:            cfg.General.DefaultProvider,
			DefaultProviderDisplayName: provider.GetProviderDisplayName(cfg.General.DefaultProvider),
			DefaultModel:               cfg.General.DefaultModel,
			WebSearchTool:              cfg.General.WebSearchTool,
			WebExtractTool:             cfg.General.WebExtractTool,
			ContextLength:              cfg.General.ContextLength,
		},
		Providers: providers,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// UpdateSettingsHandler handles PUT /api/settings/config
func UpdateSettingsHandler(w http.ResponseWriter, r *http.Request) {
	var req UpdateAppSettingsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	cfg, err := config.LoadAppConfig()
	if err != nil {
		http.Error(w, "Failed to load config: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Update general settings if provided
	if req.General != nil {
		cfg.General.DefaultProvider = req.General.DefaultProvider
		cfg.General.DefaultModel = req.General.DefaultModel
		cfg.General.WebSearchTool = req.General.WebSearchTool
		cfg.General.WebExtractTool = req.General.WebExtractTool
		if req.General.ContextLength > 0 {
			cfg.General.ContextLength = req.General.ContextLength
		}
	}

	// Update provider settings if provided
	if req.Providers != nil {
		// Check if this is a full providers array replacement (DELETE + ADD workflow)
		if len(req.Providers) > 0 {
			firstKey := ""
			for k := range req.Providers {
				firstKey = k
				break
			}
			// If the first key is "__replace_all__", treat providers as full replacement array
			if firstKey == "__replace_all__" {
				var newProviders []map[string]string
				if err := json.Unmarshal([]byte(req.Providers["__replace_all__"]["__array__"]), &newProviders); err == nil {
					// Build new providers map from array
					newProvidersMap := make(map[string]config.ProviderConfig)
					for _, p := range newProviders {
						name := p["name"]
						if name != "" {
							newProvidersMap[name] = make(config.ProviderConfig)
							for k, v := range p {
								if k != "name" {
									newProvidersMap[name][k] = v
								}
							}
						}
					}
					cfg.Providers = newProvidersMap
				}
			} else {
				// Original behavior: update individual provider fields
				for providerName, providerFields := range req.Providers {
					if cfg.Providers == nil {
						cfg.Providers = make(map[string]config.ProviderConfig)
					}
					if cfg.Providers[providerName] == nil {
						cfg.Providers[providerName] = make(config.ProviderConfig)
					}
					for key, value := range providerFields {
						// Only update if value is not masked placeholder
						if value != "" && !isMaskedValue(value) {
							cfg.Providers[providerName][key] = value
						}
					}
				}
			}
		}

		// Extract secrets from provider configs and store in credential store.
		// Then scrub secrets from the config struct before saving to YAML.
		if store := getAPICredentialStore(); store != nil {
			secrets := make(map[string]string)
			for instanceName, pCfg := range cfg.Providers {
				provType := config.GetProviderType(instanceName, pCfg)
				if provType == "" {
					provType = instanceName
				}
				for _, key := range providerSecretKeys(provType) {
					if val, has := pCfg[key]; has && val != "" {
						storeKey := "provider." + instanceName + "." + key
						secrets[storeKey] = val
					}
				}
			}
			if len(secrets) > 0 {
				if err := store.SetSecretBatch(secrets); err != nil {
					slog.Warn("failed to save provider secrets to credential store", "error", err)
				} else {
					// Scrub secrets from config before saving to YAML
					credentials.ScrubAppConfig(cfg)
				}
			}
		}
	}

	if err := config.SaveAppConfig(cfg); err != nil {
		http.Error(w, "Failed to save config: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Re-setup environment variables (prefer credential store path)
	if store := getAPICredentialStore(); store != nil {
		config.SetupAllProviderEnvFromStore(cfg, store.GetSecret)
	} else {
		config.SetupAllProviderEnv(cfg)
	}

	// Regenerate the managed OpenCode config since provider/model may have changed.
	// Reload config from disk to get the clean version (secrets were scrubbed above).
	if freshCfg, loadErr := config.LoadAppConfig(); loadErr == nil {
		regenerateOpenCodeConfig(freshCfg)
	} else {
		slog.Warn("failed to reload config for OpenCode regeneration", "error", loadErr)
	}

	// Reset the Studio chat agent so the next request picks up fresh config.
	GetChatManager().Reset()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

// isMaskedValue checks if a value is a masked placeholder
func isMaskedValue(val string) bool {
	return len(val) >= 4 && val[:4] == "****"
}

// GetMCPSettingsHandler handles GET /api/settings/mcp
func GetMCPSettingsHandler(w http.ResponseWriter, r *http.Request) {
	cfg, err := config.LoadMCPConfigRaw()
	if err != nil {
		http.Error(w, "Failed to load MCP config: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(cfg)
}

// UpdateMCPSettingsHandler handles PUT /api/settings/mcp
func UpdateMCPSettingsHandler(w http.ResponseWriter, r *http.Request) {
	var newCfg config.MCPConfig
	if err := json.NewDecoder(r.Body).Decode(&newCfg); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// Load existing config to detect added/removed servers
	oldCfg, _ := config.LoadMCPConfig()

	// Set default transport to "stdio" if not specified
	for name, server := range newCfg.MCPServers {
		if server.Transport == "" {
			server.Transport = "stdio"
			newCfg.MCPServers[name] = server
		}
	}

	// Detect removed servers and remove from persistent cache
	if oldCfg != nil && oldCfg.MCPServers != nil {
		for serverName := range oldCfg.MCPServers {
			if _, exists := newCfg.MCPServers[serverName]; !exists {
				// Server was removed - clear its tools from persistent cache
				cache.RemoveServer(serverName)
				RemoveServerToolsFromCache(serverName) // Also update in-memory cache
				slog.Info("removed server from persistent cache", "component", "cache", "server", serverName)
			}
		}
	}

	// Detect added or changed servers
	addedOrChanged := make(map[string]config.MCPServerConfig)
	for serverName, serverCfg := range newCfg.MCPServers {
		if oldCfg == nil || oldCfg.MCPServers == nil {
			addedOrChanged[serverName] = serverCfg
			continue
		}
		oldServer, existed := oldCfg.MCPServers[serverName]
		if !existed {
			// New server
			addedOrChanged[serverName] = serverCfg
		} else {
			// Check if config changed using checksum
			oldChecksum := cache.ComputeServerChecksum(oldServer.Command, oldServer.Args, oldServer.Env)
			newChecksum := cache.ComputeServerChecksum(serverCfg.Command, serverCfg.Args, serverCfg.Env)
			if oldChecksum != newChecksum {
				addedOrChanged[serverName] = serverCfg
			}
		}
	}

	if err := config.SaveMCPConfig(&newCfg); err != nil {
		http.Error(w, "Failed to save MCP config: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Re-setup environment variables
	config.SetupMCPEnv(&newCfg)

	// Update persistent cache for added/changed servers
	if len(addedOrChanged) > 0 {
		slog.Info("detected added/changed servers", "component", "cache", "count", len(addedOrChanged), "servers", keysOf(addedOrChanged))

		// Set initial status to "loading" for all added/changed servers
		now := time.Now().UTC().Format(time.RFC3339)
		for serverName := range addedOrChanged {
			SetServerStatus(serverName, cache.ServerStatus{
				Name:      serverName,
				Status:    "loading",
				ToolCount: 0,
				LastCheck: now,
			})
		}

		// Use background context since request context gets cancelled
		go updatePersistentCacheForServers(context.Background(), addedOrChanged)
	}

	// Save persistent cache after removals
	if err := cache.SaveCache(); err != nil {
		slog.Warn("failed to save persistent cache", "component", "cache", "error", err)
	}

	// Refresh in-memory tools cache
	RefreshToolsCache(r.Context())

	// Reset the Studio chat agent so the next request picks up fresh MCP config.
	GetChatManager().Reset()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

// InstallInlineMCPServerRequest is the request for POST /api/mcp/install-inline
type InstallInlineMCPServerRequest struct {
	ServerName string                 `json:"serverName"`
	Config     config.MCPServerConfig `json:"config"`
}

// InstallInlineMCPServerHandler handles POST /api/mcp/install-inline
// Adds an inline MCP server configuration to the user's MCP config
func InstallInlineMCPServerHandler(w http.ResponseWriter, r *http.Request) {
	var req InstallInlineMCPServerRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body: "+err.Error(), http.StatusBadRequest)
		return
	}

	if req.ServerName == "" {
		http.Error(w, "Server name is required", http.StatusBadRequest)
		return
	}

	if req.Config.Command == "" && req.Config.URL == "" {
		http.Error(w, "Server config must have either command or URL", http.StatusBadRequest)
		return
	}

	// Load current MCP config
	mcpCfg, err := config.LoadMCPConfig()
	if err != nil {
		mcpCfg = &config.MCPConfig{MCPServers: make(map[string]config.MCPServerConfig)}
	}

	// Set default transport
	if req.Config.Transport == "" {
		req.Config.Transport = "stdio"
	}

	// Initialize map if nil
	if mcpCfg.MCPServers == nil {
		mcpCfg.MCPServers = make(map[string]config.MCPServerConfig)
	}

	// Add the server
	mcpCfg.MCPServers[req.ServerName] = req.Config

	// Save config
	if err := config.SaveMCPConfig(mcpCfg); err != nil {
		http.Error(w, "Failed to save MCP config: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Setup environment variables
	config.SetupMCPEnv(mcpCfg)

	// Synchronously load this server's tools (should be fast for one server)
	toolsLoaded := 0
	toolError := ""
	mcpManager, err := mcp.NewManager()
	if err != nil {
		toolError = fmt.Sprintf("Failed to create MCP manager: %v", err)
		slog.Warn(toolError)
	} else {
		namedToolset, err := mcpManager.InitializeSingleToolset(r.Context(), req.ServerName)
		if err != nil {
			toolError = fmt.Sprintf("Failed to initialize server: %v", err)
			slog.Warn(toolError)
		} else {
			// Get tools from this server and add to cache
			minimalCtx := &minimalReadonlyContext{Context: r.Context()}
			mcpTools, err := namedToolset.Toolset.Tools(minimalCtx)
			if err != nil {
				stderrOutput := mcp.GetStderr(namedToolset.Stderr)
				if stderrOutput != "" && stderrOutput != "no stderr output" {
					toolError = stderrOutput
				} else {
					toolError = fmt.Sprintf("Server started but failed to get tools: %v", err)
				}
				slog.Warn(toolError)
			} else {
				var newTools []ToolInfo
				for _, t := range mcpTools {
					newTools = append(newTools, ToolInfo{
						Name:        t.Name(),
						Description: t.Description(),
						Source:      req.ServerName,
					})
				}
				AddServerToolsToCache(req.ServerName, newTools)
				toolsLoaded = len(newTools)

				// Also update persistent cache (use mcpTools for schema access)
				persistentTools := make([]cache.ToolEntry, 0, len(mcpTools))
				for _, t := range mcpTools {
					persistentTools = append(persistentTools, cache.ToolEntry{
						Name:        t.Name(),
						Description: t.Description(),
						Source:      req.ServerName,
						InputSchema: common.ExtractToolInputSchema(t),
					})
				}
				checksum := cache.ComputeServerChecksum(req.Config.Command, req.Config.Args, req.Config.Env)
				cache.AddServerTools(req.ServerName, persistentTools, checksum)
				if err := cache.SaveCache(); err != nil {
					slog.Warn("failed to save persistent cache", "component", "cache", "error", err)
				} else {
					slog.Info("saved tools to persistent cache", "component", "cache", "count", len(persistentTools), "server", req.ServerName)
				}
			}
		}
	}

	slog.Info("installed inline mcp server", "component", "mcp-install", "server", req.ServerName)

	// Reset the Studio chat agent so the next request picks up the new MCP server.
	GetChatManager().Reset()

	w.Header().Set("Content-Type", "application/json")
	response := map[string]interface{}{
		"status":      "installed",
		"serverName":  req.ServerName,
		"toolsLoaded": toolsLoaded,
	}
	if toolError != "" {
		response["toolError"] = toolError
	}
	json.NewEncoder(w).Encode(response)
}

// updatePersistentCacheForServers initializes specific servers and adds their tools to persistent cache
func updatePersistentCacheForServers(ctx context.Context, servers map[string]config.MCPServerConfig) {
	mcpManager, err := mcp.NewManager()
	if err != nil {
		slog.Error("failed to create mcp manager", "component", "cache", "error", err)
		return
	}
	defer mcpManager.Cleanup()

	for serverName, serverCfg := range servers {
		slog.Info("updating cache for server", "component", "cache", "server", serverName)

		// Initialize just this server
		namedToolset, err := mcpManager.InitializeSingleToolset(ctx, serverName)
		if err != nil {
			slog.Error("failed to initialize server", "component", "cache", "server", serverName, "error", err)
			// Update status to error
			SetServerStatus(serverName, cache.ServerStatus{
				Name:      serverName,
				Status:    "error",
				Error:     err.Error(),
				ToolCount: 0,
				LastCheck: time.Now().UTC().Format(time.RFC3339),
			})
			continue
		}

		// Get its tools
		minimalCtx := &minimalReadonlyContext{Context: ctx}
		mcpTools, err := namedToolset.Toolset.Tools(minimalCtx)
		if err != nil {
			stderrOutput := mcp.GetStderr(namedToolset.Stderr)
			slog.Error("failed to get tools from server", "component", "cache", "server", serverName, "error", err, "stderr", stderrOutput)
			// Update status to error
			errMsg := fmt.Sprintf("Failed to list tools: %v", err)
			if stderrOutput != "" && stderrOutput != "no stderr output" {
				errMsg = stderrOutput
			}
			SetServerStatus(serverName, cache.ServerStatus{
				Name:      serverName,
				Status:    "error",
				Error:     errMsg,
				ToolCount: 0,
				LastCheck: time.Now().UTC().Format(time.RFC3339),
			})
			continue
		}

		// Build tool entries
		var toolEntries []cache.ToolEntry
		for _, t := range mcpTools {
			toolEntries = append(toolEntries, cache.ToolEntry{
				Name:        t.Name(),
				Description: t.Description(),
				Source:      serverName,
				InputSchema: common.ExtractToolInputSchema(t),
			})
		}

		// Compute checksum and add to cache
		checksum := cache.ComputeServerChecksum(serverCfg.Command, serverCfg.Args, serverCfg.Env)
		cache.AddServerTools(serverName, toolEntries, checksum)
		slog.Info("added tools to persistent cache", "component", "cache", "server", serverName, "count", len(toolEntries))

		// Update status to healthy
		SetServerStatus(serverName, cache.ServerStatus{
			Name:      serverName,
			Status:    "healthy",
			ToolCount: len(toolEntries),
			LastCheck: time.Now().UTC().Format(time.RFC3339),
		})
	}

	// Save the updated cache
	if err := cache.SaveCache(); err != nil {
		slog.Error("failed to save persistent cache", "component", "cache", "error", err)
	}
}

// keysOf returns the keys of a map for logging
func keysOf(m map[string]config.MCPServerConfig) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}

// ListProviderModelsHandler handles GET /api/providers/{providerId}/models
func ListProviderModelsHandler(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	providerID := vars["providerId"]

	cfg, err := config.LoadAppConfig()
	if err != nil {
		http.Error(w, "Failed to load config: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Resolve secrets from credential store into the provider config so that
	// ListModelsForProvider sees the actual API key (secrets are scrubbed from
	// the YAML config file and stored in the credential store).
	if instance, exists := cfg.Providers[providerID]; exists {
		providerType := config.GetProviderType(providerID, instance)
		if providerType == "" {
			providerType = providerID
		}
		for _, key := range providerSecretKeys(providerType) {
			resolved := resolveProviderSecret(providerID, key, "", instance)
			if resolved != "" {
				instance[key] = resolved
			}
		}
	}

	models, err := provider.ListModelsForProvider(r.Context(), providerID, cfg)
	if err != nil {
		http.Error(w, "Failed to fetch models: "+err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"provider": providerID,
		"models":   models,
	})
}

// SetupStatusResponse represents the setup status for the wizard
type SetupStatusResponse struct {
	SetupRequired       bool     `json:"setupRequired"`
	HasDefaultProvider  bool     `json:"hasDefaultProvider"`
	HasDefaultModel     bool     `json:"hasDefaultModel"`
	ConfiguredProviders []string `json:"configuredProviders"`
}

// GetSetupStatusHandler handles GET /api/settings/status
func GetSetupStatusHandler(w http.ResponseWriter, r *http.Request) {
	cfg, err := config.LoadAppConfig()
	if err != nil {
		// If config doesn't exist, setup is definitely required
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(SetupStatusResponse{
			SetupRequired:       true,
			HasDefaultProvider:  false,
			HasDefaultModel:     false,
			ConfiguredProviders: []string{},
		})
		return
	}

	// Check for configured providers
	// For multi-instance support, check all provider instances in config
	var configuredProviders []string

	for instanceName, providerCfg := range cfg.Providers {
		if providerCfg == nil {
			continue
		}

		// Check if this provider has a valid type
		providerType := config.GetProviderType(instanceName, providerCfg)
		if providerType == "" {
			continue
		}

		// Check if provider is configured (config map + credential store)
		if isProviderConfigured(instanceName, providerCfg) {
			configuredProviders = append(configuredProviders, instanceName)
		}
	}

	hasDefaultProvider := cfg.General.DefaultProvider != ""
	hasDefaultModel := cfg.General.DefaultModel != ""

	// Setup is required if no default provider OR no configured providers
	setupRequired := !hasDefaultProvider || len(configuredProviders) == 0

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(SetupStatusResponse{
		SetupRequired:       setupRequired,
		HasDefaultProvider:  hasDefaultProvider,
		HasDefaultModel:     hasDefaultModel,
		ConfiguredProviders: configuredProviders,
	})
}

// ListProviderModelsWithMetadataHandler handles GET /api/providers/{providerId}/models-metadata
// Returns enhanced model information with pricing for OpenRouter, basic info for others
func ListProviderModelsWithMetadataHandler(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	providerID := vars["providerId"]

	cfg, err := config.LoadAppConfig()
	if err != nil {
		http.Error(w, "Failed to load config: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Resolve provider instance config and type. The providerID is the instance
	// name (e.g. "SAP AI Core"), not necessarily the type slug ("sap_ai_core").
	pCfg := cfg.Providers[providerID]
	if pCfg == nil {
		pCfg = make(config.ProviderConfig)
	}
	providerType := config.GetProviderType(providerID, pCfg)

	switch providerType {
	case "openrouter":
		apiKey := resolveProviderSecret(providerID, "api_key", "OPENROUTER_API_KEY", pCfg)
		models, err := openrouter.ListModelsWithMetadata(r.Context(), apiKey)
		if err != nil {
			http.Error(w, "Failed to fetch models: "+err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{"provider": providerID, "has_metadata": true, "models": models})
		return

	case "anthropic":
		apiKey := resolveProviderSecret(providerID, "api_key", "ANTHROPIC_API_KEY", pCfg)
		models, err := anthropic.ListModelsWithMetadata(r.Context(), apiKey)
		if err != nil {
			http.Error(w, "Failed to fetch models: "+err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{"provider": providerID, "has_metadata": true, "models": models})
		return

	case "gemini":
		apiKey := resolveProviderSecret(providerID, "api_key", "GOOGLE_API_KEY", pCfg)
		models, err := google.ListModelsWithMetadata(r.Context(), apiKey)
		if err != nil {
			http.Error(w, "Failed to fetch models: "+err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{"provider": providerID, "has_metadata": true, "models": models})
		return

	case "groq":
		apiKey := resolveProviderSecret(providerID, "api_key", "GROQ_API_KEY", pCfg)
		models, err := groq.ListModelsWithMetadata(r.Context(), apiKey)
		if err != nil {
			http.Error(w, "Failed to fetch models: "+err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{"provider": providerID, "has_metadata": true, "models": models})
		return

	case "openai":
		apiKey := resolveProviderSecret(providerID, "api_key", "OPENAI_API_KEY", pCfg)
		models, err := openai_provider.ListModelsWithMetadata(r.Context(), apiKey)
		if err != nil {
			http.Error(w, "Failed to fetch models: "+err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{"provider": providerID, "has_metadata": true, "models": models})
		return

	case "poe":
		apiKey := resolveProviderSecret(providerID, "api_key", "POE_API_KEY", pCfg)
		models, err := poe.ListModelsWithMetadata(r.Context(), apiKey)
		if err != nil {
			http.Error(w, "Failed to fetch models: "+err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{"provider": providerID, "has_metadata": true, "models": models})
		return

	case "sap_ai_core":
		clientID := resolveProviderSecret(providerID, "client_id", "AICORE_CLIENT_ID", pCfg)
		clientSecret := resolveProviderSecret(providerID, "client_secret", "AICORE_CLIENT_SECRET", pCfg)
		authURL := resolveProviderSecret(providerID, "auth_url", "AICORE_AUTH_URL", pCfg)
		baseURL := pCfg["base_url"]
		if baseURL == "" {
			baseURL = os.Getenv("AICORE_BASE_URL")
		}
		resourceGroup := pCfg["resource_group"]
		if resourceGroup == "" {
			resourceGroup = os.Getenv("AICORE_RESOURCE_GROUP")
		}
		models, err := sap.ListModelsWithMetadata(r.Context(), clientID, clientSecret, authURL, baseURL, resourceGroup)
		if err != nil {
			http.Error(w, "Failed to fetch models: "+err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{"provider": providerID, "has_metadata": true, "models": models})
		return

	case "xai":
		apiKey := resolveProviderSecret(providerID, "api_key", "XAI_API_KEY", pCfg)
		models, err := xai.ListModelsWithMetadata(r.Context(), apiKey)
		if err != nil {
			http.Error(w, "Failed to fetch models: "+err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{"provider": providerID, "has_metadata": true, "models": models})
		return

	case "lm_studio":
		baseURL := pCfg["base_url"]
		models, err := lmstudio.ListModelsWithMetadata(r.Context(), baseURL)
		if err != nil {
			http.Error(w, "Failed to fetch models: "+err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{"provider": providerID, "has_metadata": true, "models": models})
		return

	case "ollama":
		baseURL := pCfg["base_url"]
		models, err := ollama.ListModelsWithMetadata(r.Context(), baseURL)
		if err != nil {
			http.Error(w, "Failed to fetch models: "+err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{"provider": providerID, "has_metadata": true, "models": models})
		return

	case "openai_compat":
		apiKey := resolveProviderSecret(providerID, "api_key", "", pCfg)
		baseURL := pCfg["base_url"]
		if baseURL == "" {
			baseURL = "https://api.openai.com/v1"
		}
		models, err := openai_compat.ListModels(r.Context(), apiKey, baseURL)
		if err != nil {
			http.Error(w, "Failed to fetch models: "+err.Error(), http.StatusInternalServerError)
			return
		}
		var modelInfos []map[string]interface{}
		for _, m := range models {
			modelInfos = append(modelInfos, map[string]interface{}{"id": m, "name": m})
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{"provider": providerID, "has_metadata": false, "models": modelInfos})
		return
	}

	// For other providers, return basic model list wrapped as ModelInfo
	models, err := provider.ListModelsForProvider(r.Context(), providerID, cfg)
	if err != nil {
		http.Error(w, "Failed to fetch models: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Convert string list to basic ModelInfo format
	var modelInfos []map[string]interface{}
	for _, m := range models {
		modelInfos = append(modelInfos, map[string]interface{}{
			"id":   m,
			"name": m,
		})
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"provider":     providerID,
		"has_metadata": false,
		"models":       modelInfos,
	})
}

// VersionResponse represents the version API response
type VersionResponse struct {
	Version string `json:"version"`
}

// GetVersionHandler handles GET /api/version
func GetVersionHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	v := version.GetVersion()
	json.NewEncoder(w).Encode(VersionResponse{
		Version: v,
	})
}
