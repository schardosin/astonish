package fleet

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/SAP/astonish/pkg/store"
)

// PlanFromSetupBuildOptions configures plan creation from a setup build.
type PlanFromSetupBuildOptions struct {
	ValidationPassed bool
	CreatedBy        string
}

// BuildFleetPlanFromSetup assembles a FleetPlan from a setup build and base template.
func BuildFleetPlanFromSetup(baseCfg *FleetConfig, build *SetupPlanBuild, opts PlanFromSetupBuildOptions) (*FleetPlan, error) {
	if baseCfg == nil {
		return nil, fmt.Errorf("base template config is required")
	}
	if build == nil {
		return nil, fmt.Errorf("setup build is required")
	}

	cfgJSON, err := json.Marshal(baseCfg)
	if err != nil {
		return nil, fmt.Errorf("snapshot base fleet: %w", err)
	}
	var snapshotCfg FleetConfig
	if err := json.Unmarshal(cfgJSON, &snapshotCfg); err != nil {
		return nil, fmt.Errorf("snapshot base fleet: %w", err)
	}

	if len(build.IncludeAgents) > 0 {
		if err := filterAgentsInConfig(&snapshotCfg, build.IncludeAgents); err != nil {
			return nil, err
		}
	}
	for agentKey, override := range build.BehaviorOverrides {
		agentCfg, exists := snapshotCfg.Agents[agentKey]
		if !exists {
			return nil, fmt.Errorf("agent %q does not exist in template", agentKey)
		}
		override = strings.TrimSpace(override)
		if override != "" {
			agentCfg.Behaviors = agentCfg.Behaviors + "\n\n" + override
			snapshotCfg.Agents[agentKey] = agentCfg
		}
	}

	channelType := strings.TrimSpace(build.ChannelType)
	if channelType == "" {
		channelType = "chat"
	}
	if channelType != "chat" && !opts.ValidationPassed {
		return nil, fmt.Errorf("channel type %q requires validation before saving", channelType)
	}

	validationStatus := "pending"
	if channelType == "chat" || opts.ValidationPassed {
		validationStatus = "passed"
	}

	now := time.Now()
	plan := &FleetPlan{
		Name:                  build.Name,
		Key:                   build.Key,
		Description:           build.Description,
		CreatedFrom:           build.BaseFleetKey,
		SetupProfileKey:       build.SetupProfileKey,
		FleetConfig:           snapshotCfg,
		Credentials:           build.Credentials,
		CredentialInjection:   NormalizeCredentialInjection(build.Credentials, build.CredentialInjection),
		Channel: PlanChannelConfig{
			Type:     channelType,
			Config:   build.ChannelConfig,
			Schedule: build.ChannelSchedule,
		},
		Artifacts:             build.Artifacts,
		ProjectSource:         build.ProjectSource,
		Template:              build.Template,
		ContainerWorkspaceDir: build.ContainerWorkspaceDir,
		Validation: PlanValidationState{
			Status:        validationStatus,
			LastValidated: now,
		},
		CreatedBy: opts.CreatedBy,
		CreatedAt: now,
		UpdatedAt: now,
	}
	return plan, nil
}

func filterAgentsInConfig(cfg *FleetConfig, include []string) error {
	if cfg == nil || len(include) == 0 {
		return nil
	}
	allowed := map[string]struct{}{}
	for _, key := range include {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		allowed[key] = struct{}{}
	}
	for key := range cfg.Agents {
		if _, ok := allowed[key]; !ok {
			delete(cfg.Agents, key)
		}
	}
	if cfg.Communication != nil {
		filtered := make([]CommunicationNode, 0, len(cfg.Communication.Flow))
		for _, node := range cfg.Communication.Flow {
			if _, ok := allowed[node.Role]; ok {
				filtered = append(filtered, node)
			}
		}
		cfg.Communication.Flow = filtered
	}
	return nil
}

// WizardContextForTemplate resolves chat wizard prompt and pinned tools for plan creation.
func WizardContextForTemplate(cfg *FleetConfig, templateKey string) (prompt string, pinned []string) {
	if cfg == nil {
		return "", nil
	}
	if cfg.PlanWizard != nil && cfg.SetupProfileKey == "" {
		return cfg.PlanWizard.SystemPrompt, cfg.PlanWizard.PinnedToolGroups
	}
	profileKey := ProfileForTemplate(cfg)
	profile, err := ResolveSetupProfile(context.Background(), profileKey, nil)
	if err != nil || profile == nil {
		return "", nil
	}
	draftCtx := SetupDraftContext{BaseFleetKey: templateKey}
	if cfg.Agents != nil {
		for k := range cfg.Agents {
			draftCtx.TemplateAgentNames = append(draftCtx.TemplateAgentNames, k)
		}
	}
	wctx := BuildSetupWizardContext(profile, nil, draftCtx)
	return wctx.SystemPrompt, wctx.PinnedToolGroups
}

func ParseSetupCollected(raw map[string]any) SetupCollected {
	out := SetupCollected{}
	if raw == nil {
		return out
	}
	for stepID, val := range raw {
		if m, ok := val.(map[string]any); ok {
			out[stepID] = m
		}
	}
	return out
}

// SetupCollectedToMap converts SetupCollected for JSON persistence.
func SetupCollectedToMap(collected SetupCollected) map[string]any {
	out := map[string]any{}
	for stepID, values := range collected {
		out[stepID] = values
	}
	return out
}

// SaveFleetPlanFromContext saves a plan using store from context.
func SaveFleetPlanFromContext(ctx context.Context, plan *FleetPlan, planStore store.FleetPlanStore) error {
	if planStore == nil {
		return fmt.Errorf("fleet plan store not available")
	}
	return planStore.Save(ctx, plan)
}

// ResolveSetupProfile resolves a profile using bundled data and optional store.
func ResolveSetupProfile(ctx context.Context, key string, profileStore store.FleetSetupProfileStore) (*SetupProfile, error) {
	resolve := func(k string) (*SetupProfile, bool) {
		if p, ok := GetBundledSetupProfile(k); ok {
			return p, true
		}
		if profileStore != nil {
			if raw, ok := profileStore.GetProfile(ctx, k); ok {
				if p, ok := raw.(*SetupProfile); ok {
					return p, true
				}
			}
		}
		return nil, false
	}
	engine := NewSetupEngine(resolve)
	return engine.ResolveProfile(key)
}
