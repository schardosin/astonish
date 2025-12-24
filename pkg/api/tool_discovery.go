package api

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"regexp"
	"strings"

	"github.com/schardosin/astonish/pkg/config"
	"github.com/schardosin/astonish/pkg/mcp"
	"github.com/schardosin/astonish/pkg/mcpstore"
	"github.com/schardosin/astonish/pkg/provider"
	"google.golang.org/adk/agent"
	"google.golang.org/adk/memory"
	"google.golang.org/adk/model"
	"google.golang.org/adk/session"
	"google.golang.org/adk/tool"
	"google.golang.org/genai"
)

// ToolSearchRequest is the request for POST /api/ai/tool-search
type ToolSearchRequest struct {
	Requirement string `json:"requirement"` // What the user needs (e.g., "take screenshots of websites")
}

// ToolSearchResult represents a matching tool from the store
type ToolSearchResult struct {
	ID             string            `json:"id"`
	Name           string            `json:"name"`
	Description    string            `json:"description"`
	Source         string            `json:"source"`
	Tags           []string          `json:"tags"`
	Installable    bool              `json:"installable"`
	Reason         string            `json:"reason,omitempty"`       // Why this tool matches the requirement
	RequiresApiKey bool              `json:"requiresApiKey"`         // Whether tool requires API key
	EnvVars        map[string]string `json:"envVars,omitempty"`      // Required env vars (key -> placeholder value)
}

// ToolSearchResponse is the response for POST /api/ai/tool-search
type ToolSearchResponse struct {
	Results []ToolSearchResult `json:"results"`
	Total   int                `json:"total"`
}

// minimalToolContext implements tool.Context for calling MCP tools
type minimalToolContext struct {
	context.Context
}

func (m *minimalToolContext) Actions() *session.EventActions       { return &session.EventActions{} }
func (m *minimalToolContext) Branch() string                       { return "" }
func (m *minimalToolContext) AgentName() string                    { return "tool-discovery" }
func (m *minimalToolContext) AppName() string                      { return "astonish" }
func (m *minimalToolContext) Artifacts() agent.Artifacts           { return nil }
func (m *minimalToolContext) FunctionCallID() string               { return "" }
func (m *minimalToolContext) InvocationID() string                 { return "" }
func (m *minimalToolContext) SessionID() string                    { return "" }
func (m *minimalToolContext) UserID() string                       { return "" }
func (m *minimalToolContext) UserContent() *genai.Content          { return nil }
func (m *minimalToolContext) ReadonlyState() session.ReadonlyState { return nil }
func (m *minimalToolContext) State() session.State                 { return nil }
func (m *minimalToolContext) SearchMemory(ctx context.Context, query string) (*memory.SearchResponse, error) {
	return nil, nil
}

// AIToolSearchHandler handles POST /api/ai/tool-search
// Uses AI to semantically evaluate which store tools can fulfill the requirement
func AIToolSearchHandler(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	
	var req ToolSearchRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// If no requirement, return empty
	if req.Requirement == "" {
		json.NewEncoder(w).Encode(ToolSearchResponse{Results: []ToolSearchResult{}, Total: 0})
		return
	}

	// Load all servers from taps (only installable ones)
	servers, err := loadAllServersFromTaps()
	if err != nil {
		http.Error(w, "Failed to load servers: "+err.Error(), http.StatusInternalServerError)
		return
	}

	// Filter to only installable servers and build a summary
	var toolSummaries []string
	var installableServers []mcpstore.Server
	for _, srv := range servers {
		if srv.Config != nil {
			installableServers = append(installableServers, srv)
			tags := ""
			if len(srv.Tags) > 0 {
				tags = " [tags: " + strings.Join(srv.Tags, ", ") + "]"
			}
			toolSummaries = append(toolSummaries, fmt.Sprintf("- %s: %s%s", srv.Name, srv.Description, tags))
		}
	}

	// If no tools available, return empty
	if len(installableServers) == 0 {
		json.NewEncoder(w).Encode(ToolSearchResponse{Results: []ToolSearchResult{}, Total: 0})
		return
	}

	// Use AI to find matching tools
	matchingTools := findToolsWithAI(ctx, req.Requirement, toolSummaries, installableServers)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(ToolSearchResponse{
		Results: matchingTools,
		Total:   len(matchingTools),
	})
}

