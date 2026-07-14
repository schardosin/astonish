package tools

import (
	"context"
	"fmt"
	"strings"

	"github.com/schardosin/astonish/pkg/fleet"
	"github.com/schardosin/astonish/pkg/store"
	"google.golang.org/adk/tool"
)

// Package-level registries for fleet execution.
// Set by the launcher via SetFleetRegistry.
var fleetRegistryVar *fleet.Registry

// SetFleetRegistry registers the fleet registry for the fleet tools.
func SetFleetRegistry(fleetReg *fleet.Registry) {
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
	taskTools, err := GetFleetTaskTools()
	if err != nil {
		return nil, err
	}
	return append([]tool.Tool{ocT}, taskTools...), nil
}

// getEffectiveFleetTemplateStore returns the FleetTemplateStore from context (platform mode)
// or wraps the file-based registry (personal mode).
func getEffectiveFleetTemplateStore(ctx context.Context) store.FleetTemplateStore {
	if ctx != nil {
		if fs := store.FleetTemplateStoreFromContext(ctx); fs != nil {
			return fs
		}
	}
	// Personal mode fallback: wrap the file-based registry
	if fleetRegistryVar != nil {
		return &fleetRegistryStoreAdapter{reg: fleetRegistryVar}
	}
	return nil
}

// fleetRegistryStoreAdapter adapts *fleet.Registry to store.FleetTemplateStore
// for use in personal mode fallback within tools.
type fleetRegistryStoreAdapter struct {
	reg *fleet.Registry
}

func (a *fleetRegistryStoreAdapter) GetFleet(_ context.Context, key string) (any, bool) {
	return a.reg.GetFleet(key)
}

func (a *fleetRegistryStoreAdapter) ListFleets(_ context.Context) []store.FleetTemplateSummary {
	summaries := a.reg.ListFleets()
	result := make([]store.FleetTemplateSummary, len(summaries))
	for i, s := range summaries {
		result[i] = store.FleetTemplateSummary{
			Key:         s.Key,
			Name:        s.Name,
			Description: s.Description,
			AgentCount:  s.AgentCount,
			AgentNames:  s.AgentNames,
		}
	}
	return result
}

func (a *fleetRegistryStoreAdapter) Save(_ context.Context, _ string, _ any) error {
	return fmt.Errorf("save not supported via registry adapter")
}

func (a *fleetRegistryStoreAdapter) Delete(_ context.Context, _ string) error {
	return fmt.Errorf("delete not supported via registry adapter")
}

func (a *fleetRegistryStoreAdapter) Count(_ context.Context) int {
	return a.reg.Count()
}

func (a *fleetRegistryStoreAdapter) Reload(_ context.Context) error {
	return a.reg.Reload()
}

// ListAvailableFleets returns a formatted string listing available fleets.
// Uses the file-based registry (personal mode / system prompt building at init time).
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

// ListAvailableFleetsFromContext returns a formatted string listing available fleets,
// resolving from context (platform mode) or falling back to the global registry.
func ListAvailableFleetsFromContext(ctx context.Context) string {
	fs := getEffectiveFleetTemplateStore(ctx)
	if fs == nil {
		return "Fleet system is not initialized."
	}
	summaries := fs.ListFleets(ctx)
	if len(summaries) == 0 {
		return "No fleets available."
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
