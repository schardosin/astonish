package tools

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/schardosin/astonish/pkg/fleet"
	"google.golang.org/adk/tool"
	"google.golang.org/adk/tool/functiontool"
)

// fleetPlanRegistryVar holds the plan registry for the save_fleet_plan tool.
// Set by the launcher via SetFleetPlanRegistry.
var fleetPlanRegistryVar *fleet.PlanRegistry

// SetFleetPlanRegistry registers the plan registry for the fleet plan tool.
func SetFleetPlanRegistry(reg *fleet.PlanRegistry) {
	fleetPlanRegistryVar = reg
}

// GetFleetPlanRegistry returns the plan registry (for use by other packages).
func GetFleetPlanRegistry() *fleet.PlanRegistry {
	return fleetPlanRegistryVar
}

// SaveFleetPlanArgs are the arguments for the save_fleet_plan tool.
type SaveFleetPlanArgs struct {
	// Key is a unique identifier for the plan (lowercase, hyphens, e.g., "frontend-bugs")
	Key string `json:"key"`
	// Name is the human-readable display name
	Name string `json:"name"`
	// Description explains what this plan does
	Description string `json:"description"`
	// BaseFleetKey is the fleet this plan is based on (e.g., "software-dev")
	BaseFleetKey string `json:"base_fleet_key"`
	// ChannelType is the input channel: "chat", "github_issues", "jira", "email"
	ChannelType string `json:"channel_type"`
	// ChannelConfig holds channel-specific settings as a JSON object
	ChannelConfig map[string]any `json:"channel_config,omitempty"`
	// ChannelSchedule is a cron expression for polling (non-chat channels)
	ChannelSchedule string `json:"channel_schedule,omitempty"`
	// Artifacts maps artifact categories to their destinations (JSON object)
	Artifacts map[string]SaveFleetPlanArtifact `json:"artifacts,omitempty"`
	// BehaviorOverrides maps agent keys to behavior text additions.
	// These are appended to (not replacing) the base fleet agent behaviors.
	BehaviorOverrides map[string]string `json:"behavior_overrides,omitempty"`
	// ValidationPassed should be true if validate_fleet_plan was called and passed.
	// Plans for non-chat channels require validation before saving.
	ValidationPassed bool `json:"validation_passed,omitempty"`
}

// SaveFleetPlanArtifact describes a single artifact destination.
type SaveFleetPlanArtifact struct {
	Type          string `json:"type"`                     // "local" or "git_repo"
	Path          string `json:"path,omitempty"`           // for "local"
	Repo          string `json:"repo,omitempty"`           // for "git_repo"
	BranchPattern string `json:"branch_pattern,omitempty"` // for "git_repo"
	SubPath       string `json:"sub_path,omitempty"`       // for "git_repo"
	AutoPR        bool   `json:"auto_pr,omitempty"`        // for "git_repo"
}

// SaveFleetPlanResult is the result of the save_fleet_plan tool.
type SaveFleetPlanResult struct {
	Status  string `json:"status"`
	Key     string `json:"key,omitempty"`
	Message string `json:"message"`
}

