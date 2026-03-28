package tools

import (
	"context"
	"fmt"
	"sort"

	"github.com/schardosin/astonish/pkg/agent"
	"google.golang.org/adk/tool"
	"google.golang.org/adk/tool/functiontool"
)

// SearchToolsArgs defines the arguments for the search_tools tool.
type SearchToolsArgs struct {
	Query      string `json:"query" jsonschema:"Describe what you want to do (e.g., 'take a screenshot', 'send an email', 'check API health'). Use '*' or 'list all' to list every available tool."`
	MaxResults int    `json:"max_results,omitempty" jsonschema:"Maximum number of results to return (default 10, ignored when listing all)"`
}

// SearchToolsMatchResult is a single tool match in the search results.
type SearchToolsMatchResult struct {
	ToolName    string  `json:"tool_name"`
	GroupName   string  `json:"group_name"`
	Description string  `json:"description"`
	IsMainTool  bool    `json:"is_main_tool"`
	Score       float64 `json:"score"`
	Access      string  `json:"access"`
}

// SearchToolsResult is returned from tool search.
type SearchToolsResult struct {
	Matches []SearchToolsMatchResult `json:"matches"`
	Count   int                      `json:"count"`
	Message string                   `json:"message,omitempty"`
}

// isListAllQuery returns true if the query is requesting a full tool inventory.
func isListAllQuery(query string) bool {
	switch query {
	case "*", "list all", "list all tools", "all", "all tools":
		return true
	}
	return false
}

// SearchTools performs semantic search across the tool index.
// When onResults is non-nil, it is called with the matched tool names so the
// dynamic injection system can make them available for direct use.
func SearchTools(toolIndex *agent.ToolIndex, onResults func([]string)) func(ctx tool.Context, args SearchToolsArgs) (SearchToolsResult, error) {
	return func(ctx tool.Context, args SearchToolsArgs) (SearchToolsResult, error) {
		if args.Query == "" {
			return SearchToolsResult{}, fmt.Errorf("query is required — describe what you want to do, or use '*' to list all tools")
		}

		// Handle "list all" mode
		if isListAllQuery(args.Query) {
			result := listAllTools(toolIndex)
			// Notify injection system about all tools
			if onResults != nil && len(result.Matches) > 0 {
				names := make([]string, len(result.Matches))
				for i, m := range result.Matches {
					names[i] = m.ToolName
				}
				onResults(names)
			}
			return result, nil
		}

		maxResults := args.MaxResults
		if maxResults <= 0 {
			maxResults = 10
		}

		// Use background context if tool.Context is nil (e.g., in tests)
		var searchCtx context.Context
		if ctx != nil {
			searchCtx = ctx
		} else {
			searchCtx = context.Background()
		}

		matches, err := toolIndex.SearchHybrid(searchCtx, args.Query, maxResults, 0.005)
		if err != nil {
			return SearchToolsResult{}, fmt.Errorf("tool search failed: %w", err)
		}

		if len(matches) == 0 {
			return SearchToolsResult{
				Matches: []SearchToolsMatchResult{},
				Count:   0,
				Message: "No matching tools found. Try a different query, use '*' to list all tools, or check the system prompt for available tool groups.",
			}, nil
		}

		results := make([]SearchToolsMatchResult, len(matches))
		for i, m := range matches {
			access := "available (call directly)"
			if m.IsMainTool {
				access = "always available (main thread tool)"
			}
			results[i] = SearchToolsMatchResult{
				ToolName:    m.ToolName,
				GroupName:   m.GroupName,
				Description: m.Description,
				IsMainTool:  m.IsMainTool,
				Score:       m.Score,
				Access:      access,
			}
		}

		// Notify the dynamic injection system so these tools become callable
		// on the very next LLM call within this turn.
		if onResults != nil {
			names := make([]string, len(matches))
			for i, m := range matches {
				names[i] = m.ToolName
			}
			onResults(names)
		}

		return SearchToolsResult{
			Matches: results,
			Count:   len(results),
		}, nil
	}
}

// listAllTools returns every tool in the index, grouped by group name.
func listAllTools(toolIndex *agent.ToolIndex) SearchToolsResult {
	groups := toolIndex.ListAll()

	var results []SearchToolsMatchResult
	// Sort group names for deterministic output
	groupNames := make([]string, 0, len(groups))
	for g := range groups {
		groupNames = append(groupNames, g)
	}
	sort.Strings(groupNames)

	for _, gName := range groupNames {
		for _, m := range groups[gName] {
			access := "available (call directly)"
			if m.IsMainTool {
				access = "always available (main thread tool)"
			}
			results = append(results, SearchToolsMatchResult{
				ToolName:    m.ToolName,
				GroupName:   m.GroupName,
				Description: m.Description,
				IsMainTool:  m.IsMainTool,
				Score:       1.0,
				Access:      access,
			})
		}
	}

	return SearchToolsResult{
		Matches: results,
		Count:   len(results),
		Message: fmt.Sprintf("Complete tool inventory: %d tools across %d groups", len(results), len(groups)),
	}
}

// NewSearchToolsTool creates the search_tools tool using the given tool index.
// When onResults is non-nil, tools found via search become available for direct
// invocation through the dynamic tool injection system.
func NewSearchToolsTool(toolIndex *agent.ToolIndex, onResults func([]string)) (tool.Tool, error) {
	return functiontool.New(functiontool.Config{
		Name: "search_tools",
		Description: "Search for available tools by describing what you want to do. " +
			"Found tools become available for you to call directly. " +
			"Use this when you need a capability that isn't currently available. " +
			"Use query='*' to list ALL available tools.",
	}, SearchTools(toolIndex, onResults))
}