// findToolsWithAI uses the LLM to semantically match tools to requirements
func findToolsWithAI(ctx context.Context, requirement string, toolSummaries []string, servers []mcpstore.Server) []ToolSearchResult {
	// Load config for LLM access
	appCfg, err := config.LoadAppConfig()
	if err != nil {
		return nil
	}

	providerName := appCfg.General.DefaultProvider
	modelName := appCfg.General.DefaultModel
	if providerName == "" {
		providerName = "gemini"
	}
	if modelName == "" {
		modelName = "gemini-2.0-flash"
	}

	llm, err := provider.GetProvider(ctx, providerName, modelName, appCfg)
	if err != nil {
		return nil
	}

	// Build prompt for tool matching
	toolList := strings.Join(toolSummaries, "\n")
	prompt := fmt.Sprintf(`You are evaluating which MCP tools can fulfill a user's requirement.

REQUIREMENT: %s

AVAILABLE TOOLS:
%s

TASK: Identify which tools (if any) can fulfill this requirement. 

Respond in this exact JSON format:
{"matches": [{"name": "Tool Name", "reason": "Brief explanation of how this tool fulfills the requirement"}]}

If NO tools can fulfill the requirement, respond with:
{"matches": []}

IMPORTANT: 
- Only include tools that can ACTUALLY fulfill the requirement
- Consider the tool's capabilities based on its description and tags
- For example, Puppeteer can take screenshots even if "screenshot" isn't in its name
- Be selective - only return truly matching tools`, requirement, toolList)

	// Call LLM
	llmReq := &model.LLMRequest{
		Contents: []*genai.Content{
			{
				Role: "user",
				Parts: []*genai.Part{
					genai.NewPartFromText(prompt),
				},
			},
		},
		Config: &genai.GenerateContentConfig{
			Temperature: genai.Ptr(float32(0.1)), // Low temperature for consistent results
		},
	}

	var responseText strings.Builder
	for resp, err := range llm.GenerateContent(ctx, llmReq, false) {
		if err != nil {
			return nil
		}
		if resp != nil && resp.Content != nil {
			for _, part := range resp.Content.Parts {
				if part.Text != "" {
					responseText.WriteString(part.Text)
				}
			}
		}
	}

	// Parse the JSON response
	response := responseText.String()
	
	// Extract JSON from response (may be wrapped in markdown code blocks)
	jsonStart := strings.Index(response, "{")
	jsonEnd := strings.LastIndex(response, "}")
	if jsonStart == -1 || jsonEnd == -1 || jsonEnd <= jsonStart {
		return nil
	}
	jsonStr := response[jsonStart : jsonEnd+1]

	var parsed struct {
		Matches []struct {
			Name   string `json:"name"`
			Reason string `json:"reason"`
		} `json:"matches"`
	}
	if err := json.Unmarshal([]byte(jsonStr), &parsed); err != nil {
		return nil
	}

	// Build results by matching names back to servers
	var results []ToolSearchResult
	for _, match := range parsed.Matches {
		matchNameLower := strings.ToLower(match.Name)
		for _, srv := range servers {
			if strings.ToLower(srv.Name) == matchNameLower {
				// Extract env vars from config if present
				var envVars map[string]string
				if srv.Config != nil && len(srv.Config.Env) > 0 {
					envVars = srv.Config.Env
				}
				
				results = append(results, ToolSearchResult{
					ID:             srv.McpId,
					Name:           srv.Name,
					Description:    srv.Description,
					Source:         srv.Source,
					Tags:           srv.Tags,
					Installable:    true,
					Reason:         match.Reason,
					RequiresApiKey: srv.RequiresApiKey,
					EnvVars:        envVars,
				})
				break
			}
		}
	}

	return results
}

