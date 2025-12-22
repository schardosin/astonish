package api

import (
	"encoding/json"
	"net/http"
	"strings"
)

// WebCapableToolsResponse is the response for GET /api/tools/web-capable
type WebCapableToolsResponse struct {
	WebSearch  []WebCapableTool `json:"webSearch"`
	WebExtract []WebCapableTool `json:"webExtract"`
}

// WebCapableTool represents a tool that can be used for web search or extract
type WebCapableTool struct {
	Name   string `json:"name"`   // Full tool name (e.g., "mcp_tavily_tavily-websearch")
	Source string `json:"source"` // MCP server source name
}

// WebCapableToolsHandler returns tools containing "websearch" or "webextract" in their name
// GET /api/tools/web-capable
func WebCapableToolsHandler(w http.ResponseWriter, r *http.Request) {
	cachedTools := GetCachedTools()

	response := WebCapableToolsResponse{
		WebSearch:  []WebCapableTool{},
		WebExtract: []WebCapableTool{},
	}

	for _, tool := range cachedTools {
		lowerName := strings.ToLower(tool.Name)
		lowerSource := strings.ToLower(tool.Source)

		// Both webSearch and webExtract use the same "websearch" naming convention
		// since tools like Tavily offer both search and extract in the same MCP server
		if strings.Contains(lowerName, "websearch") || strings.Contains(lowerSource, "websearch") {
			webTool := WebCapableTool{
				Name:   tool.Name,
				Source: tool.Source,
			}
			response.WebSearch = append(response.WebSearch, webTool)
			response.WebExtract = append(response.WebExtract, webTool)
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}
