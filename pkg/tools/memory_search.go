package tools

import (
	"fmt"

	"github.com/schardosin/astonish/pkg/memory"
	"google.golang.org/adk/tool"
	"google.golang.org/adk/tool/functiontool"
)

// MemorySearchArgs defines the arguments for the memory_search tool.
type MemorySearchArgs struct {
	Query      string `json:"query" jsonschema:"The search query to find relevant knowledge in memory"`
	MaxResults int    `json:"max_results,omitempty" jsonschema:"Maximum number of results to return (default 6)"`
}

// MemorySearchResult is returned from memory search.
type MemorySearchResult struct {
	Results []memory.SearchResult `json:"results"`
	Count   int                   `json:"count"`
	Message string                `json:"message,omitempty"`
}

// MemorySearch performs semantic search across indexed memory files.
func MemorySearch(store *memory.Store) func(ctx tool.Context, args MemorySearchArgs) (MemorySearchResult, error) {
	return func(ctx tool.Context, args MemorySearchArgs) (MemorySearchResult, error) {
		if args.Query == "" {
			return MemorySearchResult{}, fmt.Errorf("query is required")
		}

		maxResults := args.MaxResults
		if maxResults <= 0 {
			maxResults = store.Config().MaxResults
		}

		results, err := store.Search(ctx, args.Query, maxResults, store.Config().MinScore)
		if err != nil {
			return MemorySearchResult{}, fmt.Errorf("memory search failed: %w", err)
		}

		if len(results) == 0 {
			return MemorySearchResult{
				Results: []memory.SearchResult{},
				Count:   0,
				Message: "No matching results found in memory.",
			}, nil
		}

		return MemorySearchResult{
			Results: results,
			Count:   len(results),
		}, nil
	}
}

// NewMemorySearchTool creates the memory_search tool using the given store.
func NewMemorySearchTool(store *memory.Store) (tool.Tool, error) {
	return functiontool.New(functiontool.Config{
		Name: "memory_search",
		Description: "Search memory for relevant knowledge. Use before answering questions " +
			"about prior decisions, preferences, facts, project details, or past conversations. " +
			"Returns scored snippets with file path and line references. " +
			"Use memory_get to read more context around a search result.",
	}, MemorySearch(store))
}