// ExtractMissingToolsFromResponse parses the AI response to detect missing tools
// Returns a list of search queries for tools the AI says are missing
func ExtractMissingToolsFromResponse(response string) []string {
	var queries []string

	// Pattern 1: "tools that are not currently installed:"
	// - tool description: for reason
	missingPattern := regexp.MustCompile(`(?i)tools?\s+(?:that\s+are\s+)?not\s+(?:currently\s+)?installed[:\s]*\n((?:[-•*]\s*[^\n]+\n?)+)`)
	if matches := missingPattern.FindStringSubmatch(response); len(matches) > 1 {
		lines := strings.Split(matches[1], "\n")
		for _, line := range lines {
			line = strings.TrimSpace(line)
			line = strings.TrimPrefix(line, "- ")
			line = strings.TrimPrefix(line, "• ")
			line = strings.TrimPrefix(line, "* ")
			if line != "" {
				// Extract the tool description part (before the colon if present)
				parts := strings.SplitN(line, ":", 2)
				toolDesc := strings.TrimSpace(parts[0])
				if toolDesc != "" {
					queries = append(queries, toolDesc)
				}
			}
		}
	}

	// Pattern 2: "**Missing:** [tool1, tool2]" or "**Missing:**\n- tool1"
	missingPattern2 := regexp.MustCompile(`\*\*Missing:\*\*\s*(?:\[([^\]]+)\]|((?:\n[-•*]\s*[^\n]+)+))`)
	if matches := missingPattern2.FindStringSubmatch(response); len(matches) > 0 {
		if matches[1] != "" {
			// Bracket format: [tool1, tool2]
			tools := strings.Split(matches[1], ",")
			for _, tool := range tools {
				tool = strings.TrimSpace(tool)
				if tool != "" && tool != "none" && !strings.HasPrefix(strings.ToLower(tool), "all") {
					queries = append(queries, tool)
				}
			}
		} else if matches[2] != "" {
			// List format
			lines := strings.Split(matches[2], "\n")
			for _, line := range lines {
				line = strings.TrimSpace(line)
				line = strings.TrimPrefix(line, "- ")
				line = strings.TrimPrefix(line, "• ")
				line = strings.TrimPrefix(line, "* ")
				if line != "" {
					queries = append(queries, line)
				}
			}
		}
	}

	// Pattern 3: "Would you like me to help you find and install" (indicates tool request)
	if strings.Contains(strings.ToLower(response), "help you find and install") {
		// Look for tool-related keywords in the response
		keywords := []string{"screenshot", "browser", "github", "file", "database", "api", "search", "web"}
		for _, kw := range keywords {
			if strings.Contains(strings.ToLower(response), kw) {
				queries = append(queries, kw)
			}
		}
	}

	// Deduplicate
	seen := make(map[string]bool)
	unique := make([]string, 0, len(queries))
	for _, q := range queries {
		lower := strings.ToLower(q)
		if !seen[lower] {
			seen[lower] = true
			unique = append(unique, q)
		}
	}

	return unique
}

// InternetSearchRequest is the request for POST /api/ai/tool-search-internet
type InternetSearchRequest struct {
	Requirement string `json:"requirement"`
}

// InternetMCPResult represents an MCP server found via internet search
type InternetMCPResult struct {
	Name        string            `json:"name"`
	Description string            `json:"description"`
	URL         string            `json:"url"`
	InstallType string            `json:"installType"` // npx, github, etc
	Command     string            `json:"command"`
	Args        []string          `json:"args"`
	EnvVars     map[string]string `json:"envVars,omitempty"`
	Confidence  float64           `json:"confidence"`
	Source      string            `json:"source"` // tavily-search
}

// InternetSearchResponse is the response for POST /api/ai/tool-search-internet
type InternetSearchResponse struct {
	TavilyAvailable bool                `json:"tavilyAvailable"`
	Results         []InternetMCPResult `json:"results"`
	Message         string              `json:"message,omitempty"`
	ToolUsed        string              `json:"toolUsed,omitempty"`   // Name of the MCP tool used for search
	SearchQuery     string              `json:"searchQuery,omitempty"` // The query sent to the tool
}

// URLExtractRequest is the request for POST /api/ai/url-extract
type URLExtractRequest struct {
	URL string `json:"url"`
}

