package api

import (
	"encoding/json"
	"net/http"

	"github.com/gorilla/mux"
	"github.com/schardosin/astonish/pkg/config"
	"github.com/schardosin/astonish/pkg/provider"
)

// GeneralSettings represents the general app settings
type GeneralSettings struct {
	DefaultProvider            string `json:"default_provider"`
	DefaultProviderDisplayName string `json:"default_provider_display_name"`
	DefaultModel               string `json:"default_model"`
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
	General   *GeneralSettings   `json:"general,omitempty"`
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
	knownProviders := []string{"anthropic", "gemini", "groq", "lm_studio", "ollama", "openai", "openrouter", "sap_ai_core", "xai"}
	
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
	var cfg config.MCPConfig
	if err := json.NewDecoder(r.Body).Decode(&cfg); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if err := config.SaveMCPConfig(&cfg); err != nil {
		http.Error(w, "Failed to save MCP config: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Re-setup environment variables
	config.SetupMCPEnv(&cfg)

	// Refresh the tools cache to pick up new MCP servers
	RefreshToolsCache(r.Context())

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
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
	knownProviders := []string{"anthropic", "gemini", "groq", "lm_studio", "ollama", "openai", "openrouter", "sap_ai_core", "xai"}

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
