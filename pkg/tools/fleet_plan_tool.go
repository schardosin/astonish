package tools

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/schardosin/astonish/pkg/fleet"
	"google.golang.org/adk/tool"
	"google.golang.org/adk/tool/functiontool"
)

// Package-level fleet plans directory. Set by the launcher via SetFleetPlansDir.
var fleetPlansDirVar string

// SetFleetPlansDir registers the fleet plans directory path.
func SetFleetPlansDir(dir string) {
	fleetPlansDirVar = dir
}

// --- fleet_plan tool ---

// FleetPlanArgs is the input schema for the fleet_plan tool.
type FleetPlanArgs struct {
	Request  string `json:"request" jsonschema:"Description of the user's request or task. This is used to contextualize the plan."`
	FleetKey string `json:"fleet_key,omitempty" jsonschema:"Optional: specific fleet to use (e.g., 'software-dev'). If omitted, the system auto-detects the best matching fleet."`
}

// FleetPlanResult is the output of the fleet_plan tool.
type FleetPlanResult struct {
	FleetKey    string          `json:"fleet_key"`
	FleetName   string          `json:"fleet_name"`
	Description string          `json:"description"`
	Phases      []PlanPhaseInfo `json:"phases"`
	Agents      []AgentInfo     `json:"agents"`
	SavedPlans  []SavedPlanInfo `json:"saved_plans,omitempty"`
	Message     string          `json:"message"`
	Error       string          `json:"error,omitempty"`
}

// AgentInfo describes an available agent in the fleet for plan customization.
type AgentInfo struct {
	Key         string `json:"key"`
	PersonaName string `json:"persona_name"`
	Description string `json:"description,omitempty"`
	HasDelegate bool   `json:"has_delegate,omitempty"`
}

// PlanPhaseInfo describes a single phase in the plan proposal.
type PlanPhaseInfo struct {
	Order        int      `json:"order"`
	Name         string   `json:"name"`
	Agent        string   `json:"agent"`
	PersonaName  string   `json:"persona_name"`
	Description  string   `json:"description"`
	Deliverables []string `json:"deliverables,omitempty"`
	DependsOn    []string `json:"depends_on,omitempty"`
}