// URLExtractResponse is the response for POST /api/ai/url-extract
type URLExtractResponse struct {
	Found     bool               `json:"found"`
	MCPServer *InternetMCPResult `json:"mcpServer,omitempty"`
	Message   string             `json:"message,omitempty"`
	ToolUsed  string             `json:"toolUsed,omitempty"`
	URL       string             `json:"url"`
}

// IsWebSearchConfigured checks if a web search tool is configured in settings
// Returns (configured, serverName, toolName)
// The setting value format is "serverName:toolName" to uniquely identify tools
func IsWebSearchConfigured() (bool, string) {
	appCfg, err := config.LoadAppConfig()
	if err != nil {
		return false, ""
	}
	if appCfg.General.WebSearchTool == "" {
		return false, ""
	}
	// Parse the format "serverName:toolName" - we return just the server name for MCP initialization
	serverName := appCfg.General.WebSearchTool
	if idx := strings.Index(serverName, ":"); idx > 0 {
		serverName = serverName[:idx]
	}
	return true, serverName
}

// IsWebExtractConfigured checks if a web extract tool is configured in settings
// Returns (configured, serverName)
// The setting value format is "serverName:toolName" to uniquely identify tools
func IsWebExtractConfigured() (bool, string) {
	appCfg, err := config.LoadAppConfig()
	if err != nil {
		return false, ""
	}
	if appCfg.General.WebExtractTool == "" {
		return false, ""
	}
	// Parse the format "serverName:toolName" - we return just the server name for MCP initialization
	serverName := appCfg.General.WebExtractTool
	if idx := strings.Index(serverName, ":"); idx > 0 {
		serverName = serverName[:idx]
	}
	return true, serverName
}

// AIToolSearchInternetHandler handles POST /api/ai/tool-search-internet
func AIToolSearchInternetHandler(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	var req InternetSearchRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// Check if web search tool is configured
	configured, toolName := IsWebSearchConfigured()
	log.Printf("[Internet Search] Configured: %v, ToolName: %s", configured, toolName)
	if !configured {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(InternetSearchResponse{
			TavilyAvailable: false,
			Results:         []InternetMCPResult{},
			Message:         "No web search tool configured. Go to Settings → General to configure an MCP server with 'websearch' in its name.",
		})
		return
	}

	// Clean up the requirement - extract ONLY the technology/tool keywords
	// The requirement often contains garbage like "that is not currently installed"
	cleanedReq := req.Requirement
	cleanedReq = strings.ToLower(cleanedReq)
	
	// Remove common garbage phrases
	garbagePatterns := []string{
		"that is not currently installed",
		"that is currently not installed",
		"not currently installed",
		"currently installed",
		"interacting with",
		"for interacting with",
		"to interact with",
		"would need a tool for",
		"need a tool for",
		"using the tool",
		"using the",
		"the tool",
		"mcp servers",
		"mcp server",
		"model context protocol",
		"development",
		"tool for",
	}
	for _, pattern := range garbagePatterns {
		cleanedReq = strings.ReplaceAll(cleanedReq, pattern, " ")
	}
	
	// Remove quotes, semicolons, and other punctuation
	cleanedReq = regexp.MustCompile(`[";:,\(\)\[\]]`).ReplaceAllString(cleanedReq, " ")
	
	// Clean up whitespace and deduplicate words
	cleanedReq = strings.TrimSpace(cleanedReq)
	cleanedReq = regexp.MustCompile(`\s+`).ReplaceAllString(cleanedReq, " ")
	
	words := strings.Fields(cleanedReq)
	seen := make(map[string]bool)
	uniqueWords := []string{}
	for _, w := range words {
		w = strings.TrimSpace(w)
		if !seen[w] && w != "" && len(w) > 1 {
			seen[w] = true
			uniqueWords = append(uniqueWords, w)
		}
	}
	cleanedReq = strings.Join(uniqueWords, " ")
	
	// Build a clean search query - ALWAYS include "MCP server" for proper results
	searchQuery := fmt.Sprintf("%s MCP server github npm", cleanedReq)
	log.Printf("[Internet Search] Query: %s", searchQuery)

	// Search the internet using the configured MCP tool
	results, err := searchInternetForMCPServers(ctx, searchQuery, toolName)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(InternetSearchResponse{
			TavilyAvailable: true,
			Results:         []InternetMCPResult{},
			Message:         fmt.Sprintf("Search failed: %v", err),
			ToolUsed:        toolName,
			SearchQuery:     searchQuery,
		})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(InternetSearchResponse{
		TavilyAvailable: true,
		Results:         results,
		ToolUsed:        toolName,
		SearchQuery:     searchQuery,
	})
}

