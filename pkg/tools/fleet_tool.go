package tools

import (
	"fmt"
	"strings"

	"github.com/schardosin/astonish/pkg/fleet"
	"github.com/schardosin/astonish/pkg/persona"
	"google.golang.org/adk/tool"
)

// Package-level registries for fleet execution.
// Set by the launcher via SetFleetRegistries.
var (
	fleetRegistryVar   *fleet.Registry
	personaRegistryVar *persona.Registry
)

// SetFleetRegistries registers the fleet and persona registries for the fleet tool.
func SetFleetRegistries(fleetReg *fleet.Registry, personaReg *persona.Registry) {
	fleetRegistryVar = fleetReg
	personaRegistryVar = personaReg
}

// ensureTool adds a tool name to the filter list if not already present.
// If the filter is nil (meaning all tools), returns nil (keep all tools).
func ensureTool(filter []string, name string) []string {
	if filter == nil {
		return nil
	}
	for _, t := range filter {
		if t == name {
			return filter
		}
	}
	return append(filter, name)
}

// GetFleetTools returns fleet-related tools if the fleet system is available.
func GetFleetTools() ([]tool.Tool, error) {
	phaseT, err := NewRunFleetPhaseTool()
	if err != nil {
		return nil, err
	}
	ocT, err := NewOpenCodeTool()
	if err != nil {
		return nil, err
	}
	return []tool.Tool{phaseT, ocT}, nil
}

// ListAvailableFleets returns a formatted string listing available fleets.
// Used by slash command handlers to show fleet info without leaking internal prompts.
func ListAvailableFleets() string {
	if fleetRegistryVar == nil {
		return "Fleet system is not initialized."
	}
	summaries := fleetRegistryVar.ListFleets()
	if len(summaries) == 0 {
		return "No fleets available. Add fleet configurations to `~/.config/astonish/fleets/`."
	}
	var sb strings.Builder
	sb.WriteString("**Available Fleets:**\n\n")
	for _, s := range summaries {
		sb.WriteString(fmt.Sprintf("- **%s** (`%s`) — %s\n", s.Name, s.Key, s.Description))
		sb.WriteString(fmt.Sprintf("  Agents: %s\n", strings.Join(s.AgentNames, ", ")))
	}
	sb.WriteString("\nUse `/fleet <task description>` to start a fleet-based task.\n")
	sb.WriteString("Example: `/fleet build a REST API for user management`")
	return sb.String()
}
