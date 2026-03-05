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
var fleetRegistryVar *fleet.Registry

// SetFleetRegistries registers the fleet and persona registries for the fleet tools.
// The persona registry is accepted for future use by the fleet session manager.
func SetFleetRegistries(fleetReg *fleet.Registry, _ *persona.Registry) {
	fleetRegistryVar = fleetReg
}

// GetFleetTools returns fleet-related tools that sub-agents can use (e.g., opencode).
// In fleet v2, the orchestrator/phase tools are gone. The only fleet-specific tool
// that sub-agents need is the opencode delegate tool.
func GetFleetTools() ([]tool.Tool, error) {
	ocT, err := NewOpenCodeTool()
	if err != nil {
		return nil, err
	}
	return []tool.Tool{ocT}, nil
}

// ListAvailableFleets returns a formatted string listing available fleets.
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
	sb.WriteString("\nFleet sessions are started via the Studio UI or CLI (`astonish fleet start`).\n")
	return sb.String()
}
