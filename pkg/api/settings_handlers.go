package api

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"

	"github.com/gorilla/mux"
	"github.com/schardosin/astonish/pkg/cache"
	"github.com/schardosin/astonish/pkg/config"
	"github.com/schardosin/astonish/pkg/mcp"
	"github.com/schardosin/astonish/pkg/provider"
	"github.com/schardosin/astonish/pkg/provider/anthropic"
	"github.com/schardosin/astonish/pkg/provider/google"
	"github.com/schardosin/astonish/pkg/provider/groq"
	"github.com/schardosin/astonish/pkg/provider/lmstudio"
	"github.com/schardosin/astonish/pkg/provider/ollama"
	openai_provider "github.com/schardosin/astonish/pkg/provider/openai"
	"github.com/schardosin/astonish/pkg/provider/openrouter"
	"github.com/schardosin/astonish/pkg/provider/poe"
	"github.com/schardosin/astonish/pkg/provider/sap"
	"github.com/schardosin/astonish/pkg/provider/xai"
)

// GeneralSettings represents the general app settings
type GeneralSettings struct {
	DefaultProvider            string `json:"default_provider"`
	DefaultProviderDisplayName string `json:"default_provider_display_name"`
	DefaultModel               string `json:"default_model"`
	WebSearchTool              string `json:"web_search_tool"`
	WebExtractTool             string `json:"web_extract_tool"`
}

