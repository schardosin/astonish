package fleet

import "strings"

// SetupDraftContext carries runtime context for prompt composition (not stored in profile YAML).
type SetupDraftContext struct {
	BaseFleetKey        string
	TemplateAgentNames  []string
}

// NormalizeStepType maps legacy step type names to canonical types.
func NormalizeStepType(stepType string) string {
	switch strings.TrimSpace(stepType) {
	case "template_agents":
		return "agent_select"
	default:
		return strings.TrimSpace(stepType)
	}
}

// StepEffectiveType returns the canonical type for a step.
func (s *SetupStep) EffectiveType() string {
	if s == nil {
		return ""
	}
	return NormalizeStepType(s.Type)
}

// StepPromptText returns the LLM prompt for a step (prompt field, then guidance fallback).
func (s *SetupStep) StepPromptText() string {
	if s == nil {
		return ""
	}
	if p := strings.TrimSpace(s.Prompt); p != "" {
		return p
	}
	return strings.TrimSpace(s.Guidance)
}

// StepPresetDefaults returns default tool groups and tool names for a step type.
func StepPresetDefaults(step *SetupStep, collected SetupCollected) (groups []string, tools []string) {
	if step == nil {
		return nil, nil
	}
	switch step.EffectiveType() {
	case "credentials":
		return []string{"credentials"}, []string{"list_credentials", "save_credential", "test_credential"}
	case "provision":
		g := []string{"sandbox_templates", "drill", "core", "process"}
		t := []string{"save_sandbox_template", "list_sandbox_templates", "inject_drill_credentials", "run_drill", "shell_command"}
		return g, t
	case "review":
		return []string{"fleet"}, []string{"validate_fleet_plan", "save_fleet_plan", "update_setup_draft", "get_setup_profile"}
	case "agent_select":
		return []string{"fleet"}, []string{"update_setup_draft"}
	case "form":
		if isGitHubChannel(collected) {
			return []string{"fleet", "credentials"}, []string{"validate_fleet_plan"}
		}
	}
	return nil, nil
}

func isGitHubChannel(collected SetupCollected) bool {
	if collected == nil {
		return false
	}
	if ch, ok := collected["channel"]; ok {
		if t, ok := ch["type"].(string); ok && t == "github_issues" {
			return true
		}
	}
	return getCollectedPath(collected, "channel.type") == "github_issues"
}

// StepToolGroups merges profile defaults, step pinned groups, preset groups, and tool-derived groups.
func StepToolGroups(profile *SetupProfile, step *SetupStep, collected SetupCollected) []string {
	if step == nil {
		return nil
	}
	presetGroups, presetTools := StepPresetDefaults(step, collected)
	tools := StepTools(step, collected)
	_ = presetTools

	seen := map[string]struct{}{}
	var out []string
	add := func(items ...string) {
		for _, g := range items {
			g = strings.TrimSpace(g)
			if g == "" {
				continue
			}
			if _, dup := seen[g]; dup {
				continue
			}
			seen[g] = struct{}{}
			out = append(out, g)
		}
	}
	if profile != nil {
		add(profile.PinnedToolGroups...)
	}
	add(step.PinnedToolGroups...)
	add(presetGroups...)
	add(SetupToolGroupsForNames(tools)...)
	return out
}

// StepTools merges explicit step tools with preset defaults.
func StepTools(step *SetupStep, collected SetupCollected) []string {
	if step == nil {
		return nil
	}
	_, presetTools := StepPresetDefaults(step, collected)
	seen := map[string]struct{}{}
	var out []string
	add := func(items ...string) {
		for _, t := range items {
			t = strings.TrimSpace(t)
			if t == "" {
				continue
			}
			if _, dup := seen[t]; dup {
				continue
			}
			seen[t] = struct{}{}
			out = append(out, t)
		}
	}
	add(step.Tools...)
	add(presetTools...)
	return out
}

// ChannelTypeDef describes a supported plan channel type in a setup profile.
type ChannelTypeDef struct {
	Label               string   `yaml:"label" json:"label"`
	Description         string   `yaml:"description,omitempty" json:"description,omitempty"`
	RequiresCredentials []string `yaml:"requires_credentials,omitempty" json:"requires_credentials,omitempty"`
	PinnedToolGroups    []string `yaml:"pinned_tool_groups,omitempty" json:"pinned_tool_groups,omitempty"`
}