func saveFleetPlan(_ tool.Context, args SaveFleetPlanArgs) (SaveFleetPlanResult, error) {
	if fleetPlanRegistryVar == nil {
		return SaveFleetPlanResult{
			Status:  "error",
			Message: "Fleet plan system is not initialized.",
		}, nil
	}

	if fleetRegistryVar == nil {
		return SaveFleetPlanResult{
			Status:  "error",
			Message: "Fleet system is not initialized.",
		}, nil
	}

	// Validate required fields
	key := strings.TrimSpace(args.Key)
	if key == "" {
		return SaveFleetPlanResult{
			Status:  "error",
			Message: "key is required. Use a lowercase, hyphenated identifier like 'frontend-bugs'.",
		}, nil
	}
	name := strings.TrimSpace(args.Name)
	if name == "" {
		return SaveFleetPlanResult{
			Status:  "error",
			Message: "name is required.",
		}, nil
	}
	baseKey := strings.TrimSpace(args.BaseFleetKey)
	if baseKey == "" {
		return SaveFleetPlanResult{
			Status:  "error",
			Message: "base_fleet_key is required. List available fleets with ListAvailableFleets.",
		}, nil
	}

	// Load the base fleet config
	baseCfg, ok := fleetRegistryVar.GetFleet(baseKey)
	if !ok {
		summaries := fleetRegistryVar.ListFleets()
		keys := make([]string, len(summaries))
		for i, s := range summaries {
			keys[i] = s.Key
		}
		return SaveFleetPlanResult{
			Status:  "error",
			Message: fmt.Sprintf("Fleet %q not found. Available fleets: %s", baseKey, strings.Join(keys, ", ")),
		}, nil
	}

	// Deep copy the base fleet config by marshalling/unmarshalling through JSON
	cfgJSON, err := json.Marshal(baseCfg)
	if err != nil {
		return SaveFleetPlanResult{
			Status:  "error",
			Message: fmt.Sprintf("Failed to snapshot base fleet config: %v", err),
		}, nil
	}
	var snapshotCfg fleet.FleetConfig
	if err := json.Unmarshal(cfgJSON, &snapshotCfg); err != nil {
		return SaveFleetPlanResult{
			Status:  "error",
			Message: fmt.Sprintf("Failed to snapshot base fleet config: %v", err),
		}, nil
	}

	// Apply behavior overrides (append to existing behaviors)
	if len(args.BehaviorOverrides) > 0 {
		for agentKey, override := range args.BehaviorOverrides {
			agentCfg, exists := snapshotCfg.Agents[agentKey]
			if !exists {
				return SaveFleetPlanResult{
					Status:  "error",
					Message: fmt.Sprintf("Agent %q does not exist in fleet %q. Available agents: %s", agentKey, baseKey, agentKeysString(snapshotCfg.Agents)),
				}, nil
			}
			override = strings.TrimSpace(override)
			if override != "" {
				agentCfg.Behaviors = agentCfg.Behaviors + "\n\n" + override
				snapshotCfg.Agents[agentKey] = agentCfg
			}
		}
	}

	// Build channel config
	channelType := strings.TrimSpace(args.ChannelType)
	if channelType == "" {
		channelType = "chat"
	}

	// Non-chat channels require validation before saving
	if channelType != "chat" && !args.ValidationPassed {
		return SaveFleetPlanResult{
			Status:  "error",
			Message: fmt.Sprintf("Channel type %q requires validation before saving. Call validate_fleet_plan first and set validation_passed=true after it passes.", channelType),
		}, nil
	}

	channelCfg := fleet.PlanChannelConfig{
		Type:     channelType,
		Config:   args.ChannelConfig,
		Schedule: strings.TrimSpace(args.ChannelSchedule),
	}

	// Build artifact configs
	var artifacts map[string]fleet.PlanArtifactConfig
	if len(args.Artifacts) > 0 {
		artifacts = make(map[string]fleet.PlanArtifactConfig, len(args.Artifacts))
		for aName, a := range args.Artifacts {
			artifacts[aName] = fleet.PlanArtifactConfig{
				Type:          a.Type,
				Path:          a.Path,
				Repo:          a.Repo,
				BranchPattern: a.BranchPattern,
				SubPath:       a.SubPath,
				AutoPR:        a.AutoPR,
			}
		}
	}

	// Build and save the plan
	now := time.Now()

	// Build validation state
	validationStatus := "pending"
	if channelType == "chat" || args.ValidationPassed {
		validationStatus = "passed"
	}

	plan := &fleet.FleetPlan{
		Name:        name,
		Key:         key,
		Description: strings.TrimSpace(args.Description),
		CreatedFrom: baseKey,
		FleetConfig: snapshotCfg,
		Channel:     channelCfg,
		Artifacts:   artifacts,
		Validation: fleet.PlanValidationState{
			Status:        validationStatus,
			LastValidated: now,
		},
		CreatedAt: now,
		UpdatedAt: now,
	}

	if err := fleetPlanRegistryVar.Save(plan); err != nil {
		return SaveFleetPlanResult{
			Status:  "error",
			Message: fmt.Sprintf("Failed to save fleet plan: %v", err),
		}, nil
	}

	return SaveFleetPlanResult{
		Status:  "saved",
		Key:     key,
		Message: fmt.Sprintf("Fleet plan %q saved successfully. It can be started from the Studio UI or CLI.", name),
	}, nil
}

// agentKeysString returns a comma-separated list of agent keys.
func agentKeysString(agents map[string]fleet.FleetAgentConfig) string {
	keys := make([]string, 0, len(agents))
	for k := range agents {
		keys = append(keys, k)
	}
	return strings.Join(keys, ", ")
}

// GetFleetPlanTools returns the fleet plan creation tool.
func GetFleetPlanTools() ([]tool.Tool, error) {
	t, err := functiontool.New(functiontool.Config{
		Name: "save_fleet_plan",
		Description: "Save a fleet plan configuration. Creates a reusable, fully-configured fleet definition " +
			"that includes both the team composition (snapshotted from a base fleet) and environment-specific " +
			"settings like communication channel and artifact destinations. " +
			"The plan is stored as a YAML file and can be launched from the Studio UI.",
	}, saveFleetPlan)
	if err != nil {
		return nil, err
	}
	return []tool.Tool{t}, nil
}
