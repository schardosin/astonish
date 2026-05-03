package tools

import (
	"fmt"

	"github.com/schardosin/astonish/pkg/memory"
	"github.com/schardosin/astonish/pkg/store"
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

// PlatformMemorySearchResult is returned from memory search in platform mode.
// Uses store.MemorySearchResult which includes scope information.
type PlatformMemorySearchResult struct {
	Results []store.MemorySearchResult `json:"results"`
	Count   int                        `json:"count"`
	Message string                     `json:"message,omitempty"`
}

// MemorySearch performs hybrid search (vector + BM25) across indexed memory files.
func MemorySearch(memStore *memory.Store) func(ctx tool.Context, args MemorySearchArgs) (MemorySearchResult, error) {
	return func(ctx tool.Context, args MemorySearchArgs) (MemorySearchResult, error) {
		if args.Query == "" {
			return MemorySearchResult{}, fmt.Errorf("query is required")
		}

		maxResults := args.MaxResults
		if maxResults <= 0 {
			maxResults = memStore.Config().MaxResults
		}

		results, err := memStore.SearchHybrid(ctx, args.Query, maxResults, memStore.Config().MinScore)
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

// PlatformMemorySearch performs cross-tier memory search in platform mode.
// When a ThreeTierSearcher is provided, it searches personal + team + org tiers.
// Falls back to single-tier search via the store.MemoryStore if no three-tier
// searcher is available.
func PlatformMemorySearch(searcher store.ThreeTierSearcher, fallback store.MemoryStore) func(ctx tool.Context, args MemorySearchArgs) (PlatformMemorySearchResult, error) {
	return func(ctx tool.Context, args MemorySearchArgs) (PlatformMemorySearchResult, error) {
		if args.Query == "" {
			return PlatformMemorySearchResult{}, fmt.Errorf("query is required")
		}

		maxResults := args.MaxResults
		if maxResults <= 0 {
			maxResults = 6
		}

		var results []store.MemorySearchResult
		var err error

		if searcher != nil {
			results, err = searcher.SearchAllTiers(ctx, args.Query, maxResults, 0)
		} else if fallback != nil {
			results, err = fallback.Search(ctx, args.Query, maxResults, 0)
		} else {
			return PlatformMemorySearchResult{
				Results: []store.MemorySearchResult{},
				Count:   0,
				Message: "Memory search is not available.",
			}, nil
		}

		if err != nil {
			return PlatformMemorySearchResult{}, fmt.Errorf("memory search failed: %w", err)
		}

		if len(results) == 0 {
			return PlatformMemorySearchResult{
				Results: []store.MemorySearchResult{},
				Count:   0,
				Message: "No matching results found in memory.",
			}, nil
		}

		return PlatformMemorySearchResult{
			Results: results,
			Count:   len(results),
		}, nil
	}
}

// NewMemorySearchTool creates the memory_search tool using the given store.
func NewMemorySearchTool(memStore *memory.Store) (tool.Tool, error) {
	return functiontool.New(functiontool.Config{
		Name: "memory_search",
		Description: "Search memory for relevant knowledge. Use before answering questions " +
			"about prior decisions, preferences, facts, project details, or past conversations. " +
			"Returns scored snippets with file path and line references. " +
			"Use memory_get to read more context around a search result.",
	}, MemorySearch(memStore))
}

// NewPlatformMemorySearchTool creates the memory_search tool for platform mode.
// Performs cross-tier search (personal + team + org) when a ThreeTierSearcher
// is provided. Results include a "scope" field indicating the knowledge tier.
func NewPlatformMemorySearchTool(searcher store.ThreeTierSearcher, fallback store.MemoryStore) (tool.Tool, error) {
	return functiontool.New(functiontool.Config{
		Name: "memory_search",
		Description: "Search memory for relevant knowledge across all knowledge tiers " +
			"(personal, team, organization). Use before answering questions about prior decisions, " +
			"preferences, facts, project details, or past conversations. " +
			"Results include a 'scope' field (personal/team/org) indicating the knowledge tier.",
	}, PlatformMemorySearch(searcher, fallback))
}