// URLExtractHandler handles POST /api/ai/url-extract
// Uses tavily-extract or similar to extract MCP server info from a URL
func URLExtractHandler(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	var req URLExtractRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if req.URL == "" {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(URLExtractResponse{
			Found:   false,
			Message: "No URL provided",
			URL:     req.URL,
		})
		return
	}

	log.Printf("[URL Extract] Starting extraction for: %s", req.URL)

	// Check if web extract tool is configured
	configured, toolName := IsWebExtractConfigured()
	if !configured {
		// Fall back to web search tool
		configured, toolName = IsWebSearchConfigured()
	}
	
	if !configured {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(URLExtractResponse{
			Found:   false,
			Message: "No web extract tool configured. Go to Settings → General to configure one.",
			URL:     req.URL,
		})
		return
	}

	log.Printf("[URL Extract] Using tool: %s", toolName)

	// Extract content from URL
	mcpServer, err := extractMCPServerFromURL(ctx, req.URL, toolName)
	if err != nil {
		log.Printf("[URL Extract] Error: %v", err)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(URLExtractResponse{
			Found:    false,
			Message:  fmt.Sprintf("Failed to extract: %v", err),
			ToolUsed: toolName,
			URL:      req.URL,
		})
		return
	}

	if mcpServer == nil {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(URLExtractResponse{
			Found:    false,
			Message:  "No MCP server configuration found at this URL",
			ToolUsed: toolName,
			URL:      req.URL,
		})
		return
	}

	log.Printf("[URL Extract] Found MCP server: %s", mcpServer.Name)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(URLExtractResponse{
		Found:     true,
		MCPServer: mcpServer,
		ToolUsed:  toolName,
		URL:       req.URL,
	})
}

