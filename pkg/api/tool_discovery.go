package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strings"

	"github.com/schardosin/astonish/pkg/config"
	"github.com/schardosin/astonish/pkg/mcpstore"
	"github.com/schardosin/astonish/pkg/provider"
	"google.golang.org/adk/model"
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
