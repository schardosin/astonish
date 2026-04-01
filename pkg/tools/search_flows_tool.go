package tools

import (
	"strings"

	"github.com/schardosin/astonish/pkg/agent"
	"google.golang.org/adk/tool"
	"google.golang.org/adk/tool/functiontool"
)

// flowRegistryVar holds the flow registry for search_flows and run_flow tools.
var flowRegistryVar *agent.FlowRegistry

// SetFlowRegistry wires the flow registry into the flow tools.
func SetFlowRegistry(reg *agent.FlowRegistry) {
	flowRegistryVar = reg
}

// --- search_flows tool ---

// SearchFlowsArgs defines the arguments for the search_flows tool.
type SearchFlowsArgs struct {
	Query string `json:"query,omitempty" jsonschema:"Optional search query to filter flows by name, description, or tags. If empty, lists all available flows."`
}

// SearchFlowsResult is returned from search_flows.
type SearchFlowsResult struct {
	Status string      `json:"status"`
	Flows  []FlowMatch `json:"flows,omitempty"`
	Count  int         `json:"count"`
}

// FlowMatch describes a single flow match.
type FlowMatch struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Tags        []string `json:"tags,omitempty"`
}

func searchFlows(_ tool.Context, args SearchFlowsArgs) (SearchFlowsResult, error) {
	if flowRegistryVar == nil {
		return SearchFlowsResult{
			Status: "error",
		}, nil
	}

	entries := flowRegistryVar.Entries()
	if len(entries) == 0 {
		return SearchFlowsResult{
			Status: "ok",
		}, nil
	}

	query := strings.ToLower(strings.TrimSpace(args.Query))
	var matches []FlowMatch

	for _, e := range entries {
		// Skip drills and drill suites — they're a different concept
		if e.Type == "drill" || e.Type == "drill_suite" {
			continue
		}

		// If no query, return all flows
		if query == "" {
			matches = append(matches, FlowMatch{
				Name:        strings.TrimSuffix(e.FlowFile, ".yaml"),
				Description: e.Description,
				Tags:        e.Tags,
			})
			continue
		}

		// Match against name, description, and tags
		name := strings.ToLower(strings.TrimSuffix(e.FlowFile, ".yaml"))
		desc := strings.ToLower(e.Description)

		// Split query into words for flexible matching
		queryWords := strings.Fields(query)
		matched := true
		for _, word := range queryWords {
			found := strings.Contains(name, word) || strings.Contains(desc, word)
			if !found {
				for _, tag := range e.Tags {
					if strings.Contains(strings.ToLower(tag), word) {
						found = true
						break
					}
				}
			}
			if !found {
				matched = false
				break
			}
		}

		if matched {
			matches = append(matches, FlowMatch{
				Name:        strings.TrimSuffix(e.FlowFile, ".yaml"),
				Description: e.Description,
				Tags:        e.Tags,
			})
		}
	}

	return SearchFlowsResult{
		Status: "ok",
		Flows:  matches,
		Count:  len(matches),
	}, nil
}

// NewSearchFlowsTool creates the search_flows tool.
func NewSearchFlowsTool() (tool.Tool, error) {
	return functiontool.New(functiontool.Config{
		Name: "search_flows",
		Description: "Search for saved flows/workflows. Use this when the user asks to run a workflow, " +
			"execute a flow, or when their request sounds like it could match a saved automation. " +
			"Returns matching flows with name, description, and tags. " +
			"Call with empty query to list all available flows.",
	}, searchFlows)
}