// extractMCPServerFromURL uses the configured MCP tool to extract content and parse for MCP server config
func extractMCPServerFromURL(ctx context.Context, url string, toolName string) (*InternetMCPResult, error) {
	// Load app configuration
	appCfg, err := config.LoadAppConfig()
	if err != nil {
		return nil, fmt.Errorf("failed to load config: %w", err)
	}

	// Initialize MCP manager
	mcpManager, err := mcp.NewManager()
	if err != nil {
		return nil, fmt.Errorf("failed to create MCP manager: %w", err)
	}
	defer mcpManager.Cleanup()

	log.Printf("[URL Extract] Initializing MCP server: %s", toolName)
	namedToolset, err := mcpManager.InitializeSingleToolset(ctx, toolName)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize MCP server '%s': %w", toolName, err)
	}

	// Get tools and find extract tool
	roCtx := &minimalReadonlyContext{Context: ctx}
	mcpTools, err := namedToolset.Toolset.Tools(roCtx)
	if err != nil {
		return nil, fmt.Errorf("failed to get tools from '%s': %w", toolName, err)
	}

	log.Printf("[URL Extract] Got %d tools from MCP server", len(mcpTools))

	// Find extract tool (look for "extract" in the name)
	var extractTool tool.Tool
	for _, t := range mcpTools {
		log.Printf("[URL Extract] Found tool: %s", t.Name())
		if strings.Contains(strings.ToLower(t.Name()), "extract") {
			extractTool = t
			break
		}
	}

	if extractTool == nil {
		return nil, fmt.Errorf("no extract tool found in MCP server '%s'", toolName)
	}

	log.Printf("[URL Extract] Using extract tool: %s", extractTool.Name())

	// Call the extract tool
	type runnableTool interface {
		Run(tool.Context, any) (map[string]any, error)
	}

	runnable, ok := extractTool.(runnableTool)
	if !ok {
		return nil, fmt.Errorf("extract tool does not implement Run method")
	}

	// Build args for extract tool (tavily-extract uses "urls" array)
	extractArgs := map[string]any{
		"urls": []string{url},
	}

	log.Printf("[URL Extract] Calling extract tool with URL...")
	toolCtx := &minimalToolContext{Context: ctx}
	extractResult, err := runnable.Run(toolCtx, extractArgs)
	if err != nil {
		return nil, fmt.Errorf("extract failed: %w", err)
	}

	// Convert result to JSON for AI processing
	extractResultJSON, err := json.Marshal(extractResult)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal extract results: %w", err)
	}
	log.Printf("[URL Extract] Extracted content: %d bytes", len(extractResultJSON))

	// Use AI to parse extracted content for MCP server config
	providerName := appCfg.General.DefaultProvider
	modelName := appCfg.General.DefaultModel
	if providerName == "" {
		providerName = "gemini"
	}
	if modelName == "" {
		modelName = "gemini-2.0-flash"
	}

	llm, err := provider.GetProvider(ctx, providerName, modelName, appCfg)
	if err != nil {
		return nil, fmt.Errorf("failed to get LLM provider: %w", err)
	}

	prompt := fmt.Sprintf(`You are analyzing a web page to find MCP (Model Context Protocol) server configuration information.

Extracted page content:
%s

Look for:
1. Package name (e.g., @ui5/mcp-server, @sap-ux/fiori-mcp-server)
2. Installation command (usually npx or npm/node)
3. Configuration examples (mcpServers JSON blocks)
4. Required environment variables

If you find an MCP server, respond with a JSON object:
{
  "name": "package-name",
  "description": "What this server does",
  "url": "%s",
  "installType": "npx",
  "command": "npx",
  "args": ["-y", "package-name"],
  "envVars": {},
  "confidence": 0.95
}

If NO MCP server configuration is found, respond with: null

Respond ONLY with the JSON object or null.`, string(extractResultJSON), url)

	llmReq := &model.LLMRequest{
		Contents: []*genai.Content{
			{
				Role: "user",
				Parts: []*genai.Part{
					genai.NewPartFromText(prompt),
				},
			},
		},
		Config: &genai.GenerateContentConfig{
			Temperature: genai.Ptr(float32(0.2)),
		},
	}

	log.Printf("[URL Extract] Calling AI to parse extracted content...")
	var responseText strings.Builder
	for resp, err := range llm.GenerateContent(ctx, llmReq, false) {
		if err != nil {
			return nil, fmt.Errorf("AI parsing failed: %w", err)
		}
		if resp != nil && resp.Content != nil {
			for _, part := range resp.Content.Parts {
				if part.Text != "" {
					responseText.WriteString(part.Text)
				}
			}
		}
	}

	response := strings.TrimSpace(responseText.String())
	log.Printf("[URL Extract] AI response: %s", response)

	if response == "null" || response == "" {
		return nil, nil
	}

	// Extract JSON from response
	jsonStart := strings.Index(response, "{")
	jsonEnd := strings.LastIndex(response, "}")
	if jsonStart == -1 || jsonEnd == -1 || jsonEnd <= jsonStart {
		return nil, nil
	}
	jsonStr := response[jsonStart : jsonEnd+1]

	var result InternetMCPResult
	if err := json.Unmarshal([]byte(jsonStr), &result); err != nil {
		log.Printf("[URL Extract] JSON parse error: %v", err)
		return nil, nil
	}

	result.Source = toolName
	return &result, nil
}


type InternetMCPInstallRequest struct {
	Name    string            `json:"name"`
	Command string            `json:"command"`
	Args    []string          `json:"args"`
	Env     map[string]string `json:"env"`
}

// InternetMCPInstallResponse is the response for /api/mcp-internet-install
type InternetMCPInstallResponse struct {
	Status      string `json:"status"`
	ToolsLoaded int    `json:"toolsLoaded,omitempty"`
	Error       string `json:"error,omitempty"`
}

