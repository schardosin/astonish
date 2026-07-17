package agent

import (
	"strings"

	"github.com/SAP/astonish/pkg/config"
)

const tutorialBlueprintApprovalRequiredMsg = "Cannot use validate_drill / save_drill / blueprint_to_tutorial_drill for mode:tutorial until the creator Approves a tutorial blueprint. Call present_tutorial_blueprint and wait for Approve & generate."

// DrillYAMLsContainTutorialMode reports whether any YAML parses as a drill with
// drill_config.mode == "tutorial".
func DrillYAMLsContainTutorialMode(yamls []string) bool {
	for _, y := range yamls {
		if y == "" {
			continue
		}
		cfg, err := config.LoadAgentFromBytes([]byte(y))
		if err != nil || cfg == nil || cfg.DrillConfig == nil {
			continue
		}
		if strings.EqualFold(strings.TrimSpace(cfg.DrillConfig.Mode), "tutorial") {
			return true
		}
	}
	return false
}

// TutorialBlueprintApprovalRequiredResult is the short-circuit tool result when
// the approval gate blocks execution.
func TutorialBlueprintApprovalRequiredResult() map[string]any {
	return map[string]any{
		"status":  "error",
		"message": tutorialBlueprintApprovalRequiredMsg,
	}
}

// CheckTutorialDrillToolGate blocks tutorial drill tools until the session has
// an approved blueprint. Returns (true, result) when the tool must not run.
func CheckTutorialDrillToolGate(toolName string, args map[string]any, approved bool) (blocked bool, result map[string]any) {
	switch toolName {
	case "blueprint_to_tutorial_drill":
		if approved {
			return false, nil
		}
		return true, TutorialBlueprintApprovalRequiredResult()

	case "save_drill":
		if approved || !DrillYAMLsContainTutorialMode(yamlsFromSaveDrillArgs(args)) {
			return false, nil
		}
		return true, TutorialBlueprintApprovalRequiredResult()

	case "validate_drill":
		if approved || !DrillYAMLsContainTutorialMode(yamlsFromValidateDrillArgs(args)) {
			return false, nil
		}
		return true, TutorialBlueprintApprovalRequiredResult()

	default:
		return false, nil
	}
}

func yamlsFromSaveDrillArgs(args map[string]any) []string {
	if args == nil {
		return nil
	}
	raw, ok := args["tests"]
	if !ok || raw == nil {
		return nil
	}
	var out []string
	switch tests := raw.(type) {
	case []any:
		for _, item := range tests {
			out = append(out, yamlFieldFromAny(item))
		}
	case []map[string]any:
		for _, item := range tests {
			if y, ok := item["yaml"].(string); ok {
				out = append(out, y)
			}
		}
	}
	return out
}

func yamlsFromValidateDrillArgs(args map[string]any) []string {
	if args == nil {
		return nil
	}
	raw, ok := args["test_yamls"]
	if !ok || raw == nil {
		return nil
	}
	switch yamls := raw.(type) {
	case []string:
		return yamls
	case []any:
		out := make([]string, 0, len(yamls))
		for _, item := range yamls {
			if s, ok := item.(string); ok {
				out = append(out, s)
			}
		}
		return out
	default:
		return nil
	}
}

func yamlFieldFromAny(item any) string {
	switch m := item.(type) {
	case map[string]any:
		if y, ok := m["yaml"].(string); ok {
			return y
		}
	case map[string]string:
		return m["yaml"]
	}
	return ""
}