// ProviderSettings represents a provider's configuration (masked)
type ProviderSettings struct {
	Name        string            `json:"name"`
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

	// Build response with masked provider values
	providers := []ProviderSettings{}

	// List all known providers (alphabetically ordered)
	knownProviders := []string{"anthropic", "gemini", "groq", "lm_studio", "ollama", "openai", "openrouter", "poe", "sap_ai_core", "xai"}

	for _, name := range knownProviders {
		providerCfg, exists := cfg.Providers[name]
		fields := make(map[string]string)
		configured := false

		// Get expected fields for this provider
		if mapping, ok := config.ProviderEnvMapping[name]; ok {
			for cfgKey := range mapping {
				if exists && providerCfg[cfgKey] != "" {
					// Mask the value (show last 4 chars)
					val := providerCfg[cfgKey]
					if len(val) > 4 {
						fields[cfgKey] = "****" + val[len(val)-4:]
					} else {
						fields[cfgKey] = "****"
					}
					configured = true
				} else {
					fields[cfgKey] = ""
				}
			}
		}

		providers = append(providers, ProviderSettings{
			Name:        name,
			DisplayName: provider.GetProviderDisplayName(name),
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
	}

	// Update provider settings if provided
	if req.Providers != nil {
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

	if err := config.SaveAppConfig(cfg); err != nil {
		http.Error(w, "Failed to save config: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Re-setup environment variables
	config.SetupAllProviderEnv(cfg)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

// isMaskedValue checks if a value is a masked placeholder
func isMaskedValue(val string) bool {
	return len(val) >= 4 && val[:4] == "****"
}

// GetMCPSettingsHandler handles GET /api/settings/mcp
func GetMCPSettingsHandler(w http.ResponseWriter, r *http.Request) {
	cfg, err := config.LoadMCPConfig()
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
				log.Printf("[Cache] Removed server '%s' from persistent cache", serverName)
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
		log.Printf("[Cache] Detected %d added/changed servers: %v", len(addedOrChanged), keysOf(addedOrChanged))
		// Use background context since request context gets cancelled
		go updatePersistentCacheForServers(context.Background(), addedOrChanged)
	}

	// Save persistent cache after removals
	if err := cache.SaveCache(); err != nil {
		log.Printf("[Cache] Warning: Failed to save persistent cache: %v", err)
	}

	// Refresh in-memory tools cache
	RefreshToolsCache(r.Context())

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

// InstallInlineMCPServerRequest is the request for POST /api/mcp/install-inline
type InstallInlineMCPServerRequest struct {
	ServerName string                  `json:"serverName"`
	Config     config.MCPServerConfig  `json:"config"`
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
		log.Printf("Warning: %s", toolError)
	} else {
		namedToolset, err := mcpManager.InitializeSingleToolset(r.Context(), req.ServerName)
		if err != nil {
			toolError = fmt.Sprintf("Failed to initialize server: %v", err)
			log.Printf("Warning: %s", toolError)
		} else {
			// Get tools from this server and add to cache
			minimalCtx := &minimalReadonlyContext{Context: r.Context()}
			mcpTools, err := namedToolset.Toolset.Tools(minimalCtx)
			if err != nil {
				toolError = fmt.Sprintf("Server started but failed to get tools: %v", err)
				log.Printf("Warning: %s", toolError)
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

				// Also update persistent cache
				persistentTools := make([]cache.ToolEntry, 0, len(newTools))
				for _, t := range newTools {
					persistentTools = append(persistentTools, cache.ToolEntry{
						Name:        t.Name,
						Description: t.Description,
						Source:      t.Source,
					})
				}
				checksum := cache.ComputeServerChecksum(req.Config.Command, req.Config.Args, req.Config.Env)
				cache.AddServerTools(req.ServerName, persistentTools, checksum)
				if err := cache.SaveCache(); err != nil {
					log.Printf("[Cache] Warning: Failed to save persistent cache: %v", err)
				} else {
					log.Printf("[Cache] Saved %d tools for server '%s' to persistent cache", len(persistentTools), req.ServerName)
				}
			}
		}
	}

	log.Printf("[MCP Install] Installed inline server: %s", req.ServerName)

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
		log.Printf("[Cache] Failed to create MCP manager: %v", err)
		return
	}
	defer mcpManager.Cleanup()

	for serverName, serverCfg := range servers {
		log.Printf("[Cache] Updating cache for server: %s", serverName)
		
		// Initialize just this server
		namedToolset, err := mcpManager.InitializeSingleToolset(ctx, serverName)
		if err != nil {
			log.Printf("[Cache] Failed to initialize server '%s': %v", serverName, err)
			continue
		}

		// Get its tools
		minimalCtx := &minimalReadonlyContext{Context: ctx}
		mcpTools, err := namedToolset.Toolset.Tools(minimalCtx)
		if err != nil {
			log.Printf("[Cache] Failed to get tools from '%s': %v", serverName, err)
			continue
		}

		// Build tool entries
		var toolEntries []cache.ToolEntry
		for _, t := range mcpTools {
			toolEntries = append(toolEntries, cache.ToolEntry{
				Name:        t.Name(),
				Description: t.Description(),
				Source:      serverName,
			})
		}

		// Compute checksum and add to cache
		checksum := cache.ComputeServerChecksum(serverCfg.Command, serverCfg.Args, serverCfg.Env)
		cache.AddServerTools(serverName, toolEntries, checksum)
		log.Printf("[Cache] Added %d tools from server '%s' to persistent cache", len(toolEntries), serverName)
	}

	// Save the updated cache
	if err := cache.SaveCache(); err != nil {
		log.Printf("[Cache] Failed to save persistent cache: %v", err)
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
	var configuredProviders []string
	knownProviders := []string{"anthropic", "gemini", "groq", "lm_studio", "ollama", "openai", "openrouter", "poe", "sap_ai_core", "xai"}

	for _, name := range knownProviders {
		if providerCfg, exists := cfg.Providers[name]; exists {
			// Check if at least one field has a value
			for _, val := range providerCfg {
				if val != "" {
					configuredProviders = append(configuredProviders, name)
					break
				}
			}
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

	// For OpenRouter, return full metadata with pricing
	if providerID == "openrouter" {
		apiKey := ""
		if cfg.Providers["openrouter"] != nil {
			apiKey = cfg.Providers["openrouter"]["api_key"]
		}

		models, err := openrouter.ListModelsWithMetadata(r.Context(), apiKey)
		if err != nil {
			http.Error(w, "Failed to fetch models: "+err.Error(), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"provider":     providerID,
			"has_metadata": true,
			"models":       models,
		})
		return
	}

	// For Anthropic, return model metadata with display names
	if providerID == "anthropic" {
		apiKey := ""
		if cfg.Providers["anthropic"] != nil {
			apiKey = cfg.Providers["anthropic"]["api_key"]
		}
		if apiKey == "" {
			apiKey = os.Getenv("ANTHROPIC_API_KEY")
		}

		models, err := anthropic.ListModelsWithMetadata(r.Context(), apiKey)
		if err != nil {
			http.Error(w, "Failed to fetch models: "+err.Error(), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"provider":     providerID,
			"has_metadata": true,
			"models":       models,
		})
		return
	}

	// For Google AI (Gemini), return model metadata with token limits
	if providerID == "gemini" {
		apiKey := ""
		if cfg.Providers["gemini"] != nil {
			apiKey = cfg.Providers["gemini"]["api_key"]
		}
		if apiKey == "" {
			apiKey = os.Getenv("GOOGLE_API_KEY")
		}

		models, err := google.ListModelsWithMetadata(r.Context(), apiKey)
		if err != nil {
			http.Error(w, "Failed to fetch models: "+err.Error(), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"provider":     providerID,
			"has_metadata": true,
			"models":       models,
		})
		return
	}

	// For Groq, return model metadata with context window
	if providerID == "groq" {
		apiKey := ""
		if cfg.Providers["groq"] != nil {
			apiKey = cfg.Providers["groq"]["api_key"]
		}
		if apiKey == "" {
			apiKey = os.Getenv("GROQ_API_KEY")
		}

		models, err := groq.ListModelsWithMetadata(r.Context(), apiKey)
		if err != nil {
			http.Error(w, "Failed to fetch models: "+err.Error(), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"provider":     providerID,
			"has_metadata": true,
			"models":       models,
		})
		return
	}

	// For OpenAI, return model metadata
	if providerID == "openai" {
		apiKey := ""
		if cfg.Providers["openai"] != nil {
			apiKey = cfg.Providers["openai"]["api_key"]
		}
		if apiKey == "" {
			apiKey = os.Getenv("OPENAI_API_KEY")
		}

		models, err := openai_provider.ListModelsWithMetadata(r.Context(), apiKey)
		if err != nil {
			http.Error(w, "Failed to fetch models: "+err.Error(), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"provider":     providerID,
			"has_metadata": true,
			"models":       models,
		})
		return
	}

	// For Poe, return model metadata
	if providerID == "poe" {
		apiKey := ""
		if cfg.Providers["poe"] != nil {
			apiKey = cfg.Providers["poe"]["api_key"]
		}
		if apiKey == "" {
			apiKey = os.Getenv("POE_API_KEY")
		}

		models, err := poe.ListModelsWithMetadata(r.Context(), apiKey)
		if err != nil {
			http.Error(w, "Failed to fetch models: "+err.Error(), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"provider":     providerID,
			"has_metadata": true,
			"models":       models,
		})
		return
	}

	// For SAP AI Core, return model metadata with context window and max tokens
	if providerID == "sap_ai_core" {
		clientID := ""
		clientSecret := ""
		authURL := ""
		baseURL := ""
		resourceGroup := ""
		if cfg.Providers["sap_ai_core"] != nil {
			clientID = cfg.Providers["sap_ai_core"]["client_id"]
			clientSecret = cfg.Providers["sap_ai_core"]["client_secret"]
			authURL = cfg.Providers["sap_ai_core"]["auth_url"]
			baseURL = cfg.Providers["sap_ai_core"]["base_url"]
			resourceGroup = cfg.Providers["sap_ai_core"]["resource_group"]
		}
		if clientID == "" {
			clientID = os.Getenv("AICORE_CLIENT_ID")
		}
		if clientSecret == "" {
			clientSecret = os.Getenv("AICORE_CLIENT_SECRET")
		}
		if authURL == "" {
			authURL = os.Getenv("AICORE_AUTH_URL")
		}
		if baseURL == "" {
			baseURL = os.Getenv("AICORE_BASE_URL")
		}
		if resourceGroup == "" {
			resourceGroup = os.Getenv("AICORE_RESOURCE_GROUP")
		}

		models, err := sap.ListModelsWithMetadata(r.Context(), clientID, clientSecret, authURL, baseURL, resourceGroup)
		if err != nil {
			http.Error(w, "Failed to fetch models: "+err.Error(), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"provider":     providerID,
			"has_metadata": true,
			"models":       models,
		})
		return
	}

	// For xAI, return model metadata
	if providerID == "xai" {
		apiKey := ""
		if cfg.Providers["xai"] != nil {
			apiKey = cfg.Providers["xai"]["api_key"]
		}
		if apiKey == "" {
			apiKey = os.Getenv("XAI_API_KEY")
		}

		models, err := xai.ListModelsWithMetadata(r.Context(), apiKey)
		if err != nil {
			http.Error(w, "Failed to fetch models: "+err.Error(), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"provider":     providerID,
			"has_metadata": true,
			"models":       models,
		})
		return
	}

	// For LM Studio, return model metadata
	if providerID == "lm_studio" {
		baseURL := ""
		if cfg.Providers["lm_studio"] != nil {
			baseURL = cfg.Providers["lm_studio"]["base_url"]
		}

		models, err := lmstudio.ListModelsWithMetadata(r.Context(), baseURL)
		if err != nil {
			http.Error(w, "Failed to fetch models: "+err.Error(), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"provider":     providerID,
			"has_metadata": true,
			"models":       models,
		})
		return

	}

	// For Ollama, return model metadata
	if providerID == "ollama" {
		baseURL := ""
		if cfg.Providers["ollama"] != nil {
			baseURL = cfg.Providers["ollama"]["base_url"]
		}

		models, err := ollama.ListModelsWithMetadata(r.Context(), baseURL)
		if err != nil {
			http.Error(w, "Failed to fetch models: "+err.Error(), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"provider":     providerID,
			"has_metadata": true,
			"models":       models,
		})
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