// InternetMCPInstallHandler handles POST /api/mcp-internet-install
func InternetMCPInstallHandler(w http.ResponseWriter, r *http.Request) {
	var req InternetMCPInstallRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if req.Name == "" || req.Command == "" {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(InternetMCPInstallResponse{
			Status: "error",
			Error:  "Name and command are required",
		})
		return
	}

	// Sanitize server name
	serverName := strings.ToLower(strings.ReplaceAll(req.Name, " ", "-"))
	serverName = regexp.MustCompile(`[^a-z0-9-]`).ReplaceAllString(serverName, "")
	if serverName == "" {
		serverName = "internet-mcp-server"
	}

	// Load current MCP config
	mcpCfg, err := config.LoadMCPConfig()
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(InternetMCPInstallResponse{
			Status: "error",
			Error:  fmt.Sprintf("Failed to load MCP config: %v", err),
		})
		return
	}

	// Check if server already exists
	if _, exists := mcpCfg.MCPServers[serverName]; exists {
		serverName = serverName + "-" + fmt.Sprintf("%d", len(mcpCfg.MCPServers)+1)
	}

	// Create the server config
	newServer := config.MCPServerConfig{
		Command: req.Command,
		Args:    req.Args,
		Env:     req.Env,
	}

	mcpCfg.MCPServers[serverName] = newServer

	if err := config.SaveMCPConfig(mcpCfg); err != nil {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(InternetMCPInstallResponse{
			Status: "error",
			Error:  fmt.Sprintf("Failed to save MCP config: %v", err),
		})
		return
	}

	RefreshToolsCache(context.Background())

	toolsLoaded := 0
	cachedTools := GetCachedTools()
	for _, t := range cachedTools {
		if t.Source == serverName {
			toolsLoaded++
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(InternetMCPInstallResponse{
		Status:      "ok",
		ToolsLoaded: toolsLoaded,
	})
}

// searchInternetForMCPServers uses the configured MCP web search tool to find MCP servers
func searchInternetForMCPServers(ctx context.Context, searchQuery string, toolName string) ([]InternetMCPResult, error) {
	log.Printf("[searchInternetForMCPServers] Starting with toolName=%s, query=%s", toolName, searchQuery)
	
	// Load app configuration
	appCfg, err := config.LoadAppConfig()
	if err != nil {
		return nil, fmt.Errorf("failed to load config: %w", err)
	}

	// Initialize MCP manager and the specific web search server
	mcpManager, err := mcp.NewManager()
	if err != nil {
		return nil, fmt.Errorf("failed to create MCP manager: %w", err)
	}
	defer mcpManager.Cleanup()

	log.Printf("[searchInternetForMCPServers] Initializing MCP server: %s", toolName)
	// Initialize just the web search MCP server
	namedToolset, err := mcpManager.InitializeSingleToolset(ctx, toolName)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize web search MCP server '%s': %w", toolName, err)
	}

	// Get the tools from this server using minimalReadonlyContext
	roCtx := &minimalReadonlyContext{Context: ctx}
	mcpTools, err := namedToolset.Toolset.Tools(roCtx)
	if err != nil {
		return nil, fmt.Errorf("failed to get tools from '%s': %w", toolName, err)
	}
	log.Printf("[searchInternetForMCPServers] Got %d tools from MCP server", len(mcpTools))

	// Find a search tool (look for "search" in the name)
	var searchTool tool.Tool
	for _, t := range mcpTools {
		log.Printf("[searchInternetForMCPServers] Found tool: %s", t.Name())
		if strings.Contains(strings.ToLower(t.Name()), "search") {
			searchTool = t
			break
		}
	}

	if searchTool == nil {
		return nil, fmt.Errorf("no search tool found in MCP server '%s'", toolName)
	}
	log.Printf("[searchInternetForMCPServers] Using search tool: %s", searchTool.Name())

	// Call the search tool using the standard Run interface
	type runnableTool interface {
		Run(tool.Context, any) (map[string]any, error)
	}

	runnable, ok := searchTool.(runnableTool)
	if !ok {
		return nil, fmt.Errorf("search tool does not implement Run method")
	}

	// Build args for the search tool (common pattern for Tavily-like tools)
	searchArgs := map[string]any{
		"query": searchQuery,
	}
	log.Printf("[searchInternetForMCPServers] Calling search tool with query...")

	// Create a minimal tool context
	toolCtx := &minimalToolContext{Context: ctx}
	searchResult, err := runnable.Run(toolCtx, searchArgs)
	if err != nil {
		return nil, fmt.Errorf("web search failed: %w", err)
	}
	log.Printf("[searchInternetForMCPServers] Search returned %d result keys", len(searchResult))

	// Convert search results to JSON for AI processing
	searchResultJSON, err := json.Marshal(searchResult)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal search results: %w", err)
	}
	log.Printf("[searchInternetForMCPServers] Search result JSON length: %d bytes", len(searchResultJSON))
	// Log first 1000 chars of search results to see what Tavily returns
	previewLen := min(1000, len(searchResultJSON))
	log.Printf("[searchInternetForMCPServers] Search result preview: %s", string(searchResultJSON[:previewLen]))

	// Use AI to parse search results into MCP server suggestions
	providerName := appCfg.General.DefaultProvider
	modelName := appCfg.General.DefaultModel
	if providerName == "" {
		providerName = "gemini"
	}
	if modelName == "" {
		modelName = "gemini-2.0-flash"
	}

	llm, err := provider.GetProvider(ctx, providerName, modelName, appCfg)
	if err != nil {
		return nil, fmt.Errorf("failed to get LLM provider: %w", err)
	}

	prompt := fmt.Sprintf(`You are analyzing web search results to find MCP (Model Context Protocol) servers.

Search results:
%s

Extract any MCP servers mentioned in these results. For each server found, provide:
- name: The npm package name or GitHub repo (e.g., "@anthropic/mcp-server-fetch" or "user/repo-name")
- description: What it does
- url: The GitHub or npm URL
- installType: "npx" for npm packages, "github" for GitHub repos
- command: "npx" for npm, "node" for github
- args: Installation arguments as JSON array (e.g., ["-y", "package-name"])
- envVars: Required environment variables as JSON object
- confidence: How confident you are (0.0-1.0)

Respond ONLY with a JSON array. If no MCP servers found, return [].

Example:
[{"name": "mcp-server-sqlite", "description": "SQLite database access", "url": "https://github.com/modelcontextprotocol/servers", "installType": "npx", "command": "npx", "args": ["-y", "@modelcontextprotocol/server-sqlite"], "envVars": {}, "confidence": 0.9}]`, string(searchResultJSON))

	llmReq := &model.LLMRequest{
		Contents: []*genai.Content{
			{
				Role: "user",
				Parts: []*genai.Part{
					genai.NewPartFromText(prompt),
				},
			},
		},
		Config: &genai.GenerateContentConfig{
			Temperature: genai.Ptr(float32(0.3)),
		},
	}

	log.Printf("[searchInternetForMCPServers] Calling AI to parse search results...")
	var responseText strings.Builder
	for resp, err := range llm.GenerateContent(ctx, llmReq, false) {
		if err != nil {
			return nil, fmt.Errorf("AI parsing failed: %w", err)
		}
		if resp != nil && resp.Content != nil {
			for _, part := range resp.Content.Parts {
				if part.Text != "" {
					responseText.WriteString(part.Text)
				}
			}
		}
	}

	response := strings.TrimSpace(responseText.String())
	log.Printf("[searchInternetForMCPServers] AI response length: %d chars", len(response))

	jsonStart := strings.Index(response, "[")
	jsonEnd := strings.LastIndex(response, "]")
	if jsonStart == -1 || jsonEnd == -1 || jsonEnd <= jsonStart {
		log.Printf("[searchInternetForMCPServers] No JSON array found in AI response: %s", response[:min(200, len(response))])
		return []InternetMCPResult{}, nil
	}
	jsonStr := response[jsonStart : jsonEnd+1]
	log.Printf("[searchInternetForMCPServers] Extracted JSON length: %d", len(jsonStr))

	var results []InternetMCPResult
	if err := json.Unmarshal([]byte(jsonStr), &results); err != nil {
		log.Printf("[searchInternetForMCPServers] JSON parse error: %v", err)
		return []InternetMCPResult{}, nil
	}

	log.Printf("[searchInternetForMCPServers] Parsed %d MCP server results", len(results))
	for i := range results {
		results[i].Source = toolName
	}

	return results, nil
}