// SavedPlanInfo summarizes an existing custom fleet plan.
type SavedPlanInfo struct {
	Key         string `json:"key"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	PhaseCount  int    `json:"phase_count"`
	PlanJSON    string `json:"plan_json"` // Full plan as JSON for the LLM to use
}

func fleetPlan(_ tool.Context, args FleetPlanArgs) (FleetPlanResult, error) {
	if fleetRegistryVar == nil || personaRegistryVar == nil {
		return FleetPlanResult{
			Error: "Fleet system is not initialized.",
		}, nil
	}

	// Determine which fleet to use
	fleetKey := args.FleetKey
	var fleetCfg *fleet.FleetConfig

	if fleetKey != "" {
		// Specific fleet requested
		var ok bool
		fleetCfg, ok = fleetRegistryVar.GetFleet(fleetKey)
		if !ok {
			available := fleetRegistryVar.ListFleets()
			keys := make([]string, len(available))
			for i, f := range available {
				keys[i] = f.Key
			}
			return FleetPlanResult{
				Error: fmt.Sprintf("Fleet %q not found. Available fleets: %s", fleetKey, strings.Join(keys, ", ")),
			}, nil
		}
	} else {
		// Auto-detect: pick the first available fleet (for v1, simple matching)
		summaries := fleetRegistryVar.ListFleets()
		if len(summaries) == 0 {
			return FleetPlanResult{
				Error: "No fleets are available.",
			}, nil
		}
		// Use the first fleet (typically software-dev)
		fleetKey = summaries[0].Key
		fleetCfg, _ = fleetRegistryVar.GetFleet(fleetKey)
		if fleetCfg == nil {
			return FleetPlanResult{
				Error: "Failed to load fleet configuration.",
			}, nil
		}
	}

	// Check for existing custom plans for this fleet
	var savedPlans []SavedPlanInfo
	if fleetPlansDirVar != "" {
		existingPlans, err := fleet.LoadFleetPlansForFleet(fleetPlansDirVar, fleetKey)
		if err == nil {
			for _, p := range existingPlans {
				planJSON, _ := json.Marshal(p)
				savedPlans = append(savedPlans, SavedPlanInfo{
					Key:         slugify(p.Name),
					Name:        p.Name,
					Description: p.Description,
					PhaseCount:  len(p.Phases),
					PlanJSON:    string(planJSON),
				})
			}
		}
	}

	// Build default plan from fleet's suggested flow
	var phases []PlanPhaseInfo
	if fleetCfg.SuggestedFlow != nil {
		for i, phase := range fleetCfg.SuggestedFlow.Phases {
			agentKey := phase.GetPrimaryAgent()
			agentCfg, ok := fleetCfg.Agents[agentKey]
			if !ok {
				continue
			}

			personaName := agentCfg.Persona
			if p, pOk := personaRegistryVar.GetPersona(agentCfg.Persona); pOk {
				personaName = p.Name
			}

			// Build description from agent behaviors (first 200 chars for summary)
			desc := strings.TrimSpace(agentCfg.Behaviors)
			if len(desc) > 200 {
				desc = desc[:200] + "..."
			}

			// Determine dependencies from review map
			var deps []string
			if fleetCfg.SuggestedFlow.Reviews != nil {
				if reviewDeps, ok := fleetCfg.SuggestedFlow.Reviews[phase.Name]; ok {
					deps = reviewDeps
				}
			}

			phases = append(phases, PlanPhaseInfo{
				Order:       i + 1,
				Name:        phase.Name,
				Agent:       agentKey,
				PersonaName: personaName,
				Description: desc,
				DependsOn:   deps,
			})
		}
	}

	// Build the list of available agents from fleet config (sorted for deterministic output)
	sortedKeys := make([]string, 0, len(fleetCfg.Agents))
	for key := range fleetCfg.Agents {
		sortedKeys = append(sortedKeys, key)
	}
	sort.Strings(sortedKeys)

	var agents []AgentInfo
	var agentKeys []string
	for _, key := range sortedKeys {
		agentCfg := fleetCfg.Agents[key]
		personaName := agentCfg.Persona
		personaDesc := ""
		if p, pOk := personaRegistryVar.GetPersona(agentCfg.Persona); pOk {
			personaName = p.Name
			personaDesc = p.Description
		}
		agents = append(agents, AgentInfo{
			Key:         key,
			PersonaName: personaName,
			Description: personaDesc,
			HasDelegate: agentCfg.Delegate != nil,
		})
		agentKeys = append(agentKeys, fmt.Sprintf("`%s` (%s)", key, personaName))
	}

	// Build the message for the LLM
	var msg strings.Builder
	msg.WriteString("Present this plan to the user for review. ")

	if len(savedPlans) > 0 {
		msg.WriteString(fmt.Sprintf("The user has %d saved custom plan(s) for this fleet. ", len(savedPlans)))
		msg.WriteString("Offer to use a saved plan or create a new one. ")
		msg.WriteString("If using a saved plan, present its phases and ask the user to approve or adjust. ")
	} else {
		msg.WriteString("This is a default plan based on the fleet's suggested workflow. ")
		msg.WriteString("Walk through each phase with the user and let them customize: ")
		msg.WriteString("add/remove phases, add instructions or deliverables, change the order, etc. ")
	}

	msg.WriteString("\n\n**Available agents:** ")
	msg.WriteString(strings.Join(agentKeys, ", "))
	msg.WriteString(". ")
	msg.WriteString("When the user asks to add or modify phases, you MUST assign one of these agents. ")
	msg.WriteString("Do NOT invent agent names or roles that are not in this list. ")
	msg.WriteString("If the user asks for a role that doesn't exist (e.g., 'UX designer'), pick the closest available agent and explain the mapping to the user. ")
	msg.WriteString("When presenting the plan to the user, show the persona name (e.g., 'Product Owner') but always use the agent key (e.g., 'po') in the JSON plan.\n\n")

	msg.WriteString("Once the user approves the plan, call fleet_execute with the finalized plan. ")
	msg.WriteString("The plan should be passed as a JSON object with fields: ")
	msg.WriteString("base_fleet, name, description, phases (array with name, agent, instructions, deliverables, depends_on), ")
	msg.WriteString("reviews (map), preferences (string), settings (object with max_reviews_per_phase).")

	return FleetPlanResult{
		FleetKey:    fleetKey,
		FleetName:   fleetCfg.Name,
		Description: fleetCfg.Description,
		Phases:      phases,
		Agents:      agents,
		SavedPlans:  savedPlans,
		Message:     msg.String(),
	}, nil
}

// slugify creates a simple URL-safe key from a name.
func slugify(name string) string {
	s := strings.ToLower(name)
	s = strings.ReplaceAll(s, " ", "-")
	// Remove characters that aren't alphanumeric or hyphens
	var result strings.Builder
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' {
			result.WriteRune(r)
		}
	}
	return result.String()
}

// NewFleetPlanTool creates the fleet_plan tool.
func NewFleetPlanTool() (tool.Tool, error) {
	return functiontool.New(functiontool.Config{
		Name: "fleet_plan",
		Description: `Create a phased execution plan using a fleet of specialized agents.

Call this tool when the user requests a complex task that would benefit from multiple specialized agents working together (e.g., software development with requirements, architecture, implementation, testing, and security phases).

The tool returns a plan proposal based on the fleet's suggested workflow, along with any saved custom plans the user has previously created.

After receiving the plan, present it to the user for review and customization. The user can:
- Add or remove phases
- Add instructions or expected deliverables per phase
- Change the phase order or dependencies
- Specify preferences that apply across all phases

Once the user approves the plan, call fleet_execute with the finalized plan to start execution.`,
	}, fleetPlan)
}
