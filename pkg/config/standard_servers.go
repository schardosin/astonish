package config

// StandardEnvVar describes an environment variable required by a standard MCP server.
type StandardEnvVar struct {
	Name        string `json:"name"`
	Required    bool   `json:"required"`
	Description string `json:"description"`
}

// StandardMCPServer describes a pre-configured MCP server that Astonish
// knows how to install with minimal user input (just an API key).
// The server command, args, and env var names are hardcoded here.
// Only the API key value is stored in config.yaml under web_servers.
type StandardMCPServer struct {
	ID             string           `json:"id"`
	DisplayName    string           `json:"displayName"`
	Description    string           `json:"description"`
	Command        string           `json:"command"`
	Args           []string         `json:"args"`
	EnvVars        []StandardEnvVar `json:"envVars"`
	WebSearchTool  string           `json:"webSearchTool,omitempty"`  // "serverName:toolName"
	WebExtractTool string           `json:"webExtractTool,omitempty"` // "serverName:toolName" or empty
	IsDefault      bool             `json:"isDefault"`
}

// standardServers is the hardcoded registry of supported web MCP servers.
var standardServers = []StandardMCPServer{
	{
		ID:          "tavily",
		DisplayName: "Tavily",
		Description: "Web search and content extraction via Tavily API. Recommended for most users.",
		Command:     "npx",
		Args:        []string{"-y", "tavily-mcp@latest"},
		EnvVars: []StandardEnvVar{
			{Name: "TAVILY_API_KEY", Required: true, Description: "Your Tavily API key (get one at tavily.com)"},
		},
		WebSearchTool:  "tavily:tavily-search",
		WebExtractTool: "tavily:tavily-extract",
		IsDefault:      true,
	},
	{
		ID:          "brave-search",
		DisplayName: "Brave Search",
		Description: "Web search powered by Brave Search API. Search only, no content extraction.",
		Command:     "npx",
		Args:        []string{"-y", "@brave/brave-search-mcp-server", "--transport", "stdio"},
		EnvVars: []StandardEnvVar{
			{Name: "BRAVE_API_KEY", Required: true, Description: "Your Brave Search API key (get one at brave.com/search/api)"},
		},
		WebSearchTool:  "brave-search:brave_web_search",
		WebExtractTool: "",
		IsDefault:      false,
	},
	{
		ID:          "firecrawl",
		DisplayName: "Firecrawl",
		Description: "Web search, scraping, and content extraction via Firecrawl API.",
		Command:     "npx",
		Args:        []string{"-y", "firecrawl-mcp"},
		EnvVars: []StandardEnvVar{
			{Name: "FIRECRAWL_API_KEY", Required: true, Description: "Your Firecrawl API key (get one at firecrawl.dev)"},
		},
		WebSearchTool:  "firecrawl:firecrawl_search",
		WebExtractTool: "firecrawl:firecrawl_scrape",
		IsDefault:      false,
	},
}

// GetStandardServers returns all pre-configured standard MCP servers.
func GetStandardServers() []StandardMCPServer {
	result := make([]StandardMCPServer, len(standardServers))
	copy(result, standardServers)
	return result
}

// GetStandardServer returns a standard server by ID, or nil if not found.
func GetStandardServer(id string) *StandardMCPServer {
	for _, s := range standardServers {
		if s.ID == id {
			srv := s
			return &srv
		}
	}
	return nil
}

// GetStandardServerIDs returns the set of all standard server IDs for quick lookup.
func GetStandardServerIDs() map[string]bool {
	ids := make(map[string]bool, len(standardServers))
	for _, s := range standardServers {
		ids[s.ID] = true
	}
	return ids
}

// IsStandardServerInstalled checks if a standard server has credentials stored in config.yaml.
func IsStandardServerInstalled(id string) bool {
	appCfg, err := LoadAppConfig()
	if err != nil {
		return false
	}
	ws, exists := appCfg.WebServers[id]
	return exists && ws.APIKey != ""
}

// InstallStandardServer stores a standard server's API key in config.yaml
// and configures it as the active web search/extract tool.
// The server definition (command, args, env var names) comes from the hardcoded registry.
// At MCP load time, LoadMCPConfig() merges these into the MCP server list automatically.
func InstallStandardServer(id string, envValues map[string]string) error {
	srv := GetStandardServer(id)
	if srv == nil {
		return &StandardServerError{ID: id, Message: "unknown standard server"}
	}

	// Extract the API key from the provided env values
	var apiKey string
	for _, ev := range srv.EnvVars {
		if val, ok := envValues[ev.Name]; ok && val != "" {
			apiKey = val
			break
		}
		if ev.Required {
			return &StandardServerError{ID: id, Message: "missing required environment variable: " + ev.Name}
		}
	}

	// Load and update app config
	appCfg, err := LoadAppConfig()
	if err != nil {
		appCfg = &AppConfig{
			Providers:  make(map[string]ProviderConfig),
			WebServers: make(map[string]WebServerConfig),
		}
	}
	if appCfg.WebServers == nil {
		appCfg.WebServers = make(map[string]WebServerConfig)
	}

	appCfg.WebServers[id] = WebServerConfig{APIKey: apiKey}

	if srv.WebSearchTool != "" {
		appCfg.General.WebSearchTool = srv.WebSearchTool
	}
	if srv.WebExtractTool != "" {
		appCfg.General.WebExtractTool = srv.WebExtractTool
	}

	return SaveAppConfig(appCfg)
}

// BuildMCPServerConfig constructs an MCPServerConfig for a standard server
// using its hardcoded definition and the stored API key.
func BuildMCPServerConfig(srv *StandardMCPServer, apiKey string) MCPServerConfig {
	env := make(map[string]string)
	for _, ev := range srv.EnvVars {
		env[ev.Name] = apiKey
	}
	return MCPServerConfig{
		Command:   srv.Command,
		Args:      srv.Args,
		Env:       env,
		Transport: "stdio",
	}
}

// StandardServerError represents an error related to standard server operations.
type StandardServerError struct {
	ID      string
	Message string
}

func (e *StandardServerError) Error() string {
	return "standard server " + e.ID + ": " + e.Message
}
